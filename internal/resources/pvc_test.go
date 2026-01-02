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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelsv1alpha1 "github.com/rsJames-ttrpg/model-operator/api/v1alpha1"
)

func TestBuildPVC(t *testing.T) {
	tests := []struct {
		name             string
		model            *modelsv1alpha1.Model
		wantName         string
		wantStorageClass string
		wantSize         string
		wantAccessModes  []corev1.PersistentVolumeAccessMode
	}{
		{
			name: "basic PVC",
			model: &modelsv1alpha1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "llama-3-8b",
					Namespace: "default",
				},
				Spec: modelsv1alpha1.ModelSpec{
					Storage: modelsv1alpha1.StorageSpec{
						StorageClass: "longhorn",
						Size:         "20Gi",
					},
				},
			},
			wantName:         "model-llama-3-8b",
			wantStorageClass: "longhorn",
			wantSize:         "20Gi",
			wantAccessModes:  []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
		{
			name: "PVC with custom access modes",
			model: &modelsv1alpha1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shared-model",
					Namespace: "ml",
				},
				Spec: modelsv1alpha1.ModelSpec{
					Storage: modelsv1alpha1.StorageSpec{
						StorageClass: "nfs",
						Size:         "100Gi",
						AccessModes:  []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
					},
				},
			},
			wantName:         "model-shared-model",
			wantStorageClass: "nfs",
			wantSize:         "100Gi",
			wantAccessModes:  []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pvc := BuildPVC(tt.model)

			if pvc.Name != tt.wantName {
				t.Errorf("PVC name = %v, want %v", pvc.Name, tt.wantName)
			}

			if pvc.Namespace != tt.model.Namespace {
				t.Errorf("PVC namespace = %v, want %v", pvc.Namespace, tt.model.Namespace)
			}

			if *pvc.Spec.StorageClassName != tt.wantStorageClass {
				t.Errorf("StorageClassName = %v, want %v", *pvc.Spec.StorageClassName, tt.wantStorageClass)
			}

			gotSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
			if gotSize.String() != tt.wantSize {
				t.Errorf("Size = %v, want %v", gotSize.String(), tt.wantSize)
			}

			if len(pvc.Spec.AccessModes) != len(tt.wantAccessModes) {
				t.Errorf("AccessModes length = %v, want %v", len(pvc.Spec.AccessModes), len(tt.wantAccessModes))
			}

			for i, mode := range pvc.Spec.AccessModes {
				if mode != tt.wantAccessModes[i] {
					t.Errorf("AccessModes[%d] = %v, want %v", i, mode, tt.wantAccessModes[i])
				}
			}

			// Check labels
			if pvc.Labels["app.kubernetes.io/managed-by"] != "model-operator" {
				t.Errorf("Missing managed-by label")
			}
		})
	}
}
