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
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	modelsv1alpha1 "github.com/rsJames-ttrpg/model-operator/api/v1alpha1"
	"github.com/rsJames-ttrpg/model-operator/internal/resources"
)

const (
	// Requeue intervals
	requeuePending     = 10 * time.Second
	requeueDownloading = 15 * time.Second
	requeueReady       = 5 * time.Minute
	requeueFailed      = 1 * time.Minute

	// Condition types
	conditionTypeReady = "Ready"
)

// ModelReconciler reconciles a Model object
type ModelReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=models.main-currents.news,resources=models,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=models.main-currents.news,resources=models/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=models.main-currents.news,resources=models/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ModelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the Model
	model := &modelsv1alpha1.Model{}
	if err := r.Get(ctx, req.NamespacedName, model); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Model resource not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Model")
		return ctrl.Result{}, err
	}

	// Determine current phase (default to Pending)
	phase := model.Status.Phase
	if phase == "" {
		phase = modelsv1alpha1.ModelPhasePending
	}

	log.Info("Reconciling Model", "phase", phase)

	switch phase {
	case modelsv1alpha1.ModelPhasePending:
		return r.reconcilePending(ctx, model)
	case modelsv1alpha1.ModelPhaseDownloading:
		return r.reconcileDownloading(ctx, model)
	case modelsv1alpha1.ModelPhaseReady:
		return r.reconcileReady(ctx, model)
	case modelsv1alpha1.ModelPhaseFailed:
		return r.reconcileFailed(ctx, model)
	default:
		log.Info("Unknown phase, resetting to Pending", "phase", phase)
		return r.updateStatus(ctx, model, modelsv1alpha1.ModelPhasePending, "Unknown phase, resetting")
	}
}

// reconcilePending handles the Pending phase: creates PVC and Job, transitions to Downloading
func (r *ModelReconciler) reconcilePending(ctx context.Context, model *modelsv1alpha1.Model) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Create PVC if not exists
	pvc := resources.BuildPVC(model)
	if err := controllerutil.SetControllerReference(model, pvc, r.Scheme); err != nil {
		log.Error(err, "Failed to set owner reference on PVC")
		return ctrl.Result{}, err
	}

	existingPVC := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, existingPVC)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Creating PVC", "name", pvc.Name)
			if err := r.Create(ctx, pvc); err != nil {
				log.Error(err, "Failed to create PVC")
				return r.updateStatus(ctx, model, modelsv1alpha1.ModelPhasePending,
					fmt.Sprintf("Failed to create PVC: %v", err))
			}
		} else {
			log.Error(err, "Failed to get PVC")
			return ctrl.Result{}, err
		}
	}

	// Create download Job if not exists
	job, err := resources.BuildDownloadJob(model)
	if err != nil {
		log.Error(err, "Failed to build download Job")
		return r.updateStatus(ctx, model, modelsv1alpha1.ModelPhaseFailed,
			fmt.Sprintf("Failed to build download Job: %v", err))
	}

	if err := controllerutil.SetControllerReference(model, job, r.Scheme); err != nil {
		log.Error(err, "Failed to set owner reference on Job")
		return ctrl.Result{}, err
	}

	existingJob := &batchv1.Job{}
	err = r.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, existingJob)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Creating download Job", "name", job.Name)
			if err := r.Create(ctx, job); err != nil {
				log.Error(err, "Failed to create Job")
				return r.updateStatus(ctx, model, modelsv1alpha1.ModelPhasePending,
					fmt.Sprintf("Failed to create Job: %v", err))
			}
		} else {
			log.Error(err, "Failed to get Job")
			return ctrl.Result{}, err
		}
	}

	// Transition to Downloading
	return r.updateStatus(ctx, model, modelsv1alpha1.ModelPhaseDownloading, "Download started")
}

// reconcileDownloading handles the Downloading phase: monitors Job status
func (r *ModelReconciler) reconcileDownloading(ctx context.Context, model *modelsv1alpha1.Model) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	jobName := resources.JobName(model.Name)
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: model.Namespace}, job)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Job was deleted, recreate by going back to Pending
			log.Info("Download Job not found, resetting to Pending")
			return r.updateStatus(ctx, model, modelsv1alpha1.ModelPhasePending, "Job not found, recreating")
		}
		log.Error(err, "Failed to get Job")
		return ctrl.Result{}, err
	}

	// Check Job status
	if job.Status.Succeeded > 0 {
		log.Info("Download Job succeeded")
		return r.updateStatusWithProgress(ctx, model, modelsv1alpha1.ModelPhaseReady, "Download complete", 100)
	}

	// Check if Job failed (exceeded backoff limit)
	if job.Status.Failed > 0 {
		// Check conditions for failure
		for _, cond := range job.Status.Conditions {
			if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
				log.Info("Download Job failed", "reason", cond.Reason, "message", cond.Message)
				return r.updateStatus(ctx, model, modelsv1alpha1.ModelPhaseFailed,
					fmt.Sprintf("Download failed: %s", cond.Message))
			}
		}
	}

	// Still running, update status and requeue
	message := "Download in progress"
	if job.Status.Active > 0 {
		message = fmt.Sprintf("Download in progress (active pods: %d)", job.Status.Active)
	}

	// Update status to ensure PVCName is set
	if model.Status.PVCName == "" {
		model.Status.PVCName = resources.PVCName(model.Name)
		model.Status.Message = message
		model.Status.ObservedGeneration = model.Generation
		if err := r.Status().Update(ctx, model); err != nil {
			log.Error(err, "Failed to update Model status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: requeueDownloading}, nil
}

// reconcileReady handles the Ready phase: verifies PVC still exists
func (r *ModelReconciler) reconcileReady(ctx context.Context, model *modelsv1alpha1.Model) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Verify PVC still exists
	pvcName := resources.PVCName(model.Name)
	pvc := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: model.Namespace}, pvc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("PVC was deleted, resetting to Pending")
			return r.updateStatus(ctx, model, modelsv1alpha1.ModelPhasePending, "PVC was deleted, recreating")
		}
		log.Error(err, "Failed to get PVC")
		return ctrl.Result{}, err
	}

	// Still ready, slow poll
	return ctrl.Result{RequeueAfter: requeueReady}, nil
}

// reconcileFailed handles the Failed phase: allows retry when Job is deleted
func (r *ModelReconciler) reconcileFailed(ctx context.Context, model *modelsv1alpha1.Model) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Check if Job was deleted (manual retry trigger)
	jobName := resources.JobName(model.Name)
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: model.Namespace}, job)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Download Job was deleted, retrying")
			return r.updateStatus(ctx, model, modelsv1alpha1.ModelPhasePending, "Retrying download")
		}
		log.Error(err, "Failed to get Job")
		return ctrl.Result{}, err
	}

	// Job still exists, stay in Failed state
	return ctrl.Result{RequeueAfter: requeueFailed}, nil
}

// updateStatus updates the Model status with a new phase and message
func (r *ModelReconciler) updateStatus(ctx context.Context, model *modelsv1alpha1.Model, phase modelsv1alpha1.ModelPhase, message string) (ctrl.Result, error) {
	return r.updateStatusWithProgress(ctx, model, phase, message, model.Status.Progress)
}

// updateStatusWithProgress updates the Model status with a new phase, message, and progress
func (r *ModelReconciler) updateStatusWithProgress(ctx context.Context, model *modelsv1alpha1.Model, phase modelsv1alpha1.ModelPhase, message string, progress int) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	model.Status.Phase = phase
	model.Status.Message = message
	model.Status.Progress = progress
	model.Status.PVCName = resources.PVCName(model.Name)
	model.Status.ObservedGeneration = model.Generation

	// Update condition
	condition := metav1.Condition{
		Type:               conditionTypeReady,
		ObservedGeneration: model.Generation,
		LastTransitionTime: metav1.Now(),
	}

	switch phase {
	case modelsv1alpha1.ModelPhaseReady:
		condition.Status = metav1.ConditionTrue
		condition.Reason = "DownloadComplete"
		condition.Message = message
	case modelsv1alpha1.ModelPhaseFailed:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "DownloadFailed"
		condition.Message = message
	default:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "InProgress"
		condition.Message = message
	}

	meta.SetStatusCondition(&model.Status.Conditions, condition)

	if err := r.Status().Update(ctx, model); err != nil {
		log.Error(err, "Failed to update Model status")
		return ctrl.Result{}, err
	}

	// Determine requeue interval based on phase
	var requeueAfter time.Duration
	switch phase {
	case modelsv1alpha1.ModelPhasePending:
		requeueAfter = requeuePending
	case modelsv1alpha1.ModelPhaseDownloading:
		requeueAfter = requeueDownloading
	case modelsv1alpha1.ModelPhaseReady:
		requeueAfter = requeueReady
	case modelsv1alpha1.ModelPhaseFailed:
		requeueAfter = requeueFailed
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&modelsv1alpha1.Model{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&batchv1.Job{}).
		Named("model").
		Complete(r)
}
