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

package resources

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelsv1alpha1 "github.com/rsJames-ttrpg/model-operator/api/v1alpha1"
)

func TestBuildDownloadJob_HuggingFace(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "llama-3-8b",
			Namespace: "default",
		},
		Spec: modelsv1alpha1.ModelSpec{
			Source: modelsv1alpha1.ModelSource{
				HuggingFace: &modelsv1alpha1.HuggingFaceSource{
					RepoID:   "meta-llama/Llama-3.1-8B-Instruct",
					Revision: "main",
				},
			},
			Storage: modelsv1alpha1.StorageSpec{
				StorageClass: "longhorn",
				Size:         "20Gi",
			},
		},
	}

	job, err := BuildDownloadJob(model)
	if err != nil {
		t.Fatalf("BuildDownloadJob() error = %v", err)
	}

	if job.Name != "model-download-llama-3-8b" {
		t.Errorf("Job name = %v, want model-download-llama-3-8b", job.Name)
	}

	if job.Namespace != "default" {
		t.Errorf("Job namespace = %v, want default", job.Namespace)
	}

	// Check container image
	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != huggingFaceImage {
		t.Errorf("Container image = %v, want %v", container.Image, huggingFaceImage)
	}

	// Check that script contains the repo ID
	script := container.Args[0]
	if !strings.Contains(script, "meta-llama/Llama-3.1-8B-Instruct") {
		t.Errorf("Script should contain repo ID")
	}

	// Check volume mount
	if len(container.VolumeMounts) == 0 {
		t.Errorf("Expected volume mount")
	}
	if container.VolumeMounts[0].MountPath != "/models" {
		t.Errorf("Mount path = %v, want /models", container.VolumeMounts[0].MountPath)
	}
}

func TestBuildDownloadJob_HuggingFace_WithFilters(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "llama-filtered",
			Namespace: "default",
		},
		Spec: modelsv1alpha1.ModelSpec{
			Source: modelsv1alpha1.ModelSource{
				HuggingFace: &modelsv1alpha1.HuggingFaceSource{
					RepoID:   "meta-llama/Llama-3.1-8B-Instruct",
					Revision: "main",
					Include:  []string{"*.safetensors", "*.json"},
					Exclude:  []string{"*.bin"},
				},
			},
			Storage: modelsv1alpha1.StorageSpec{
				StorageClass: "longhorn",
				Size:         "20Gi",
			},
		},
	}

	job, err := BuildDownloadJob(model)
	if err != nil {
		t.Fatalf("BuildDownloadJob() error = %v", err)
	}

	script := job.Spec.Template.Spec.Containers[0].Args[0]

	// Check include patterns
	if !strings.Contains(script, "allow_patterns") {
		t.Errorf("Script should contain allow_patterns for include filters")
	}
	if !strings.Contains(script, "*.safetensors") {
		t.Errorf("Script should contain safetensors pattern")
	}

	// Check exclude patterns
	if !strings.Contains(script, "ignore_patterns") {
		t.Errorf("Script should contain ignore_patterns for exclude filters")
	}
}

func TestBuildDownloadJob_S3(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "s3-model",
			Namespace: "default",
		},
		Spec: modelsv1alpha1.ModelSpec{
			Source: modelsv1alpha1.ModelSource{
				S3: &modelsv1alpha1.S3Source{
					Bucket:   "my-bucket",
					Key:      "models/llama/",
					Region:   "us-east-1",
					Endpoint: "https://s3.amazonaws.com",
				},
			},
			Storage: modelsv1alpha1.StorageSpec{
				StorageClass: "gp3",
				Size:         "50Gi",
			},
		},
	}

	job, err := BuildDownloadJob(model)
	if err != nil {
		t.Fatalf("BuildDownloadJob() error = %v", err)
	}

	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != s3Image {
		t.Errorf("Container image = %v, want %v", container.Image, s3Image)
	}

	script := container.Args[0]
	if !strings.Contains(script, "s3://my-bucket/models/llama/") {
		t.Errorf("Script should contain S3 path")
	}
	if !strings.Contains(script, "--region us-east-1") {
		t.Errorf("Script should contain region")
	}
}

func TestBuildDownloadJob_URL(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "url-model",
			Namespace: "default",
		},
		Spec: modelsv1alpha1.ModelSpec{
			Source: modelsv1alpha1.ModelSource{
				URL: &modelsv1alpha1.URLSource{
					URL: "https://example.com/model.gguf",
				},
			},
			Storage: modelsv1alpha1.StorageSpec{
				StorageClass: "local-path",
				Size:         "5Gi",
			},
		},
	}

	job, err := BuildDownloadJob(model)
	if err != nil {
		t.Fatalf("BuildDownloadJob() error = %v", err)
	}

	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != urlImage {
		t.Errorf("Container image = %v, want %v", container.Image, urlImage)
	}

	script := container.Args[0]
	if !strings.Contains(script, "https://example.com/model.gguf") {
		t.Errorf("Script should contain URL")
	}
	if !strings.Contains(script, "curl") {
		t.Errorf("Script should use curl")
	}
}

func TestBuildDownloadJob_Git(t *testing.T) {
	lfsEnabled := true
	depth := 1

	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "git-model",
			Namespace: "default",
		},
		Spec: modelsv1alpha1.ModelSpec{
			Source: modelsv1alpha1.ModelSource{
				Git: &modelsv1alpha1.GitSource{
					URL:   "https://github.com/example/model.git",
					Ref:   "v1.0.0",
					LFS:   &lfsEnabled,
					Depth: &depth,
				},
			},
			Storage: modelsv1alpha1.StorageSpec{
				StorageClass: "local-path",
				Size:         "10Gi",
			},
		},
	}

	job, err := BuildDownloadJob(model)
	if err != nil {
		t.Fatalf("BuildDownloadJob() error = %v", err)
	}

	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != gitImage {
		t.Errorf("Container image = %v, want %v", container.Image, gitImage)
	}

	script := container.Args[0]
	if !strings.Contains(script, "git clone") {
		t.Errorf("Script should contain git clone")
	}
	if !strings.Contains(script, "--branch v1.0.0") {
		t.Errorf("Script should contain branch/ref")
	}
	if !strings.Contains(script, "git-lfs") {
		t.Errorf("Script should install git-lfs when LFS is enabled")
	}
	if !strings.Contains(script, "--depth 1") {
		t.Errorf("Script should contain depth argument")
	}
}

func TestBuildDownloadJob_NoSource(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-source",
			Namespace: "default",
		},
		Spec: modelsv1alpha1.ModelSpec{
			Source: modelsv1alpha1.ModelSource{},
			Storage: modelsv1alpha1.StorageSpec{
				StorageClass: "local-path",
				Size:         "1Gi",
			},
		},
	}

	_, err := BuildDownloadJob(model)
	if err == nil {
		t.Errorf("Expected error for model with no source")
	}
}

func TestBuildDownloadJob_WithCredentials(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "private-model",
			Namespace: "default",
		},
		Spec: modelsv1alpha1.ModelSpec{
			Source: modelsv1alpha1.ModelSource{
				HuggingFace: &modelsv1alpha1.HuggingFaceSource{
					RepoID: "private-org/private-model",
				},
			},
			Storage: modelsv1alpha1.StorageSpec{
				StorageClass: "longhorn",
				Size:         "20Gi",
			},
			CredentialsSecret: "hf-token",
		},
	}

	job, err := BuildDownloadJob(model)
	if err != nil {
		t.Fatalf("BuildDownloadJob() error = %v", err)
	}

	container := job.Spec.Template.Spec.Containers[0]

	// Check for HF_TOKEN env var
	foundToken := false
	for _, env := range container.Env {
		if env.Name == "HF_TOKEN" {
			foundToken = true
			if env.ValueFrom.SecretKeyRef.Name != "hf-token" {
				t.Errorf("Secret name = %v, want hf-token", env.ValueFrom.SecretKeyRef.Name)
			}
		}
	}
	if !foundToken {
		t.Errorf("Expected HF_TOKEN env var")
	}
}

func TestBuildDownloadJob_WithNodeSelector(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gpu-model",
			Namespace: "default",
		},
		Spec: modelsv1alpha1.ModelSpec{
			Source: modelsv1alpha1.ModelSource{
				HuggingFace: &modelsv1alpha1.HuggingFaceSource{
					RepoID: "meta-llama/Llama-3.1-8B-Instruct",
				},
			},
			Storage: modelsv1alpha1.StorageSpec{
				StorageClass: "longhorn",
				Size:         "20Gi",
			},
			NodeSelector: map[string]string{
				"node-type": "gpu",
			},
		},
	}

	job, err := BuildDownloadJob(model)
	if err != nil {
		t.Fatalf("BuildDownloadJob() error = %v", err)
	}

	nodeSelector := job.Spec.Template.Spec.NodeSelector
	if nodeSelector["node-type"] != "gpu" {
		t.Errorf("NodeSelector not applied correctly")
	}
}

func TestBuildModelfileContent(t *testing.T) {
	temperature := "0.7"
	topK := 40

	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-model",
		},
		Spec: modelsv1alpha1.ModelSpec{
			Source: modelsv1alpha1.ModelSource{
				HuggingFace: &modelsv1alpha1.HuggingFaceSource{
					RepoID: "meta-llama/Llama-3.1-8B-Instruct",
				},
			},
			Modelfile: &modelsv1alpha1.ModelfileSpec{
				Template: "{{ .System }}\n{{ .Prompt }}",
				System:   "You are a helpful assistant.",
				Parameters: &modelsv1alpha1.ModelParameters{
					Temperature: &temperature,
					TopK:        &topK,
					Stop:        []string{"</s>", "<|end|>"},
				},
			},
		},
	}

	content := buildModelfileContent(model)

	if !strings.Contains(content, "# HUGGINGFACE_PATH huggingface.co/meta-llama/Llama-3.1-8B-Instruct") {
		t.Errorf("Content should contain HUGGINGFACE_PATH")
	}
	if !strings.Contains(content, "FROM /models") {
		t.Errorf("Content should contain FROM directive")
	}
	if !strings.Contains(content, "TEMPLATE") {
		t.Errorf("Content should contain TEMPLATE")
	}
	if !strings.Contains(content, "SYSTEM") {
		t.Errorf("Content should contain SYSTEM")
	}
	if !strings.Contains(content, "PARAMETER temperature 0.7") {
		t.Errorf("Content should contain temperature parameter")
	}
	if !strings.Contains(content, "PARAMETER top_k 40") {
		t.Errorf("Content should contain top_k parameter")
	}
	if !strings.Contains(content, `PARAMETER stop "</s>"`) {
		t.Errorf("Content should contain stop parameter")
	}
}

func TestBuildModelfileContent_CustomPaths(t *testing.T) {
	model := &modelsv1alpha1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-model",
		},
		Spec: modelsv1alpha1.ModelSpec{
			Source: modelsv1alpha1.ModelSource{
				HuggingFace: &modelsv1alpha1.HuggingFaceSource{
					RepoID: "meta-llama/Llama-3.1-8B-Instruct",
				},
			},
			Modelfile: &modelsv1alpha1.ModelfileSpec{
				From:            "/custom/path",
				HuggingFacePath: "custom.hf.co/my-model",
			},
		},
	}

	content := buildModelfileContent(model)

	if !strings.Contains(content, "# HUGGINGFACE_PATH custom.hf.co/my-model") {
		t.Errorf("Content should contain custom HUGGINGFACE_PATH")
	}
	if !strings.Contains(content, "FROM /custom/path") {
		t.Errorf("Content should contain custom FROM path")
	}
}
