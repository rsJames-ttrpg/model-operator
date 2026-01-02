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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ModelPhase represents the current phase of a Model
type ModelPhase string

const (
	ModelPhasePending     ModelPhase = "Pending"
	ModelPhaseDownloading ModelPhase = "Downloading"
	ModelPhaseReady       ModelPhase = "Ready"
	ModelPhaseFailed      ModelPhase = "Failed"
)

// HuggingFaceSource defines configuration for downloading from HuggingFace Hub
type HuggingFaceSource struct {
	// RepoID is the HuggingFace repository ID (e.g., "meta-llama/Llama-3.1-8B-Instruct")
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`
	RepoID string `json:"repoId"`

	// Revision is the git revision (branch, tag, or commit hash)
	// +optional
	// +kubebuilder:default="main"
	Revision string `json:"revision,omitempty"`

	// Include patterns for files to download (e.g., ["*.safetensors", "*.json"])
	// +optional
	Include []string `json:"include,omitempty"`

	// Exclude patterns for files to skip (e.g., ["*.bin", "*.h5"])
	// +optional
	Exclude []string `json:"exclude,omitempty"`
}

// URLSource defines configuration for direct HTTP/HTTPS downloads
type URLSource struct {
	// URL is the direct download URL
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^https?://`
	URL string `json:"url"`
}

// S3Source defines configuration for S3-compatible storage
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

// GitSource defines configuration for Git repositories (with LFS support)
type GitSource struct {
	// URL is the Git repository URL
	// +kubebuilder:validation:Required
	URL string `json:"url"`

	// Ref is the git reference (branch, tag, or commit)
	// +optional
	// +kubebuilder:default="main"
	Ref string `json:"ref,omitempty"`

	// LFS enables Git LFS for large file downloads
	// +optional
	// +kubebuilder:default=true
	LFS *bool `json:"lfs,omitempty"`

	// Depth for shallow clone (0 = full clone)
	// +optional
	// +kubebuilder:default=1
	Depth *int `json:"depth,omitempty"`

	// Include patterns for sparse checkout (e.g., ["*.safetensors", "config.json"])
	// Uses git sparse-checkout with cone mode disabled for glob support
	// +optional
	Include []string `json:"include,omitempty"`

	// Exclude patterns to remove after checkout (e.g., ["*.bin", "*.h5"])
	// +optional
	Exclude []string `json:"exclude,omitempty"`
}

// ModelSource defines where to download the model from.
// Exactly one field must be set.
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

	// Git source for Git repositories (with optional LFS support)
	// +optional
	Git *GitSource `json:"git,omitempty"`
}

// ModelfileSpec defines Ollama-style Modelfile configuration
type ModelfileSpec struct {
	// From overrides the FROM directive in the Modelfile
	// If not set, defaults to "/models"
	// +optional
	From string `json:"from,omitempty"`

	// HuggingFacePath sets the HUGGINGFACE_PATH comment in the Modelfile
	// If not set, auto-generated from source.huggingFace.repoId
	// +optional
	HuggingFacePath string `json:"huggingFacePath,omitempty"`

	// Template is the prompt template for the model
	// +optional
	Template string `json:"template,omitempty"`

	// System is the system prompt
	// +optional
	System string `json:"system,omitempty"`

	// Parameters are model inference parameters
	// +optional
	Parameters *ModelParameters `json:"parameters,omitempty"`
}

// ModelParameters defines inference parameters for the model
type ModelParameters struct {
	// Temperature controls randomness (0.0-2.0)
	// +optional
	Temperature *string `json:"temperature,omitempty"`

	// TopP nucleus sampling parameter (0.0-1.0)
	// +optional
	TopP *string `json:"topP,omitempty"`

	// TopK limits token selection to top K options
	// +optional
	TopK *int `json:"topK,omitempty"`

	// RepeatPenalty penalizes repetition (1.0 = no penalty)
	// +optional
	RepeatPenalty *string `json:"repeatPenalty,omitempty"`

	// Stop sequences that halt generation
	// +optional
	Stop []string `json:"stop,omitempty"`

	// NumCtx context window size
	// +optional
	NumCtx *int `json:"numCtx,omitempty"`

	// NumGPU number of GPU layers to offload
	// +optional
	NumGPU *int `json:"numGpu,omitempty"`

	// Seed for reproducibility (-1 for random)
	// +optional
	Seed *int `json:"seed,omitempty"`
}

// StorageSpec defines PVC configuration for model storage
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

// ModelSpec defines the desired state of Model
type ModelSpec struct {
	// Source defines where to download the model from
	// +kubebuilder:validation:Required
	Source ModelSource `json:"source"`

	// Storage defines PVC configuration
	// +kubebuilder:validation:Required
	Storage StorageSpec `json:"storage"`

	// Modelfile defines Ollama-style configuration (template, system prompt, parameters)
	// +optional
	Modelfile *ModelfileSpec `json:"modelfile,omitempty"`

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

// ModelStatus defines the observed state of Model
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
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the last observed generation
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Size",type=string,JSONPath=`.spec.storage.size`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Model is the Schema for the models API
type Model struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec   ModelSpec   `json:"spec"`
	Status ModelStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ModelList contains a list of Model
type ModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Model `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Model{}, &ModelList{})
}
