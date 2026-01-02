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
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	modelsv1alpha1 "github.com/rsJames-ttrpg/model-operator/api/v1alpha1"
)

const (
	// Job configuration
	backoffLimit            = int32(3)
	ttlSecondsAfterFinished = int32(3600)

	// Container images
	huggingFaceImage = "python:3.11-slim"
	s3Image          = "amazon/aws-cli:latest"
	urlImage         = "curlimages/curl:latest"
	gitImage         = "alpine/git:latest"

	// Volume and mount names
	modelVolumeName = "model-storage"
	modelMountPath  = "/models"
)

// BuildDownloadJob creates a Job to download the model based on the source type
func BuildDownloadJob(model *modelsv1alpha1.Model) (*batchv1.Job, error) {
	source := model.Spec.Source

	var container corev1.Container
	switch {
	case source.HuggingFace != nil:
		container = buildHuggingFaceContainer(model)
	case source.S3 != nil:
		container = buildS3Container(model)
	case source.URL != nil:
		container = buildURLContainer(model)
	case source.Git != nil:
		container = buildGitContainer(model)
	default:
		return nil, fmt.Errorf("no source specified in model %s", model.Name)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      JobName(model.Name),
			Namespace: model.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "model-downloader",
				"app.kubernetes.io/instance":   model.Name,
				"app.kubernetes.io/managed-by": "model-operator",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(backoffLimit),
			TTLSecondsAfterFinished: ptr.To(ttlSecondsAfterFinished),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":       "model-downloader",
						"app.kubernetes.io/instance":   model.Name,
						"app.kubernetes.io/managed-by": "model-operator",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers:    []corev1.Container{container},
					Volumes: []corev1.Volume{
						{
							Name: modelVolumeName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: PVCName(model.Name),
								},
							},
						},
					},
				},
			},
		},
	}

	// Apply node selector if specified
	if len(model.Spec.NodeSelector) > 0 {
		job.Spec.Template.Spec.NodeSelector = model.Spec.NodeSelector
	}

	return job, nil
}

func buildHuggingFaceContainer(model *modelsv1alpha1.Model) corev1.Container {
	hf := model.Spec.Source.HuggingFace
	revision := hf.Revision
	if revision == "" {
		revision = "main"
	}

	// Build snapshot_download kwargs
	kwargs := []string{
		fmt.Sprintf("'%s'", hf.RepoID),
		fmt.Sprintf("revision='%s'", revision),
		"local_dir='/models'",
	}

	// Add include patterns
	if len(hf.Include) > 0 {
		patterns := make([]string, len(hf.Include))
		for i, p := range hf.Include {
			patterns[i] = fmt.Sprintf("'%s'", p)
		}
		kwargs = append(kwargs, fmt.Sprintf("allow_patterns=[%s]", strings.Join(patterns, ", ")))
	}

	// Add exclude patterns
	if len(hf.Exclude) > 0 {
		patterns := make([]string, len(hf.Exclude))
		for i, p := range hf.Exclude {
			patterns[i] = fmt.Sprintf("'%s'", p)
		}
		kwargs = append(kwargs, fmt.Sprintf("ignore_patterns=[%s]", strings.Join(patterns, ", ")))
	}

	// Build the Python download command
	downloadCmd := fmt.Sprintf("from huggingface_hub import snapshot_download; snapshot_download(%s)",
		strings.Join(kwargs, ", "))

	// Build the Modelfile content
	modelfileContent := buildModelfileContent(model)

	script := fmt.Sprintf(`pip install -q huggingface_hub hf_transfer && \
export HF_HUB_ENABLE_HF_TRANSFER=1 && \
python -c "%s" && \
cat > /models/Modelfile << 'MODELFILE_EOF'
%s
MODELFILE_EOF
echo "Download complete" && \
ls -la /models`, downloadCmd, modelfileContent)

	container := corev1.Container{
		Name:    "downloader",
		Image:   huggingFaceImage,
		Command: []string{"sh", "-c"},
		Args:    []string{script},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      modelVolumeName,
				MountPath: modelMountPath,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("512Mi"),
				corev1.ResourceCPU:    resource.MustParse("500m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("2Gi"),
				corev1.ResourceCPU:    resource.MustParse("2"),
			},
		},
	}

	// Add HF_TOKEN from secret if specified
	if model.Spec.CredentialsSecret != "" {
		container.Env = append(container.Env, corev1.EnvVar{
			Name: "HF_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: model.Spec.CredentialsSecret,
					},
					Key:      "HF_TOKEN",
					Optional: ptr.To(true),
				},
			},
		})
	}

	return container
}

// buildModelfileContent generates Ollama-style Modelfile content
func buildModelfileContent(model *modelsv1alpha1.Model) string {
	var lines []string

	// Determine HuggingFace path (can be overridden in modelfile spec)
	var hfPath string
	if model.Spec.Modelfile != nil && model.Spec.Modelfile.HuggingFacePath != "" {
		hfPath = model.Spec.Modelfile.HuggingFacePath
	} else if model.Spec.Source.HuggingFace != nil {
		hfPath = fmt.Sprintf("huggingface.co/%s", model.Spec.Source.HuggingFace.RepoID)
	}

	// Determine FROM path (can be overridden in modelfile spec)
	fromPath := "/models"
	if model.Spec.Modelfile != nil && model.Spec.Modelfile.From != "" {
		fromPath = model.Spec.Modelfile.From
	}

	// Add source path comment
	if hfPath != "" {
		lines = append(lines, fmt.Sprintf("# HUGGINGFACE_PATH %s", hfPath))
	} else if model.Spec.Source.Git != nil {
		lines = append(lines, fmt.Sprintf("# GIT_URL %s", model.Spec.Source.Git.URL))
		if model.Spec.Source.Git.Ref != "" {
			lines = append(lines, fmt.Sprintf("# GIT_REF %s", model.Spec.Source.Git.Ref))
		}
	} else if model.Spec.Source.URL != nil {
		lines = append(lines, fmt.Sprintf("# SOURCE_URL %s", model.Spec.Source.URL.URL))
	} else if model.Spec.Source.S3 != nil {
		s3 := model.Spec.Source.S3
		lines = append(lines, fmt.Sprintf("# S3_PATH s3://%s/%s", s3.Bucket, s3.Key))
	}

	// FROM directive
	lines = append(lines, fmt.Sprintf("FROM %s", fromPath))

	// Add Modelfile config if specified
	if model.Spec.Modelfile != nil {
		mf := model.Spec.Modelfile

		if mf.Template != "" {
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf("TEMPLATE \"\"\"%s\"\"\"", mf.Template))
		}

		if mf.System != "" {
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf("SYSTEM \"\"\"%s\"\"\"", mf.System))
		}

		if mf.Parameters != nil {
			lines = append(lines, "")
			p := mf.Parameters
			if p.Temperature != nil {
				lines = append(lines, fmt.Sprintf("PARAMETER temperature %s", *p.Temperature))
			}
			if p.TopP != nil {
				lines = append(lines, fmt.Sprintf("PARAMETER top_p %s", *p.TopP))
			}
			if p.TopK != nil {
				lines = append(lines, fmt.Sprintf("PARAMETER top_k %d", *p.TopK))
			}
			if p.RepeatPenalty != nil {
				lines = append(lines, fmt.Sprintf("PARAMETER repeat_penalty %s", *p.RepeatPenalty))
			}
			if p.NumCtx != nil {
				lines = append(lines, fmt.Sprintf("PARAMETER num_ctx %d", *p.NumCtx))
			}
			if p.NumGPU != nil {
				lines = append(lines, fmt.Sprintf("PARAMETER num_gpu %d", *p.NumGPU))
			}
			if p.Seed != nil {
				lines = append(lines, fmt.Sprintf("PARAMETER seed %d", *p.Seed))
			}
			for _, stop := range p.Stop {
				lines = append(lines, fmt.Sprintf("PARAMETER stop \"%s\"", stop))
			}
		}
	}

	return strings.Join(lines, "\n")
}

func buildS3Container(model *modelsv1alpha1.Model) corev1.Container {
	s3 := model.Spec.Source.S3

	// Build the aws s3 cp command with optional endpoint and region
	var endpointArg, regionArg string
	if s3.Endpoint != "" {
		endpointArg = fmt.Sprintf("--endpoint-url %s", s3.Endpoint)
	}
	if s3.Region != "" {
		regionArg = fmt.Sprintf("--region %s", s3.Region)
	}

	script := fmt.Sprintf(`aws s3 cp %s %s s3://%s/%s /models/ --recursive && \
echo "Download complete" && \
ls -la /models`, endpointArg, regionArg, s3.Bucket, s3.Key)

	container := corev1.Container{
		Name:    "downloader",
		Image:   s3Image,
		Command: []string{"sh", "-c"},
		Args:    []string{script},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      modelVolumeName,
				MountPath: modelMountPath,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("256Mi"),
				corev1.ResourceCPU:    resource.MustParse("250m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1Gi"),
				corev1.ResourceCPU:    resource.MustParse("1"),
			},
		},
	}

	// Add AWS credentials from secret if specified
	if model.Spec.CredentialsSecret != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name: "AWS_ACCESS_KEY_ID",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: model.Spec.CredentialsSecret,
						},
						Key:      "AWS_ACCESS_KEY_ID",
						Optional: ptr.To(true),
					},
				},
			},
			corev1.EnvVar{
				Name: "AWS_SECRET_ACCESS_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: model.Spec.CredentialsSecret,
						},
						Key:      "AWS_SECRET_ACCESS_KEY",
						Optional: ptr.To(true),
					},
				},
			},
		)
	}

	return container
}

func buildURLContainer(model *modelsv1alpha1.Model) corev1.Container {
	url := model.Spec.Source.URL

	script := fmt.Sprintf(`curl -L -o /models/model "%s" && \
echo "Download complete" && \
ls -la /models`, url.URL)

	return corev1.Container{
		Name:    "downloader",
		Image:   urlImage,
		Command: []string{"sh", "-c"},
		Args:    []string{script},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      modelVolumeName,
				MountPath: modelMountPath,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("128Mi"),
				corev1.ResourceCPU:    resource.MustParse("100m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("512Mi"),
				corev1.ResourceCPU:    resource.MustParse("500m"),
			},
		},
	}
}

func buildGitContainer(model *modelsv1alpha1.Model) corev1.Container {
	git := model.Spec.Source.Git
	ref := git.Ref
	if ref == "" {
		ref = "main"
	}

	// Default to LFS enabled
	lfsEnabled := true
	if git.LFS != nil {
		lfsEnabled = *git.LFS
	}

	// Default to shallow clone
	depth := 1
	if git.Depth != nil {
		depth = *git.Depth
	}

	// Build clone command
	var depthArg string
	if depth > 0 {
		depthArg = fmt.Sprintf("--depth %d", depth)
	}

	var lfsCommands string
	if lfsEnabled {
		lfsCommands = `apk add --no-cache git-lfs && \
git lfs install && \
`
	}

	// Build the Modelfile content
	modelfileContent := buildModelfileContent(model)

	var script string

	// Check if we need sparse checkout (include patterns)
	if len(git.Include) > 0 {
		// Build sparse checkout patterns
		var patterns string
		for _, p := range git.Include {
			patterns += fmt.Sprintf("echo '%s' >> .git/info/sparse-checkout && \\\n", p)
		}

		script = fmt.Sprintf(`%sgit clone --no-checkout %s --branch %s %s /tmp/repo && \
cd /tmp/repo && \
git sparse-checkout init --no-cone && \
%sgit checkout %s && \
`, lfsCommands, depthArg, ref, git.URL, patterns, ref)

		// Add LFS pull if enabled
		if lfsEnabled {
			script += `git lfs pull && \
`
		}

		script += `cd / && \
mv /tmp/repo/* /models/ 2>/dev/null || true && \
mv /tmp/repo/.* /models/ 2>/dev/null || true && \
rm -rf /tmp/repo && \
`
	} else {
		// Standard clone
		script = fmt.Sprintf(`%sgit clone %s --branch %s %s /tmp/repo && \
mv /tmp/repo/* /models/ && \
rm -rf /tmp/repo && \
`, lfsCommands, depthArg, ref, git.URL)
	}

	// Add exclude patterns (delete files after clone)
	if len(git.Exclude) > 0 {
		script += "cd /models && \\\n"
		for _, p := range git.Exclude {
			script += fmt.Sprintf("rm -rf %s 2>/dev/null || true && \\\n", p)
		}
	}

	// Write Modelfile and finish
	script += fmt.Sprintf(`cat > /models/Modelfile << 'MODELFILE_EOF'
%s
MODELFILE_EOF
echo "Clone complete" && \
ls -la /models`, modelfileContent)

	container := corev1.Container{
		Name:    "downloader",
		Image:   gitImage,
		Command: []string{"sh", "-c"},
		Args:    []string{script},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      modelVolumeName,
				MountPath: modelMountPath,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("256Mi"),
				corev1.ResourceCPU:    resource.MustParse("250m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("2Gi"),
				corev1.ResourceCPU:    resource.MustParse("2"),
			},
		},
	}

	// Add Git credentials from secret if specified (username/password or token)
	if model.Spec.CredentialsSecret != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name: "GIT_USERNAME",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: model.Spec.CredentialsSecret,
						},
						Key:      "GIT_USERNAME",
						Optional: ptr.To(true),
					},
				},
			},
			corev1.EnvVar{
				Name: "GIT_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: model.Spec.CredentialsSecret,
						},
						Key:      "GIT_PASSWORD",
						Optional: ptr.To(true),
					},
				},
			},
		)
	}

	return container
}
