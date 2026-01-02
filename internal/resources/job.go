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

	script := fmt.Sprintf(`pip install -q huggingface_hub hf_transfer && \
export HF_HUB_ENABLE_HF_TRANSFER=1 && \
python -c "from huggingface_hub import snapshot_download; snapshot_download('%s', revision='%s', local_dir='/models')" && \
echo "Download complete" && \
ls -la /models`, hf.RepoID, revision)

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
