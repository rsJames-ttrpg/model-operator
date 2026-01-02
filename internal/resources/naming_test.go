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
	"testing"
)

func TestPVCName(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		want      string
	}{
		{"simple name", "llama", "model-llama"},
		{"with hyphens", "llama-3-8b", "model-llama-3-8b"},
		{"with numbers", "gpt4", "model-gpt4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PVCName(tt.modelName); got != tt.want {
				t.Errorf("PVCName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJobName(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		want      string
	}{
		{"simple name", "llama", "model-download-llama"},
		{"with hyphens", "llama-3-8b", "model-download-llama-3-8b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JobName(tt.modelName); got != tt.want {
				t.Errorf("JobName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVolumeName(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		want      string
	}{
		{"simple name", "llama", "model-llama"},
		{"with hyphens", "llama-3-8b", "model-llama-3-8b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VolumeName(tt.modelName); got != tt.want {
				t.Errorf("VolumeName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnvVarPrefix(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		want      string
	}{
		{"simple name", "llama", "MODEL_LLAMA"},
		{"with hyphens", "llama-3-8b", "MODEL_LLAMA_3_8B"},
		{"lowercase", "gpt-4-turbo", "MODEL_GPT_4_TURBO"},
		{"mixed case", "Mistral-7B", "MODEL_MISTRAL_7B"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EnvVarPrefix(tt.modelName); got != tt.want {
				t.Errorf("EnvVarPrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultMountPath(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		want      string
	}{
		{"simple name", "llama", "/models/llama"},
		{"with hyphens", "llama-3-8b", "/models/llama-3-8b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DefaultMountPath(tt.modelName); got != tt.want {
				t.Errorf("DefaultMountPath() = %v, want %v", got, tt.want)
			}
		})
	}
}
