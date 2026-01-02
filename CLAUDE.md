# CLAUDE.md - Model Operator

## Project Overview

Build a Kubernetes operator in Go that manages ML model files on PVCs with automatic injection into workloads.

## Quick Reference

- **Language**: Go 1.22+
- **Framework**: kubebuilder / controller-runtime
- **API Group**: `models.main-currents.news`
- **API Version**: `v1alpha1`
- **Kind**: `Model`

## Core Components

### 1. CRD (`api/v1alpha1/model_types.go`)

```go
// ModelSpec defines the desired state
type ModelSpec struct {
    Source            ModelSource       `json:"source"`
    Storage           StorageSpec       `json:"storage"`
    Version           string            `json:"version,omitempty"`
    CredentialsSecret string            `json:"credentialsSecret,omitempty"`
    NodeSelector      map[string]string `json:"nodeSelector,omitempty"`
}

// ModelSource - exactly ONE field must be set
type ModelSource struct {
    HuggingFace *HuggingFaceSource `json:"huggingFace,omitempty"`
    URL         *URLSource         `json:"url,omitempty"`
    S3          *S3Source          `json:"s3,omitempty"`
}

// ModelStatus defines the observed state
type ModelStatus struct {
    Phase              ModelPhase         `json:"phase,omitempty"`
    PVCName            string             `json:"pvcName,omitempty"`
    Message            string             `json:"message,omitempty"`
    Progress           int                `json:"progress,omitempty"`
    Conditions         []metav1.Condition `json:"conditions,omitempty"`
    ObservedGeneration int64              `json:"observedGeneration,omitempty"`
}

type ModelPhase string
const (
    ModelPhasePending     ModelPhase = "Pending"
    ModelPhaseDownloading ModelPhase = "Downloading"
    ModelPhaseReady       ModelPhase = "Ready"
    ModelPhaseFailed      ModelPhase = "Failed"
)
```

### 2. Controller (`internal/controller/model_controller.go`)

State machine:
- `Pending` → Create PVC + Job → `Downloading`
- `Downloading` → Watch Job → `Ready` or `Failed`
- `Ready` → Verify PVC exists → Stay or reset to `Pending`
- `Failed` → If Job deleted → `Pending` (retry)

Key methods:
```go
func (r *ModelReconciler) Reconcile(ctx, req) (ctrl.Result, error)
func (r *ModelReconciler) reconcilePending(ctx, model) (ctrl.Result, error)
func (r *ModelReconciler) reconcileDownloading(ctx, model) (ctrl.Result, error)
func (r *ModelReconciler) reconcileReady(ctx, model) (ctrl.Result, error)
func (r *ModelReconciler) reconcileFailed(ctx, model) (ctrl.Result, error)
func (r *ModelReconciler) updateStatus(ctx, model, phase, message) error
```

### 3. Webhook (`internal/webhook/model_injector.go`)

Mutating webhook that:
1. Intercepts Pod CREATE
2. Checks for annotation `models.example.com/inject`
3. Looks up Model CRs, verifies Ready state
4. Patches Pod with volumes, mounts, env vars

Annotations:
- `models.example.com/inject` - required, comma-separated model names
- `models.example.com/mount-path` - optional, default `/models/{name}`
- `models.example.com/read-only` - optional, default `"true"`
- `models.example.com/container` - optional, default first container
- `models.example.com/inject-env` - optional, default `"true"`

### 4. Resource Builders (`internal/resources/`)

```go
// naming.go
func PVCName(modelName string) string { return "model-" + modelName }
func JobName(modelName string) string { return "model-download-" + modelName }
func EnvVarPrefix(modelName string) string // MODEL_LLAMA_3_8B

// pvc.go
func BuildPVC(model *v1alpha1.Model) *corev1.PersistentVolumeClaim

// job.go
func BuildDownloadJob(model *v1alpha1.Model) *batchv1.Job
func buildHuggingFaceJob(model) *batchv1.Job
func buildS3Job(model) *batchv1.Job
func buildURLJob(model) *batchv1.Job
```

## Implementation Order

1. **Scaffold project**
   ```bash
   kubebuilder init --domain example.com --repo github.com/yourorg/model-operator
   kubebuilder create api --group models --version v1alpha1 --kind Model
   ```

2. **Define types** in `api/v1alpha1/model_types.go`

3. **Implement resource builders** in `internal/resources/`

4. **Implement controller** in `internal/controller/model_controller.go`

5. **Create webhook**
   ```bash
   kubebuilder create webhook --group "" --version v1 --kind Pod --mutating --defaulting=false
   ```

6. **Implement webhook** in `internal/webhook/model_injector.go`

7. **Write tests**

8. **Generate manifests**: `make manifests`

## Key Behaviors

### PVC Creation
- Name: `model-{modelName}`
- Owner reference to Model (garbage collection)
- Storage class and size from spec

### Job Creation
- Name: `model-download-{modelName}`
- Owner reference to Model
- `backoffLimit: 3`
- `ttlSecondsAfterFinished: 3600`
- Image depends on source type:
  - HuggingFace: `python:3.11-slim`
  - S3: `amazon/aws-cli:latest`
  - URL: `curlimages/curl:latest`

### Webhook Injection
- Only triggers on `models.example.com/inject` annotation
- Denies if Model not found or not Ready
- Adds label `models.example.com/injected: "true"` to prevent re-processing
- Patches: volumes, volumeMounts, env vars

## Testing Commands

```bash
# Run locally against cluster
make run

# Run tests
make test

# Generate CRD manifests
make manifests

# Install CRDs
make install

# Build image
make docker-build IMG=myregistry/model-operator:latest
```

## Sample Resources

### Model
```yaml
apiVersion: models.example.com/v1alpha1
kind: Model
metadata:
  name: llama-3-8b
spec:
  source:
    huggingFace:
      repoId: meta-llama/Llama-3.1-8B-Instruct
  version: "3.1"
  storage:
    storageClass: longhorn
    size: 20Gi
```

### Pod with Injection
```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    models.example.com/inject: "llama-3-8b"
spec:
  containers:
    - name: app
      image: myapp:latest
```

## Common Errors

| Error | Cause | Fix |
|-------|-------|-----|
| "model not found" | Model CR doesn't exist | Create Model first |
| "model not ready" | Download incomplete | Wait for Ready phase |
| Webhook timeout | Model lookup slow | Check RBAC, network |
| PVC stuck Pending | StorageClass issue | Verify storage class exists |
| Job fails | Download error | Check job logs, credentials |

## Files to Create

```
api/v1alpha1/
├── model_types.go          # CRD definitions
├── groupversion_info.go    # Generated, edit group
└── zz_generated.deepcopy.go # Generated

internal/
├── controller/
│   ├── model_controller.go
│   └── model_controller_test.go
├── webhook/
│   ├── model_injector.go
│   └── model_injector_test.go
└── resources/
    ├── naming.go
    ├── pvc.go
    └── job.go

config/
├── crd/bases/              # Generated
├── rbac/                   # Generated + customized
├── webhook/                # Generated
└── samples/                # Example YAMLs
```
