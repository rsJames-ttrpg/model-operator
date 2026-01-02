# Model Operator Specification

## Overview

A Kubernetes operator that declaratively manages ML model files on PersistentVolumeClaims. Users define `Model` custom resources specifying a source (HuggingFace, S3, HTTP URL, gitlfs) and storage configuration. The operator creates PVCs and download Jobs automatically. A mutating admission webhook enables annotation-based injection of model volumes into workloads.

## Goals

1. **Declarative model management** - Models are Kubernetes-native resources with status tracking
2. **Multiple sources** - Support HuggingFace Hub, S3-compatible storage, and direct URLs
3. **Automatic injection** - Annotate pods to inject model volumes without manual PVC references
4. **Version tracking** - Explicit version field for model lifecycle management
5. **Credential management** - Reference Kubernetes Secrets for private model access

## Technology Stack

- **Language**: Go 1.22+
- **Framework**: controller-runtime (kubebuilder)
- **Kubernetes**: 1.28+
- **Dependencies**:
  - `sigs.k8s.io/controller-runtime`
  - `k8s.io/api`
  - `k8s.io/apimachinery`
  - `k8s.io/client-go`

---

## Custom Resource Definitions

### Model CRD

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: models.models.example.com
spec:
  group: models.example.com
  names:
    kind: Model
    listKind: ModelList
    plural: models
    singular: model
    shortNames:
      - mdl
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      subresources:
        status: {}
      additionalPrinterColumns:
        - name: Phase
          type: string
          jsonPath: .status.phase
        - name: Version
          type: string
          jsonPath: .spec.version
        - name: Size
          type: string
          jsonPath: .spec.storage.size
        - name: Age
          type: date
          jsonPath: .metadata.creationTimestamp
```

### ModelSpec

```go
type ModelSpec struct {
    // Source defines where to download the model from
    // +kubebuilder:validation:Required
    Source ModelSource `json:"source"`

    // Storage defines PVC configuration
    // +kubebuilder:validation:Required
    Storage StorageSpec `json:"storage"`

    // Version is an optional version identifier for tracking
    // +optional
    Version string `json:"version,omitempty"`

    // CredentialsSecret references a Secret containing credentials
    // For HuggingFace: key "HF_TOKEN"
    // For S3: keys "AWS_ACCESS_KEY_ID" and "AWS_SECRET_ACCESS_KEY"
    // +optional
    CredentialsSecret string `json:"credentialsSecret,omitempty"`

    // NodeSelector for the download Job
    // +optional
    NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}
```

### ModelSource

ModelSource is a discriminated union. Exactly one field must be set.

```go
type ModelSource struct {
    // HuggingFace source configuration
    // +optional
    HuggingFace *HuggingFaceSource `json:"huggingFace,omitempty"`

    // URL source for direct HTTP/HTTPS downloads
    // +optional
    URL *URLSource `json:"url,omitempty"`

    // S3 source for S3-compatible storage
    // +optional
    S3 *S3Source `json:"s3,omitempty"`
}

type HuggingFaceSource struct {
    // RepoID is the HuggingFace repository ID (e.g., "meta-llama/Llama-3.1-8B-Instruct")
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Pattern=`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`
    RepoID string `json:"repoId"`

    // Revision is the git revision (branch, tag, or commit hash)
    // +optional
    // +kubebuilder:default="main"
    Revision string `json:"revision,omitempty"`
}

type URLSource struct {
    // URL is the direct download URL
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Pattern=`^https?://`
    URL string `json:"url"`
}

type S3Source struct {
    // Bucket name
    // +kubebuilder:validation:Required
    Bucket string `json:"bucket"`

    // Key is the object key or prefix
    // +kubebuilder:validation:Required
    Key string `json:"key"`

    // Endpoint for S3-compatible storage (e.g., MinIO)
    // +optional
    Endpoint string `json:"endpoint,omitempty"`

    // Region for AWS S3
    // +optional
    Region string `json:"region,omitempty"`
}
```

### StorageSpec

```go
type StorageSpec struct {
    // StorageClass name (e.g., "longhorn", "gp3")
    // +kubebuilder:validation:Required
    StorageClass string `json:"storageClass"`

    // Size of the PVC (e.g., "20Gi")
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Pattern=`^[0-9]+[KMGTPE]i?$`
    Size string `json:"size"`

    // AccessModes for the PVC
    // +optional
    // +kubebuilder:default={"ReadWriteOnce"}
    AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
}
```

### ModelStatus

```go
type ModelStatus struct {
    // Phase indicates the current state
    // +kubebuilder:validation:Enum=Pending;Downloading;Ready;Failed
    Phase ModelPhase `json:"phase,omitempty"`

    // PVCName is the name of the created PVC
    PVCName string `json:"pvcName,omitempty"`

    // Message is a human-readable status message
    Message string `json:"message,omitempty"`

    // Progress is the download progress (0-100)
    // +kubebuilder:validation:Minimum=0
    // +kubebuilder:validation:Maximum=100
    Progress int `json:"progress,omitempty"`

    // Conditions provide detailed status information
    Conditions []metav1.Condition `json:"conditions,omitempty"`

    // ObservedGeneration is the last observed generation
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

type ModelPhase string

const (
    ModelPhasePending     ModelPhase = "Pending"
    ModelPhaseDownloading ModelPhase = "Downloading"
    ModelPhaseReady       ModelPhase = "Ready"
    ModelPhaseFailed      ModelPhase = "Failed"
)
```

---

## Controller Logic

### Reconciliation State Machine

```
┌─────────────────────────────────────────────────────────────┐
│                                                             │
│  ┌─────────┐    ┌─────────────┐    ┌───────┐    ┌────────┐ │
│  │ Pending │───▶│ Downloading │───▶│ Ready │    │ Failed │ │
│  └─────────┘    └─────────────┘    └───────┘    └────────┘ │
│       │               │                │             │      │
│       │               │                │             │      │
│       └───────────────┴────────────────┴─────────────┘      │
│                       (on PVC deleted)   (on Job deleted)   │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Reconcile Function Pseudocode

```go
func (r *ModelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch the Model
    model := &modelsv1alpha1.Model{}
    if err := r.Get(ctx, req.NamespacedName, model); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 2. Determine current phase (default to Pending)
    phase := model.Status.Phase
    if phase == "" {
        phase = ModelPhasePending
    }

    switch phase {
    case ModelPhasePending:
        return r.reconcilePending(ctx, model)
    case ModelPhaseDownloading:
        return r.reconcileDownloading(ctx, model)
    case ModelPhaseReady:
        return r.reconcileReady(ctx, model)
    case ModelPhaseFailed:
        return r.reconcileFailed(ctx, model)
    }

    return ctrl.Result{}, nil
}
```

### Phase: Pending

1. Create PVC if not exists
   - Name: `model-{model.Name}`
   - Set OwnerReference to Model
   - Apply storage configuration from spec
2. Create download Job if not exists
   - Name: `model-download-{model.Name}`
   - Set OwnerReference to Model
   - Configure based on source type (see Download Job section)
3. Update status to `Downloading`
4. Requeue after 10 seconds

### Phase: Downloading

1. Get the download Job
2. Check Job status:
   - If `succeeded > 0`: Update to `Ready`, progress=100
   - If `failed >= backoffLimit (3)`: Update to `Failed`
   - Otherwise: Requeue after 15 seconds
3. If Job not found: Recreate it, requeue after 10 seconds

### Phase: Ready

1. Verify PVC still exists
2. If PVC deleted: Reset to `Pending`
3. Requeue after 5 minutes (slow poll)

### Phase: Failed

1. Check if Job was deleted (manual retry trigger)
2. If Job deleted: Reset to `Pending`
3. Requeue after 1 minute

### Download Job Specifications

#### HuggingFace Source

```yaml
spec:
  backoffLimit: 3
  ttlSecondsAfterFinished: 3600
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: downloader
          image: python:3.11-slim
          command: ["sh", "-c"]
          args:
            - |
              pip install -q huggingface_hub hf_transfer &&
              export HF_HUB_ENABLE_HF_TRANSFER=1 &&
              huggingface-cli download {repoId} --revision {revision} --local-dir /models &&
              echo "Download complete" &&
              ls -la /models
          env:
            - name: HF_TOKEN
              valueFrom:
                secretKeyRef:
                  name: {credentialsSecret}
                  key: HF_TOKEN
                  optional: true
          volumeMounts:
            - name: model-storage
              mountPath: /models
          resources:
            requests:
              memory: "512Mi"
              cpu: "500m"
            limits:
              memory: "2Gi"
              cpu: "2"
      volumes:
        - name: model-storage
          persistentVolumeClaim:
            claimName: model-{modelName}
```

#### S3 Source

```yaml
containers:
  - name: downloader
    image: amazon/aws-cli:latest
    command: ["sh", "-c"]
    args:
      - |
        aws s3 cp {endpoint_arg} {region_arg} s3://{bucket}/{key} /models/ --recursive &&
        echo "Download complete" &&
        ls -la /models
    env:
      - name: AWS_ACCESS_KEY_ID
        valueFrom:
          secretKeyRef:
            name: {credentialsSecret}
            key: AWS_ACCESS_KEY_ID
            optional: true
      - name: AWS_SECRET_ACCESS_KEY
        valueFrom:
          secretKeyRef:
            name: {credentialsSecret}
            key: AWS_SECRET_ACCESS_KEY
            optional: true
```

#### URL Source

```yaml
containers:
  - name: downloader
    image: curlimages/curl:latest
    command: ["sh", "-c"]
    args:
      - |
        curl -L -o /models/model "{{url}}" &&
        echo "Download complete" &&
        ls -la /models
```

### Controller Setup

```go
func (r *ModelReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&modelsv1alpha1.Model{}).
        Owns(&corev1.PersistentVolumeClaim{}).
        Owns(&batchv1.Job{}).
        Complete(r)
}
```

---

## Mutating Admission Webhook

### Purpose

Automatically inject model volumes and environment variables into Pods based on annotations, removing the need for manual PVC references.

### Annotations Reference

| Annotation | Required | Default | Description |
|------------|----------|---------|-------------|
| `models.example.com/inject` | Yes | - | Comma-separated list of Model names to inject |
| `models.example.com/mount-path` | No | `/models/{name}` | Override mount path (single model) or base path (multiple) |
| `models.example.com/read-only` | No | `"true"` | Mount as read-only |
| `models.example.com/container` | No | First container | Target container name for injection |
| `models.example.com/inject-env` | No | `"true"` | Inject MODEL_* environment variables |

### Injected Environment Variables

For each model `{name}`, the following environment variables are injected:

```bash
MODEL_{NAME}_NAME={name}                    # Always
MODEL_{NAME}_VERSION={version}              # If spec.version set
MODEL_{NAME}_SOURCE_TYPE={huggingface|s3|url}
MODEL_{NAME}_REPO_ID={repoId}               # If HuggingFace
MODEL_{NAME}_URL={url}                      # If URL source
MODEL_{NAME}_BUCKET={bucket}                # If S3
MODEL_{NAME}_MOUNT_PATH={mountPath}         # Always
```

Where `{NAME}` is the model name uppercased with hyphens replaced by underscores.

### Webhook Logic Pseudocode

```go
func (w *ModelInjector) Handle(ctx context.Context, req admission.Request) admission.Response {
    pod := &corev1.Pod{}
    if err := w.decoder.Decode(req, pod); err != nil {
        return admission.Errored(http.StatusBadRequest, err)
    }

    // Check for injection annotation
    injectAnnotation, ok := pod.Annotations["models.example.com/inject"]
    if !ok {
        return admission.Allowed("no injection requested")
    }

    // Parse options
    opts := parseOptions(pod.Annotations)
    modelNames := strings.Split(injectAnnotation, ",")

    // Build patches
    var patches []jsonpatch.Operation

    for _, name := range modelNames {
        name = strings.TrimSpace(name)
        
        // Fetch Model CR
        model := &modelsv1alpha1.Model{}
        if err := w.client.Get(ctx, types.NamespacedName{
            Name:      name,
            Namespace: req.Namespace,
        }, model); err != nil {
            return admission.Denied(fmt.Sprintf("model %q not found: %v", name, err))
        }

        // Verify model is Ready
        if model.Status.Phase != ModelPhaseReady {
            return admission.Denied(fmt.Sprintf("model %q is not ready (phase: %s)", name, model.Status.Phase))
        }

        // Add volume patch
        patches = append(patches, buildVolumePatch(model, opts))

        // Add volumeMount patch
        patches = append(patches, buildVolumeMountPatch(model, opts))

        // Add env patches if enabled
        if opts.InjectEnv {
            patches = append(patches, buildEnvPatches(model, opts)...)
        }
    }

    // Add label to mark injection
    patches = append(patches, jsonpatch.Operation{
        Operation: "add",
        Path:      "/metadata/labels/models.example.com~1injected",
        Value:     "true",
    })

    return admission.Patched("models injected", patches...)
}
```

### Webhook Configuration

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: model-injector
webhooks:
  - name: model-injector.models.example.com
    admissionReviewVersions: ["v1"]
    sideEffects: None
    timeoutSeconds: 10
    failurePolicy: Ignore  # Don't block pod creation if webhook unavailable
    objectSelector:
      matchExpressions:
        - key: models.example.com/injected
          operator: DoesNotExist
    rules:
      - apiGroups: [""]
        apiVersions: ["v1"]
        resources: ["pods"]
        operations: ["CREATE"]
        scope: Namespaced
    clientConfig:
      service:
        name: model-operator-webhook
        namespace: model-operator
        path: /mutate
        port: 443
```

---

## Project Structure

```
model-operator/
├── cmd/
│   └── main.go                     # Entry point
├── api/
│   └── v1alpha1/
│       ├── model_types.go          # CRD type definitions
│       ├── groupversion_info.go    # API group registration
│       └── zz_generated.deepcopy.go
├── internal/
│   ├── controller/
│   │   ├── model_controller.go     # Reconciliation logic
│   │   └── model_controller_test.go
│   ├── webhook/
│   │   ├── model_injector.go       # Mutating webhook
│   │   └── model_injector_test.go
│   └── resources/
│       ├── pvc.go                  # PVC builder
│       ├── job.go                  # Job builder (per source type)
│       └── naming.go               # Naming conventions
├── config/
│   ├── crd/
│   │   └── bases/
│   │       └── models.example.com_models.yaml
│   ├── manager/
│   │   └── manager.yaml
│   ├── rbac/
│   │   ├── role.yaml
│   │   ├── role_binding.yaml
│   │   └── service_account.yaml
│   ├── webhook/
│   │   ├── manifests.yaml
│   │   └── service.yaml
│   └── samples/
│       ├── model_huggingface.yaml
│       ├── model_s3.yaml
│       ├── model_url.yaml
│       └── deployment_with_injection.yaml
├── Dockerfile
├── Makefile
├── go.mod
└── README.md
```

---

## Naming Conventions

| Resource | Name Pattern | Example |
|----------|--------------|---------|
| PVC | `model-{modelName}` | `model-llama-3-8b` |
| Download Job | `model-download-{modelName}` | `model-download-llama-3-8b` |
| Volume (in Pod) | `model-{modelName}` | `model-llama-3-8b` |
| Env Var Prefix | `MODEL_{UPPER_NAME}` | `MODEL_LLAMA_3_8B` |

---

## RBAC Requirements

```yaml
rules:
  # Model CRD
  - apiGroups: ["models.example.com"]
    resources: ["models"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["models.example.com"]
    resources: ["models/status"]
    verbs: ["get", "update", "patch"]

  # PVCs
  - apiGroups: [""]
    resources: ["persistentvolumeclaims"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]

  # Jobs
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]

  # Pods (for webhook and job status)
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]

  # Secrets (for credentials)
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch"]

  # Events
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
```

---

## Error Handling

### Controller Errors

| Error | Action |
|-------|--------|
| Model not found | Return (deleted, no requeue) |
| PVC creation failed | Requeue with backoff, update status message |
| Job creation failed | Requeue with backoff, update status message |
| Status update failed | Requeue immediately |

### Webhook Errors

| Error | Response |
|-------|----------|
| Model not found | Deny with message |
| Model not Ready | Deny with message |
| Invalid annotation value | Deny with message |
| Decode error | Error 400 |

---

## Testing Requirements

### Unit Tests

1. **CRD Validation**
   - Valid ModelSpec accepted
   - Invalid source (none set) rejected
   - Invalid source (multiple set) rejected
   - Invalid size format rejected
   - Invalid repoId format rejected

2. **Resource Builders**
   - PVC has correct owner reference
   - PVC has correct storage class and size
   - Job has correct image per source type
   - Job has correct env vars from secret
   - Job has correct volume mounts

3. **Webhook**
   - No annotation = allowed unchanged
   - Single model injection
   - Multiple model injection
   - Custom mount path
   - Read-only vs read-write
   - Target specific container
   - Env var injection disabled
   - Model not found = denied
   - Model not ready = denied

### Integration Tests

1. **Happy Path**
   - Create Model → PVC created → Job created → Job completes → Status=Ready
   - Create Pod with annotation → Volumes injected

2. **Failure Recovery**
   - Job fails → Status=Failed → Delete Job → Status=Pending → Retry

3. **Cleanup**
   - Delete Model → PVC deleted (owner reference) → Job deleted

### E2E Tests

1. Full workflow with real HuggingFace download (small model)
2. Injection into Deployment, verify pod has volumes
3. Model update triggers re-download (future feature)

---

## Future Enhancements (Out of Scope for v1)

1. **Model Updates** - Detect source changes, trigger re-download
2. **Progress Tracking** - Parse job logs for download progress
3. **Model Registry Integration** - Support `model-registry://` URIs
4. **Multi-node Caching** - LocalModelCache-style node-local storage
5. **Validation Webhook** - Validate Model specs before creation
6. **Metrics** - Prometheus metrics for download times, cache hits
7. **Garbage Collection** - Clean up orphaned PVCs

---

## Example Usage

### 1. Create a Model

```yaml
apiVersion: models.example.com/v1alpha1
kind: Model
metadata:
  name: llama-3-8b
  namespace: default
spec:
  source:
    huggingFace:
      repoId: meta-llama/Llama-3.1-8B-Instruct
      revision: main
  version: "3.1"
  storage:
    storageClass: longhorn
    size: 20Gi
  credentialsSecret: hf-credentials
```

### 2. Check Status

```bash
$ kubectl get models
NAME         PHASE         VERSION   SIZE   AGE
llama-3-8b   Downloading   3.1       20Gi   2m

$ kubectl get models llama-3-8b -o jsonpath='{.status}'
{"phase":"Downloading","pvcName":"model-llama-3-8b","message":"Download in progress"}
```

### 3. Use in Workload

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vllm-server
spec:
  template:
    metadata:
      annotations:
        models.example.com/inject: "llama-3-8b"
    spec:
      containers:
        - name: vllm
          image: vllm/vllm-openai:latest
          args: ["--model", "/models/llama-3-8b"]
          resources:
            limits:
              nvidia.com/gpu: "1"
```

### 4. Resulting Pod (after webhook mutation)

```yaml
spec:
  containers:
    - name: vllm
      env:
        - name: MODEL_LLAMA_3_8B_NAME
          value: "llama-3-8b"
        - name: MODEL_LLAMA_3_8B_VERSION
          value: "3.1"
        - name: MODEL_LLAMA_3_8B_SOURCE_TYPE
          value: "huggingface"
        - name: MODEL_LLAMA_3_8B_REPO_ID
          value: "meta-llama/Llama-3.1-8B-Instruct"
        - name: MODEL_LLAMA_3_8B_MOUNT_PATH
          value: "/models/llama-3-8b"
      volumeMounts:
        - name: model-llama-3-8b
          mountPath: /models/llama-3-8b
          readOnly: true
  volumes:
    - name: model-llama-3-8b
      persistentVolumeClaim:
        claimName: model-llama-3-8b
        readOnly: true
```

---

## Quick Start Commands

```bash
# Initialize project
kubebuilder init --domain example.com --repo github.com/yourorg/model-operator

# Create API
kubebuilder create api --group models --version v1alpha1 --kind Model

# Create webhook
kubebuilder create webhook --group models --version v1alpha1 --kind Model --mutating --defaulting=false

# Generate manifests
make manifests

# Install CRDs
make install

# Run locally
make run

# Build and push image
make docker-build docker-push IMG=yourregistry/model-operator:latest

# Deploy to cluster
make deploy IMG=yourregistry/model-operator:latest
```
