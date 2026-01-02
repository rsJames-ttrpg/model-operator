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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelsv1alpha1 "github.com/rsJames-ttrpg/model-operator/api/v1alpha1"
)

// BuildPVC creates a PersistentVolumeClaim for the given Model
func BuildPVC(model *modelsv1alpha1.Model) *corev1.PersistentVolumeClaim {
	storageClass := model.Spec.Storage.StorageClass

	accessModes := model.Spec.Storage.AccessModes
	if len(accessModes) == 0 {
		accessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PVCName(model.Name),
			Namespace: model.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "model",
				"app.kubernetes.io/instance":   model.Name,
				"app.kubernetes.io/managed-by": "model-operator",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      accessModes,
			StorageClassName: &storageClass,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(model.Spec.Storage.Size),
				},
			},
		},
	}

	return pvc
}
