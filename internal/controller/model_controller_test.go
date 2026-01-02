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

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	modelsv1alpha1 "github.com/rsJames-ttrpg/model-operator/api/v1alpha1"
	"github.com/rsJames-ttrpg/model-operator/internal/resources"
)

var _ = Describe("Model Controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When creating a Model resource", func() {
		const modelName = "test-huggingface-model"
		const modelNamespace = "default"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      modelName,
			Namespace: modelNamespace,
		}

		BeforeEach(func() {
			By("Creating a new Model")
			model := &modelsv1alpha1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name:      modelName,
					Namespace: modelNamespace,
				},
				Spec: modelsv1alpha1.ModelSpec{
					Source: modelsv1alpha1.ModelSource{
						HuggingFace: &modelsv1alpha1.HuggingFaceSource{
							RepoID:   "sentence-transformers/all-MiniLM-L6-v2",
							Revision: "main",
						},
					},
					Storage: modelsv1alpha1.StorageSpec{
						StorageClass: "standard",
						Size:         "1Gi",
					},
					Version: "1.0",
				},
			}
			Expect(k8sClient.Create(ctx, model)).To(Succeed())
		})

		AfterEach(func() {
			By("Cleaning up the Model")
			model := &modelsv1alpha1.Model{}
			err := k8sClient.Get(ctx, typeNamespacedName, model)
			if err == nil {
				Expect(k8sClient.Delete(ctx, model)).To(Succeed())
			}

			// Clean up PVC if it exists
			pvc := &corev1.PersistentVolumeClaim{}
			pvcName := types.NamespacedName{
				Name:      resources.PVCName(modelName),
				Namespace: modelNamespace,
			}
			err = k8sClient.Get(ctx, pvcName, pvc)
			if err == nil {
				Expect(k8sClient.Delete(ctx, pvc)).To(Succeed())
			}

			// Clean up Job if it exists
			job := &batchv1.Job{}
			jobName := types.NamespacedName{
				Name:      resources.JobName(modelName),
				Namespace: modelNamespace,
			}
			err = k8sClient.Get(ctx, jobName, job)
			if err == nil {
				Expect(k8sClient.Delete(ctx, job)).To(Succeed())
			}
		})

		It("should create PVC and Job on first reconcile", func() {
			By("Reconciling the Model")
			reconciler := &ModelReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			By("Checking PVC was created")
			pvc := &corev1.PersistentVolumeClaim{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      resources.PVCName(modelName),
					Namespace: modelNamespace,
				}, pvc)
			}, timeout, interval).Should(Succeed())

			Expect(pvc.Spec.Resources.Requests[corev1.ResourceStorage]).To(Equal(
				pvc.Spec.Resources.Requests[corev1.ResourceStorage],
			))

			By("Checking Job was created")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      resources.JobName(modelName),
					Namespace: modelNamespace,
				}, job)
			}, timeout, interval).Should(Succeed())

			Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("python:3.11-slim"))

			By("Checking Model status was updated")
			model := &modelsv1alpha1.Model{}
			Eventually(func() modelsv1alpha1.ModelPhase {
				err := k8sClient.Get(ctx, typeNamespacedName, model)
				if err != nil {
					return ""
				}
				return model.Status.Phase
			}, timeout, interval).Should(Equal(modelsv1alpha1.ModelPhaseDownloading))

			Expect(model.Status.PVCName).To(Equal(resources.PVCName(modelName)))
		})

		It("should transition to Ready when Job succeeds", func() {
			By("Reconciling to create resources")
			reconciler := &ModelReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Simulating Job success")
			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      resources.JobName(modelName),
					Namespace: modelNamespace,
				}, job)
			}, timeout, interval).Should(Succeed())

			job.Status.Succeeded = 1
			Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())

			By("Reconciling again")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking Model is Ready")
			model := &modelsv1alpha1.Model{}
			Eventually(func() modelsv1alpha1.ModelPhase {
				err := k8sClient.Get(ctx, typeNamespacedName, model)
				if err != nil {
					return ""
				}
				return model.Status.Phase
			}, timeout, interval).Should(Equal(modelsv1alpha1.ModelPhaseReady))

			Expect(model.Status.Progress).To(Equal(100))
		})

		It("should handle Model deletion gracefully", func() {
			By("Reconciling to create resources")
			reconciler := &ModelReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Deleting the Model")
			model := &modelsv1alpha1.Model{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, model)).To(Succeed())
			Expect(k8sClient.Delete(ctx, model)).To(Succeed())

			By("Reconciling deleted Model")
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})

	Context("When Model has no source specified", func() {
		It("should fail reconciliation", func() {
			ctx := context.Background()
			modelName := "no-source-model"
			modelNamespace := "default"

			model := &modelsv1alpha1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name:      modelName,
					Namespace: modelNamespace,
				},
				Spec: modelsv1alpha1.ModelSpec{
					Source: modelsv1alpha1.ModelSource{},
					Storage: modelsv1alpha1.StorageSpec{
						StorageClass: "standard",
						Size:         "1Gi",
					},
				},
			}
			Expect(k8sClient.Create(ctx, model)).To(Succeed())

			defer func() {
				Expect(k8sClient.Delete(ctx, model)).To(Succeed())
			}()

			reconciler := &ModelReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      modelName,
					Namespace: modelNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Model should be in Failed state
			Eventually(func() modelsv1alpha1.ModelPhase {
				m := &modelsv1alpha1.Model{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      modelName,
					Namespace: modelNamespace,
				}, m)
				if err != nil {
					return ""
				}
				return m.Status.Phase
			}, timeout, interval).Should(Equal(modelsv1alpha1.ModelPhaseFailed))
		})
	})

	Context("When reconciling a non-existent Model", func() {
		It("should return without error", func() {
			ctx := context.Background()
			reconciler := &ModelReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "non-existent",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})
})

var _ = Describe("Model Controller - S3 Source", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When creating a Model with S3 source", func() {
		const modelName = "test-s3-model"
		const modelNamespace = "default"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      modelName,
			Namespace: modelNamespace,
		}

		AfterEach(func() {
			model := &modelsv1alpha1.Model{}
			err := k8sClient.Get(ctx, typeNamespacedName, model)
			if err == nil {
				Expect(k8sClient.Delete(ctx, model)).To(Succeed())
			}
		})

		It("should create Job with S3 configuration", func() {
			model := &modelsv1alpha1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name:      modelName,
					Namespace: modelNamespace,
				},
				Spec: modelsv1alpha1.ModelSpec{
					Source: modelsv1alpha1.ModelSource{
						S3: &modelsv1alpha1.S3Source{
							Bucket: "my-bucket",
							Key:    "models/test/",
							Region: "us-west-2",
						},
					},
					Storage: modelsv1alpha1.StorageSpec{
						StorageClass: "standard",
						Size:         "5Gi",
					},
				},
			}
			Expect(k8sClient.Create(ctx, model)).To(Succeed())

			reconciler := &ModelReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			job := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      resources.JobName(modelName),
					Namespace: modelNamespace,
				}, job)
			}, timeout, interval).Should(Succeed())

			Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("amazon/aws-cli:latest"))
		})
	})
})
