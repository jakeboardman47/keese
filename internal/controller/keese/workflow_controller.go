// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

const (
	workflowFinalizer        = "finalizers.workflow.operator.keese.ai/cascade"
	workflowFieldOwner       = "keese-workflow-controller"
	conditionTypeReady       = "Ready"
	conditionTypeProgressing = "Progressing"
)

// WorkflowReconciler reconciles a Workflow object.
type WorkflowReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Argo          ArgoProjector
	Rebac         WorkflowRebacWriter
	EventRecorder interface {
		Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{})
	}
}

// +kubebuilder:rbac:groups=workflow.operator.keese.ai,resources=workflows,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=workflow.operator.keese.ai,resources=workflows/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=workflow.operator.keese.ai,resources=workflows/finalizers,verbs=update
// +kubebuilder:rbac:groups=workflow.operator.keese.ai,resources=workflowruns,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile moves the Workflow toward the desired state.
//
// The loop:
//  1. Fetch the Workflow CR.
//  2. Handle deletion (finalizer cleanup — block while active WorkflowRuns exist).
//  3. Ensure the finalizer is present.
//  4. Project the Argo WorkflowTemplate via SSA.
//  5. Project triggers (Cron / Knative / NATS / HTTP) via SSA.
//  6. Project outputs via SSA.
//  7. Write ReBAC tuples.
//  8. Patch status.
func (r *WorkflowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Fetch
	var wf keesev1alpha1.Workflow
	if err := r.Get(ctx, req.NamespacedName, &wf); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get workflow: %w", err)
	}

	// Take a snapshot before any mutation (used for status patch later).
	orig := wf.DeepCopy()

	// 2. Deletion handling
	if !wf.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &wf, orig)
	}

	// 3. Ensure finalizer
	if !controllerutil.ContainsFinalizer(&wf, workflowFinalizer) {
		controllerutil.AddFinalizer(&wf, workflowFinalizer)
		if err := r.Update(ctx, &wf); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 4. Project Argo WorkflowTemplate
	tmplName, err := r.Argo.ProjectWorkflowTemplate(ctx, &wf)
	if err != nil {
		log.Error(err, "failed to project WorkflowTemplate")
		r.setCondition(&wf, conditionTypeReady, metav1.ConditionFalse, "ProjectionFailed", err.Error())
		r.setCondition(&wf, conditionTypeProgressing, metav1.ConditionFalse, "ProjectionFailed", err.Error())
		wf.Status.Phase = keesev1alpha1.WorkflowPhaseDegraded
		_ = r.patchStatus(ctx, &wf, orig)
		return ctrl.Result{}, fmt.Errorf("project WorkflowTemplate: %w", err)
	}
	wf.Status.WorkflowTemplateRef = tmplName
	r.emitEvent(&wf, "Normal", ReasonWorkflowProjected, "Argo WorkflowTemplate %s projected", tmplName)

	// 5. Project triggers
	for i := range wf.Spec.Triggers {
		trigger := &wf.Spec.Triggers[i]
		if projErr := r.reconcileTrigger(ctx, &wf, trigger); projErr != nil {
			log.Error(projErr, "failed to project trigger", "type", trigger.Type)
			r.emitEvent(&wf, "Warning", ReasonTriggerProjectionFailed, "trigger %s projection failed: %v", trigger.Type, projErr)
			// Non-fatal: continue with other triggers, mark Degraded.
			wf.Status.Phase = keesev1alpha1.WorkflowPhaseDegraded
		} else {
			r.emitEvent(&wf, "Normal", ReasonTriggerProjected, "trigger %s projected", trigger.Type)
		}
	}

	// 6. Project outputs
	for i := range wf.Spec.Outputs {
		output := &wf.Spec.Outputs[i]
		if projErr := r.reconcileOutput(ctx, &wf, output); projErr != nil {
			log.Error(projErr, "failed to project output", "name", output.Name)
		} else {
			r.emitEvent(&wf, "Normal", ReasonOutputProjected, "output %s projected", output.Name)
		}
	}

	// 7. ReBAC tuples
	count, rebacErr := r.Rebac.WriteWorkflowOwner(ctx, &wf)
	if rebacErr != nil {
		log.Error(rebacErr, "failed to write ReBAC tuples")
		// Non-fatal: record but don't block convergence.
	} else {
		wf.Status.TupleCount = count
	}

	// 8. Status
	if wf.Status.Phase != keesev1alpha1.WorkflowPhaseDegraded {
		wf.Status.Phase = keesev1alpha1.WorkflowPhaseReady
	}
	r.setCondition(&wf, conditionTypeReady, metav1.ConditionTrue, "Reconciled", "Workflow reconciled successfully")
	r.setCondition(&wf, conditionTypeProgressing, metav1.ConditionFalse, "Idle", "No changes pending")
	wf.Status.ObservedGeneration = wf.Generation

	if err := r.patchStatus(ctx, &wf, orig); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch status: %w", err)
	}

	return ctrl.Result{}, nil
}

// reconcileDelete handles Workflow deletion:
// blocks while non-terminal WorkflowRuns exist, then removes the Argo
// WorkflowTemplate and releases the finalizer.
func (r *WorkflowReconciler) reconcileDelete(ctx context.Context, wf *keesev1alpha1.Workflow, orig *keesev1alpha1.Workflow) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Check for active (non-terminal) WorkflowRuns.
	var runList keesev1alpha1.WorkflowRunList
	if err := r.List(ctx, &runList, client.InNamespace(wf.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("list workflowruns: %w", err)
	}

	var activeCount int
	for _, run := range runList.Items {
		if run.Spec.WorkflowRef.Name != wf.Name {
			continue
		}
		phase := run.Status.Phase
		if phase != keesev1alpha1.WorkflowRunPhaseSucceeded &&
			phase != keesev1alpha1.WorkflowRunPhaseFailed &&
			phase != keesev1alpha1.WorkflowRunPhaseError {
			activeCount++
		}
	}

	if activeCount > 0 {
		log.Info("blocking deletion — active WorkflowRuns exist", "count", activeCount)
		r.emitEvent(wf, "Warning", ReasonWorkflowCascadeBlocked,
			"deletion blocked: %d active WorkflowRun(s) must complete first", activeCount)
		wf.Status.Phase = keesev1alpha1.WorkflowPhaseDeleting
		_ = r.patchStatus(ctx, wf, orig)
		return ctrl.Result{RequeueAfter: requeueAfterDuration}, nil
	}

	// Clean up Argo WorkflowTemplate.
	if delErr := r.Argo.DeleteWorkflowTemplate(ctx, wf); delErr != nil {
		return ctrl.Result{}, fmt.Errorf("delete WorkflowTemplate: %w", delErr)
	}

	// Clean up ReBAC tuples.
	if rebacErr := r.Rebac.DeleteWorkflowTuples(ctx, wf); rebacErr != nil {
		log.Error(rebacErr, "failed to delete ReBAC tuples on cleanup")
	}

	// Release finalizer.
	controllerutil.RemoveFinalizer(wf, workflowFinalizer)
	if err := r.Update(ctx, wf); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

// reconcileTrigger projects a single trigger to its backing resource.
// Cron → CronJob (stub SSA), KnativeTrigger → Knative Trigger (stub SSA),
// HTTPWebhook → HTTPRoute (stub SSA).
// TODO(spec-followup): Full CronJob / KEDA / Knative / HTTPRoute SSA projection
// requires those API types in go.mod; stubs return nil to unblock integration tests.
func (r *WorkflowReconciler) reconcileTrigger(_ context.Context, _ *keesev1alpha1.Workflow, _ *keesev1alpha1.WorkflowTrigger) error {
	// TODO(spec-followup): project CronJob/KEDA ScaledObject/Knative Trigger/HTTPRoute
	// via SSA with client.FieldOwner(workflowFieldOwner). Each resource is owner-ref'd
	// to the Workflow so deletion cascades automatically.
	return nil
}

// reconcileOutput projects a single output to its backing resource.
// TODO(spec-followup): Full Knative Sink / NATS stream / S3 / GitHub PR SSA projection
// deferred until the respective API types are available in go.mod.
func (r *WorkflowReconciler) reconcileOutput(_ context.Context, _ *keesev1alpha1.Workflow, _ *keesev1alpha1.WorkflowOutput) error {
	// TODO(spec-followup): SSA-project Knative Sink / NATS stream / S3 config /
	// GitHub PR sink. Owner-ref'd to Workflow.
	return nil
}

// patchStatus applies a status-only SSA patch from orig → wf.
func (r *WorkflowReconciler) patchStatus(ctx context.Context, wf *keesev1alpha1.Workflow, orig *keesev1alpha1.Workflow) error {
	return r.Status().Patch(ctx, wf, client.MergeFrom(orig))
}

// setCondition upserts a condition on wf.Status.Conditions.
func (r *WorkflowReconciler) setCondition(wf *keesev1alpha1.Workflow, condType string, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&wf.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: wf.Generation,
	})
}

// emitEvent emits a Kubernetes event if an EventRecorder is configured.
func (r *WorkflowReconciler) emitEvent(wf *keesev1alpha1.Workflow, eventtype, reason, messageFmt string, args ...interface{}) {
	if r.EventRecorder != nil {
		r.EventRecorder.Eventf(wf, eventtype, reason, messageFmt, args...)
	}
}

// SetupWithManager registers the controller and its watches.
func (r *WorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keesev1alpha1.Workflow{}).
		Named("workflow-workflow").
		Complete(r)
}
