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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelsv1alpha1 "github.com/rsJames-ttrpg/model-operator/api/v1alpha1"
	"github.com/rsJames-ttrpg/model-operator/internal/resources"
)

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantOpts    injectionOptions
	}{
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			wantOpts: injectionOptions{
				ReadOnly:  true,
				InjectEnv: true,
			},
		},
		{
			name: "custom mount path",
			annotations: map[string]string{
				AnnotationMountPath: "/custom/models",
			},
			wantOpts: injectionOptions{
				MountPath: "/custom/models",
				ReadOnly:  true,
				InjectEnv: true,
			},
		},
		{
			name: "read-write mount",
			annotations: map[string]string{
				AnnotationReadOnly: "false",
			},
			wantOpts: injectionOptions{
				ReadOnly:  false,
				InjectEnv: true,
			},
		},
		{
			name: "disable env injection",
			annotations: map[string]string{
				AnnotationInjectEnv: "false",
			},
			wantOpts: injectionOptions{
				ReadOnly:  true,
				InjectEnv: false,
			},
		},
		{
			name: "target specific container",
			annotations: map[string]string{
				AnnotationContainer: "sidecar",
			},
			wantOpts: injectionOptions{
				ContainerName: "sidecar",
				ReadOnly:      true,
				InjectEnv:     true,
			},
		},
		{
			name: "all options",
			annotations: map[string]string{
				AnnotationMountPath: "/data/models",
				AnnotationReadOnly:  "false",
				AnnotationContainer: "inference",
				AnnotationInjectEnv: "true",
			},
			wantOpts: injectionOptions{
				MountPath:     "/data/models",
				ReadOnly:      false,
				ContainerName: "inference",
				InjectEnv:     true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := parseOptions(tt.annotations)

			if opts.MountPath != tt.wantOpts.MountPath {
				t.Errorf("MountPath = %v, want %v", opts.MountPath, tt.wantOpts.MountPath)
			}
			if opts.ReadOnly != tt.wantOpts.ReadOnly {
				t.Errorf("ReadOnly = %v, want %v", opts.ReadOnly, tt.wantOpts.ReadOnly)
			}
			if opts.ContainerName != tt.wantOpts.ContainerName {
				t.Errorf("ContainerName = %v, want %v", opts.ContainerName, tt.wantOpts.ContainerName)
			}
			if opts.InjectEnv != tt.wantOpts.InjectEnv {
				t.Errorf("InjectEnv = %v, want %v", opts.InjectEnv, tt.wantOpts.InjectEnv)
			}
		})
	}
}

func TestInjectVolume(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model",
			Namespace: "default",
		},
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{},
		},
	}

	injectVolume(pod, model)

	if len(pod.Spec.Volumes) != 1 {
		t.Fatalf("Expected 1 volume, got %d", len(pod.Spec.Volumes))
	}

	vol := pod.Spec.Volumes[0]
	expectedName := resources.VolumeName(model.Name)
	if vol.Name != expectedName {
		t.Errorf("Volume name = %v, want %v", vol.Name, expectedName)
	}

	if vol.PersistentVolumeClaim == nil {
		t.Fatal("Expected PVC volume source")
	}

	expectedPVC := resources.PVCName(model.Name)
	if vol.PersistentVolumeClaim.ClaimName != expectedPVC {
		t.Errorf("PVC name = %v, want %v", vol.PersistentVolumeClaim.ClaimName, expectedPVC)
	}
}

func TestInjectVolume_NoDuplicate(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model",
			Namespace: "default",
		},
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: resources.VolumeName(model.Name),
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: resources.PVCName(model.Name),
						},
					},
				},
			},
		},
	}

	injectVolume(pod, model)

	if len(pod.Spec.Volumes) != 1 {
		t.Errorf("Expected 1 volume (no duplicate), got %d", len(pod.Spec.Volumes))
	}
}

func TestInjectVolumeMount(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model",
			Namespace: "default",
		},
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:         "main",
					VolumeMounts: []corev1.VolumeMount{},
				},
			},
		},
	}

	opts := injectionOptions{
		ReadOnly: true,
	}

	err := injectVolumeMount(pod, model, opts)
	if err != nil {
		t.Fatalf("injectVolumeMount() error = %v", err)
	}

	if len(pod.Spec.Containers[0].VolumeMounts) != 1 {
		t.Fatalf("Expected 1 volume mount, got %d", len(pod.Spec.Containers[0].VolumeMounts))
	}

	mount := pod.Spec.Containers[0].VolumeMounts[0]
	expectedName := resources.VolumeName(model.Name)
	if mount.Name != expectedName {
		t.Errorf("VolumeMount name = %v, want %v", mount.Name, expectedName)
	}

	expectedPath := resources.DefaultMountPath(model.Name)
	if mount.MountPath != expectedPath {
		t.Errorf("MountPath = %v, want %v", mount.MountPath, expectedPath)
	}

	if !mount.ReadOnly {
		t.Error("Expected ReadOnly = true")
	}
}

func TestInjectVolumeMount_CustomPath(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model",
			Namespace: "default",
		},
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:         "main",
					VolumeMounts: []corev1.VolumeMount{},
				},
			},
		},
	}

	opts := injectionOptions{
		MountPath: "/custom/path",
		ReadOnly:  false,
	}

	err := injectVolumeMount(pod, model, opts)
	if err != nil {
		t.Fatalf("injectVolumeMount() error = %v", err)
	}

	mount := pod.Spec.Containers[0].VolumeMounts[0]
	// Custom path should have model name appended
	expectedPath := "/custom/path/test-model"
	if mount.MountPath != expectedPath {
		t.Errorf("MountPath = %v, want %v", mount.MountPath, expectedPath)
	}

	if mount.ReadOnly {
		t.Error("Expected ReadOnly = false")
	}
}

func TestInjectVolumeMount_TargetContainer(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model",
			Namespace: "default",
		},
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:         "main",
					VolumeMounts: []corev1.VolumeMount{},
				},
				{
					Name:         "sidecar",
					VolumeMounts: []corev1.VolumeMount{},
				},
			},
		},
	}

	opts := injectionOptions{
		ContainerName: "sidecar",
		ReadOnly:      true,
	}

	err := injectVolumeMount(pod, model, opts)
	if err != nil {
		t.Fatalf("injectVolumeMount() error = %v", err)
	}

	// Main container should have no mounts
	if len(pod.Spec.Containers[0].VolumeMounts) != 0 {
		t.Errorf("Main container should have 0 mounts, got %d", len(pod.Spec.Containers[0].VolumeMounts))
	}

	// Sidecar container should have the mount
	if len(pod.Spec.Containers[1].VolumeMounts) != 1 {
		t.Errorf("Sidecar container should have 1 mount, got %d", len(pod.Spec.Containers[1].VolumeMounts))
	}
}

func TestInjectVolumeMount_ContainerNotFound(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model",
			Namespace: "default",
		},
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "main",
				},
			},
		},
	}

	opts := injectionOptions{
		ContainerName: "nonexistent",
	}

	err := injectVolumeMount(pod, model, opts)
	if err == nil {
		t.Error("Expected error for nonexistent container")
	}
}

func TestInjectVolumeMount_NoContainers(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model",
			Namespace: "default",
		},
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{},
		},
	}

	opts := injectionOptions{}

	err := injectVolumeMount(pod, model, opts)
	if err == nil {
		t.Error("Expected error for pod with no containers")
	}
}

func TestInjectEnvVars(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model",
			Namespace: "default",
		},
		Spec: modelsv1alpha1.ModelSpec{
			Version: "1.0",
			Source: modelsv1alpha1.ModelSource{
				HuggingFace: &modelsv1alpha1.HuggingFaceSource{
					RepoID: "org/model-name",
				},
			},
		},
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "main",
					Env:  []corev1.EnvVar{},
				},
			},
		},
	}

	opts := injectionOptions{
		InjectEnv: true,
	}

	err := injectEnvVars(pod, model, opts)
	if err != nil {
		t.Fatalf("injectEnvVars() error = %v", err)
	}

	env := pod.Spec.Containers[0].Env
	if len(env) == 0 {
		t.Fatal("Expected env vars to be injected")
	}

	// Check for expected env vars
	envMap := make(map[string]string)
	for _, e := range env {
		envMap[e.Name] = e.Value
	}

	prefix := resources.EnvVarPrefix(model.Name)

	if _, ok := envMap[prefix+"_NAME"]; !ok {
		t.Errorf("Expected %s_NAME env var", prefix)
	}

	if _, ok := envMap[prefix+"_VERSION"]; !ok {
		t.Errorf("Expected %s_VERSION env var", prefix)
	}

	if _, ok := envMap[prefix+"_SOURCE_TYPE"]; !ok {
		t.Errorf("Expected %s_SOURCE_TYPE env var", prefix)
	}

	if envMap[prefix+"_SOURCE_TYPE"] != "huggingface" {
		t.Errorf("SOURCE_TYPE = %v, want huggingface", envMap[prefix+"_SOURCE_TYPE"])
	}

	if _, ok := envMap[prefix+"_REPO_ID"]; !ok {
		t.Errorf("Expected %s_REPO_ID env var", prefix)
	}
}

func TestInjectEnvVars_S3Source(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "s3-model",
			Namespace: "default",
		},
		Spec: modelsv1alpha1.ModelSpec{
			Source: modelsv1alpha1.ModelSource{
				S3: &modelsv1alpha1.S3Source{
					Bucket: "my-bucket",
					Key:    "models/test",
				},
			},
		},
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "main",
					Env:  []corev1.EnvVar{},
				},
			},
		},
	}

	opts := injectionOptions{
		InjectEnv: true,
	}

	err := injectEnvVars(pod, model, opts)
	if err != nil {
		t.Fatalf("injectEnvVars() error = %v", err)
	}

	env := pod.Spec.Containers[0].Env
	envMap := make(map[string]string)
	for _, e := range env {
		envMap[e.Name] = e.Value
	}

	prefix := resources.EnvVarPrefix(model.Name)

	if envMap[prefix+"_SOURCE_TYPE"] != "s3" {
		t.Errorf("SOURCE_TYPE = %v, want s3", envMap[prefix+"_SOURCE_TYPE"])
	}

	if envMap[prefix+"_BUCKET"] != "my-bucket" {
		t.Errorf("BUCKET = %v, want my-bucket", envMap[prefix+"_BUCKET"])
	}
}
