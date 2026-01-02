/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	modelsv1alpha1 "github.com/rsJames-ttrpg/model-operator/api/v1alpha1"
	"github.com/rsJames-ttrpg/model-operator/internal/resources"
)

// Annotation keys
const (
	AnnotationInject    = "models.main-currents.news/inject"
	AnnotationMountPath = "models.main-currents.news/mount-path"
	AnnotationReadOnly  = "models.main-currents.news/read-only"
	AnnotationContainer = "models.main-currents.news/container"
	AnnotationInjectEnv = "models.main-currents.news/inject-env"

	LabelInjected = "models.main-currents.news/injected"
)

// injectionOptions holds parsed annotation values
type injectionOptions struct {
	MountPath     string
	ReadOnly      bool
	ContainerName string
	InjectEnv     bool
}

// ModelInjector handles pod mutation for model injection
// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=ignore,sideEffects=None,groups="",resources=pods,verbs=create,versions=v1,name=model-injector.models.main-currents.news,admissionReviewVersions=v1

type ModelInjector struct {
	Client  client.Client
	Decoder admission.Decoder
}

// Handle processes admission requests for pods
func (m *ModelInjector) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx).WithName("model-injector")

	pod := &corev1.Pod{}
	if err := m.Decoder.Decode(req, pod); err != nil {
		log.Error(err, "Failed to decode pod")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Check if already injected
	if pod.Labels != nil && pod.Labels[LabelInjected] == "true" {
		return admission.Allowed("already injected")
	}

	// Check for injection annotation
	if pod.Annotations == nil {
		return admission.Allowed("no injection requested")
	}

	injectAnnotation, ok := pod.Annotations[AnnotationInject]
	if !ok || injectAnnotation == "" {
		return admission.Allowed("no injection requested")
	}

	// Parse options
	opts := parseOptions(pod.Annotations)

	// Parse model names
	modelNames := strings.Split(injectAnnotation, ",")

	log.Info("Processing pod for model injection",
		"pod", req.Name,
		"namespace", req.Namespace,
		"models", modelNames)

	// Process each model
	for _, name := range modelNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		// Fetch Model CR
		model := &modelsv1alpha1.Model{}
		if err := m.Client.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: req.Namespace,
		}, model); err != nil {
			log.Error(err, "Failed to get model", "model", name)
			return admission.Denied(fmt.Sprintf("model %q not found: %v", name, err))
		}

		// Verify model is Ready
		if model.Status.Phase != modelsv1alpha1.ModelPhaseReady {
			log.Info("Model not ready", "model", name, "phase", model.Status.Phase)
			return admission.Denied(fmt.Sprintf("model %q is not ready (phase: %s)", name, model.Status.Phase))
		}

		// Inject volume
		injectVolume(pod, model)

		// Inject volume mount
		if err := injectVolumeMount(pod, model, opts); err != nil {
			log.Error(err, "Failed to inject volume mount", "model", name)
			return admission.Denied(fmt.Sprintf("failed to inject volume mount for model %q: %v", name, err))
		}

		// Inject environment variables if enabled
		if opts.InjectEnv {
			if err := injectEnvVars(pod, model, opts); err != nil {
				log.Error(err, "Failed to inject env vars", "model", name)
				return admission.Denied(fmt.Sprintf("failed to inject env vars for model %q: %v", name, err))
			}
		}
	}

	// Add label to mark injection
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[LabelInjected] = "true"

	// Marshal the modified pod
	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		log.Error(err, "Failed to marshal pod")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	log.Info("Successfully injected models into pod", "pod", req.Name)
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

// parseOptions extracts injection options from pod annotations
func parseOptions(annotations map[string]string) injectionOptions {
	opts := injectionOptions{
		ReadOnly:  true, // Default to read-only
		InjectEnv: true, // Default to inject env vars
	}

	if v, ok := annotations[AnnotationMountPath]; ok {
		opts.MountPath = v
	}

	if v, ok := annotations[AnnotationReadOnly]; ok {
		opts.ReadOnly = v != "false"
	}

	if v, ok := annotations[AnnotationContainer]; ok {
		opts.ContainerName = v
	}

	if v, ok := annotations[AnnotationInjectEnv]; ok {
		opts.InjectEnv = v != "false"
	}

	return opts
}

// injectVolume adds the model PVC volume to the pod
func injectVolume(pod *corev1.Pod, model *modelsv1alpha1.Model) {
	volumeName := resources.VolumeName(model.Name)
	pvcName := resources.PVCName(model.Name)

	// Check if volume already exists
	for _, v := range pod.Spec.Volumes {
		if v.Name == volumeName {
			return
		}
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
				ReadOnly:  true,
			},
		},
	})
}

// injectVolumeMount adds the volume mount to the target container
func injectVolumeMount(pod *corev1.Pod, model *modelsv1alpha1.Model, opts injectionOptions) error {
	if len(pod.Spec.Containers) == 0 {
		return fmt.Errorf("pod has no containers")
	}

	volumeName := resources.VolumeName(model.Name)

	// Determine mount path
	mountPath := opts.MountPath
	if mountPath == "" {
		mountPath = resources.DefaultMountPath(model.Name)
	} else if strings.Contains(opts.MountPath, "{name}") {
		// Replace {name} placeholder
		mountPath = strings.ReplaceAll(opts.MountPath, "{name}", model.Name)
	} else if !strings.HasSuffix(mountPath, model.Name) {
		// If custom base path specified, append model name
		mountPath = strings.TrimSuffix(mountPath, "/") + "/" + model.Name
	}

	mount := corev1.VolumeMount{
		Name:      volumeName,
		MountPath: mountPath,
		ReadOnly:  opts.ReadOnly,
	}

	// Find target container
	containerIdx := 0
	if opts.ContainerName != "" {
		found := false
		for i, c := range pod.Spec.Containers {
			if c.Name == opts.ContainerName {
				containerIdx = i
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("container %q not found", opts.ContainerName)
		}
	}

	// Check if mount already exists
	for _, m := range pod.Spec.Containers[containerIdx].VolumeMounts {
		if m.Name == volumeName {
			return nil
		}
	}

	pod.Spec.Containers[containerIdx].VolumeMounts = append(
		pod.Spec.Containers[containerIdx].VolumeMounts,
		mount,
	)

	return nil
}

// injectEnvVars adds model-related environment variables to the target container
func injectEnvVars(pod *corev1.Pod, model *modelsv1alpha1.Model, opts injectionOptions) error {
	if len(pod.Spec.Containers) == 0 {
		return fmt.Errorf("pod has no containers")
	}

	prefix := resources.EnvVarPrefix(model.Name)

	// Determine mount path for env var
	mountPath := opts.MountPath
	if mountPath == "" {
		mountPath = resources.DefaultMountPath(model.Name)
	} else if strings.Contains(opts.MountPath, "{name}") {
		mountPath = strings.ReplaceAll(opts.MountPath, "{name}", model.Name)
	} else if !strings.HasSuffix(mountPath, model.Name) {
		mountPath = strings.TrimSuffix(mountPath, "/") + "/" + model.Name
	}

	// Build env vars
	envVars := []corev1.EnvVar{
		{Name: prefix + "_NAME", Value: model.Name},
		{Name: prefix + "_MOUNT_PATH", Value: mountPath},
	}

	// Add version if set
	if model.Spec.Version != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  prefix + "_VERSION",
			Value: model.Spec.Version,
		})
	}

	// Add source-specific env vars
	source := model.Spec.Source
	switch {
	case source.HuggingFace != nil:
		envVars = append(envVars,
			corev1.EnvVar{Name: prefix + "_SOURCE_TYPE", Value: "huggingface"},
			corev1.EnvVar{Name: prefix + "_REPO_ID", Value: source.HuggingFace.RepoID},
		)
	case source.S3 != nil:
		envVars = append(envVars,
			corev1.EnvVar{Name: prefix + "_SOURCE_TYPE", Value: "s3"},
			corev1.EnvVar{Name: prefix + "_BUCKET", Value: source.S3.Bucket},
		)
	case source.URL != nil:
		envVars = append(envVars,
			corev1.EnvVar{Name: prefix + "_SOURCE_TYPE", Value: "url"},
			corev1.EnvVar{Name: prefix + "_URL", Value: source.URL.URL},
		)
	}

	// Find target container
	containerIdx := 0
	if opts.ContainerName != "" {
		for i, c := range pod.Spec.Containers {
			if c.Name == opts.ContainerName {
				containerIdx = i
				break
			}
		}
	}

	// Add env vars (skip if already exists)
	existingEnvNames := make(map[string]bool)
	for _, e := range pod.Spec.Containers[containerIdx].Env {
		existingEnvNames[e.Name] = true
	}

	for _, env := range envVars {
		if !existingEnvNames[env.Name] {
			pod.Spec.Containers[containerIdx].Env = append(
				pod.Spec.Containers[containerIdx].Env,
				env,
			)
		}
	}

	return nil
}
