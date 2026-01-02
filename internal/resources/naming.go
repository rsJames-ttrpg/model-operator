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
)

const (
	// PVCPrefix is the prefix for PVC names
	PVCPrefix = "model-"
	// JobPrefix is the prefix for download Job names
	JobPrefix = "model-download-"
	// VolumePrefix is the prefix for volume names in pods
	VolumePrefix = "model-"
)

// PVCName returns the PVC name for a given model name
func PVCName(modelName string) string {
	return PVCPrefix + modelName
}

// JobName returns the download Job name for a given model name
func JobName(modelName string) string {
	return JobPrefix + modelName
}

// VolumeName returns the volume name for a given model name
func VolumeName(modelName string) string {
	return VolumePrefix + modelName
}

// EnvVarPrefix returns the environment variable prefix for a given model name.
// Converts the model name to uppercase and replaces hyphens with underscores.
// Example: "llama-3-8b" -> "MODEL_LLAMA_3_8B"
func EnvVarPrefix(modelName string) string {
	name := strings.ToUpper(modelName)
	name = strings.ReplaceAll(name, "-", "_")
	return "MODEL_" + name
}

// DefaultMountPath returns the default mount path for a model
func DefaultMountPath(modelName string) string {
	return "/models/" + modelName
}
