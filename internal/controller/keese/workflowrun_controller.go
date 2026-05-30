// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

const (
	workflowRunFinalizer  = "finalizers.workflowrun.keese.ai/cleanup"
	workflowRunFieldOwner = "keese-workflowrun-controller"
	requeueAfterDuration  = 15 * time.Second
	natsStreamReplicas    = 3
	natsStreamRetention   = "workqueue"
)

// WorkflowRunReconciler reconciles a WorkflowRun object.
type WorkflowRunReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Argo          ArgoProjector
	Nats          NatsStreamProvisioner
	NatsDeleter   NatsStreamDeleter
	Rebac         WorkflowRebacWriter
	CTA           CrossTenantAgreementResolver
	EventRecorder interface {
		Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{})
	}
}

// +kubebuilder:rbac:groups=keese.ai,resources=workflowruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keese.ai,resources=workflowruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=workflowruns/finalizers,verbs=update
// +kubebuilder:rbac:groups=keese.ai,resources=workflows,verbs=get;list;watch

// Reconcile moves the WorkflowRun toward the desired state.
//
// The loop:
//  1. Fetch the WorkflowRun.
//  2. Handle deletion (NATS stream cleanup, Secret cleanup, finalizer release).
//  3. Ensure finalizer.
//  4. Fetch the owning Workflow.
//  5. ConcurrencyPolicy check.
//  6. CrossTenantAgreement admission check.
//  7. Provision NATS JetStream stream.
//  8. Project Argo Workflow via SSA (with SA audience injection + retry budget).
//  9. Back-project Argo Workflow status → WorkflowRun.status.
//
// 10. Write ReBAC tuples.
// 11. Patch status.
func (r *WorkflowRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Fetch
	var wfr keesev1alpha1.WorkflowRun
	if err := r.Get(ctx, req.NamespacedName, &wfr); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get workflowrun: %w", err)
	}

	orig := wfr.DeepCopy()

	// 2. Deletion
	if !wfr.DeletionTimestamp.IsZero() {
		return r.reconcileRunDelete(ctx, &wfr)
	}

	// 3. Finalizer
	if !controllerutil.ContainsFinalizer(&wfr, workflowRunFinalizer) {
		controllerutil.AddFinalizer(&wfr, workflowRunFinalizer)
		if err := r.Update(ctx, &wfr); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 4. Fetch owning Workflow
	var wf keesev1alpha1.Workflow
	if err := r.Get(ctx, types.NamespacedName{
		Name:      wfr.Spec.WorkflowRef.Name,
		Namespace: wfr.Namespace,
	}, &wf); err != nil {
		if errors.IsNotFound(err) {
			wfr.Status.Phase = keesev1alpha1.WorkflowRunPhaseError
			r.setRunCondition(&wfr, conditionTypeReady, metav1.ConditionFalse, "WorkflowNotFound",
				fmt.Sprintf("Workflow %q not found", wfr.Spec.WorkflowRef.Name))
			_ = r.patchRunStatus(ctx, &wfr, orig)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get workflow: %w", err)
	}

	// 5. ConcurrencyPolicy
	if requeue, err := r.checkConcurrency(ctx, &wf, &wfr, orig); err != nil || requeue {
		return ctrl.Result{Requeue: requeue}, err
	}

	// 6. CrossTenantAgreement check
	if blocked, err := r.checkCTA(ctx, &wf, &wfr, orig); err != nil || blocked {
		return ctrl.Result{Requeue: blocked}, err
	}

	// 7. NATS JetStream stream provisioning
	streamName, err := r.ensureNATSStream(ctx, &wfr)
	if err != nil {
		log.Error(err, "NATS stream provisioning failed")
		r.emitRunEvent(&wfr, "Warning", ReasonNATSStreamCreateFailed, "NATS stream create failed: %v", err)
		wfr.Status.Phase = keesev1alpha1.WorkflowRunPhaseError
		r.setRunCondition(&wfr, conditionTypeReady, metav1.ConditionFalse, "NATSProvisionFailed", err.Error())
		_ = r.patchRunStatus(ctx, &wfr, orig)
		return ctrl.Result{}, fmt.Errorf("provision NATS stream: %w", err)
	}
	if streamName != "" {
		r.emitRunEvent(&wfr, "Normal", ReasonWorkflowNATSStreamProvisioned, "NATS stream %s provisioned", streamName)
	}

	// 8. Project Argo Workflow
	argoName, err := r.projectArgoWorkflow(ctx, &wf, &wfr)
	if err != nil {
		log.Error(err, "Argo Workflow projection failed")
		r.emitRunEvent(&wfr, "Warning", ReasonWorkflowRunFailed, "Argo projection failed: %v", err)
		wfr.Status.Phase = keesev1alpha1.WorkflowRunPhaseError
		r.setRunCondition(&wfr, conditionTypeReady, metav1.ConditionFalse, "ArgoProjectionFailed", err.Error())
		_ = r.patchRunStatus(ctx, &wfr, orig)
		return ctrl.Result{}, fmt.Errorf("project Argo Workflow: %w", err)
	}
	if wfr.Status.ArgoWorkflowName == "" {
		wfr.Status.ArgoWorkflowName = argoName
		wfr.Status.Phase = keesev1alpha1.WorkflowRunPhaseProvisioning
		r.emitRunEvent(&wfr, "Normal", ReasonWorkflowRunProjected, "Argo Workflow %s projected", argoName)
		r.emitRunEvent(&wfr, "Normal", ReasonWorkflowAudienceInjected,
			"SA audience keese-wf-%s injected", string(wfr.UID))
	}

	// 9. Back-project Argo status
	if syncErr := r.syncArgoStatus(ctx, &wfr); syncErr != nil {
		log.Error(syncErr, "failed to sync Argo status")
		r.emitRunEvent(&wfr, "Warning", ReasonArgoWatchDisconnected, "Argo status sync failed: %v", syncErr)
		// Non-fatal: requeue to retry.
		return ctrl.Result{RequeueAfter: requeueAfterDuration}, nil
	}

	// 10. ReBAC tuples
	count, rebacErr := r.Rebac.WriteWorkflowRunOwner(ctx, &wfr)
	if rebacErr != nil {
		log.Error(rebacErr, "failed to write ReBAC tuples")
	} else {
		wfr.Status.TupleCount = count
	}

	// 11. Status
	wfr.Status.ObservedGeneration = wfr.Generation
	r.setRunCondition(&wfr, conditionTypeProgressing, metav1.ConditionFalse, "Idle", "No changes pending")

	if err := r.patchRunStatus(ctx, &wfr, orig); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch status: %w", err)
	}

	// Requeue while running to pick up Argo phase changes.
	if wfr.Status.Phase == keesev1alpha1.WorkflowRunPhaseRunning ||
		wfr.Status.Phase == keesev1alpha1.WorkflowRunPhaseProvisioning {
		return ctrl.Result{RequeueAfter: requeueAfterDuration}, nil
	}

	return ctrl.Result{}, nil
}

// checkConcurrency enforces the Workflow's ConcurrencyPolicy.
// Returns (requeue=true, nil) when a run should be blocked;
// returns (false, nil) when it's safe to proceed.
func (r *WorkflowRunReconciler) checkConcurrency(
	ctx context.Context,
	wf *keesev1alpha1.Workflow,
	wfr *keesev1alpha1.WorkflowRun,
	orig *keesev1alpha1.WorkflowRun,
) (requeue bool, err error) {
	if wf.Spec.ConcurrencyPolicy == keesev1alpha1.ConcurrencyPolicyAllow || wf.Spec.ConcurrencyPolicy == "" {
		return false, nil
	}

	// Check for an active run that is not this run.
	var runList keesev1alpha1.WorkflowRunList
	if listErr := r.List(ctx, &runList, client.InNamespace(wfr.Namespace)); listErr != nil {
		return false, fmt.Errorf("list workflowruns for concurrency check: %w", listErr)
	}

	for _, run := range runList.Items {
		if run.Name == wfr.Name {
			continue
		}
		if run.Spec.WorkflowRef.Name != wfr.Spec.WorkflowRef.Name {
			continue
		}
		phase := run.Status.Phase
		if phase == keesev1alpha1.WorkflowRunPhaseSucceeded ||
			phase == keesev1alpha1.WorkflowRunPhaseFailed ||
			phase == keesev1alpha1.WorkflowRunPhaseError {
			continue
		}

		switch wf.Spec.ConcurrencyPolicy {
		case keesev1alpha1.ConcurrencyPolicyForbid:
			r.emitRunEvent(wfr, "Warning", ReasonConcurrentRunForbidden,
				"ConcurrencyPolicy=Forbid: run %s is still active", run.Name)
			wfr.Status.Phase = keesev1alpha1.WorkflowRunPhasePending
			r.setRunCondition(wfr, conditionTypeReady, metav1.ConditionFalse, "ConcurrentRunForbidden",
				fmt.Sprintf("active run %s must complete first", run.Name))
			_ = r.patchRunStatus(ctx, wfr, orig)
			return true, nil

		case keesev1alpha1.ConcurrencyPolicyReplace:
			r.emitRunEvent(wfr, "Warning", ReasonConcurrentRunForced,
				"ConcurrencyPolicy=Replace: terminating run %s", run.Name)
			// Mark the old run as Failed (best-effort).
			runCopy := run.DeepCopy()
			runCopy.Status.Phase = keesev1alpha1.WorkflowRunPhaseFailed
			_ = r.Status().Update(ctx, runCopy)
		}
	}

	return false, nil
}

// checkCTA performs a pre-reconcile CrossTenantAgreement admission check.
// Returns (blocked=true, nil) when reconcile should pause pending an agreement.
func (r *WorkflowRunReconciler) checkCTA(
	ctx context.Context,
	wf *keesev1alpha1.Workflow,
	wfr *keesev1alpha1.WorkflowRun,
	orig *keesev1alpha1.WorkflowRun,
) (blocked bool, err error) {
	peers, peersErr := r.CTA.ResolvePeers(ctx, wf)
	if peersErr != nil {
		return false, fmt.Errorf("resolve CTA peers: %w", peersErr)
	}
	if len(peers) == 0 {
		return false, nil
	}

	missing, checkErr := r.CTA.CheckApproved(ctx, wfr, peers)
	if checkErr != nil {
		return false, fmt.Errorf("check CTA approved: %w", checkErr)
	}
	if missing != nil {
		r.emitRunEvent(wfr, "Warning", ReasonCrossTenantAgreementMissing,
			"CrossTenantAgreement missing for transportRef %s (peer workspace %s)",
			missing.TransportRefName, missing.PeerWorkspaceRef)
		wfr.Status.Phase = keesev1alpha1.WorkflowRunPhasePending
		r.setRunCondition(wfr, conditionTypeReady, metav1.ConditionFalse, "CrossTenantAgreementMissing",
			fmt.Sprintf("approved CrossTenantAgreement required for %s", missing.TransportRefName))
		_ = r.patchRunStatus(ctx, wfr, orig)
		return true, nil
	}

	return false, nil
}

// ensureNATSStream provisions a JetStream stream for the WorkflowRun.
// Returns the stream name (or empty string if already provisioned).
func (r *WorkflowRunReconciler) ensureNATSStream(ctx context.Context, wfr *keesev1alpha1.WorkflowRun) (string, error) {
	// Idempotent: if we already have the Argo workflow name assigned the stream
	// was provisioned on the first reconcile.
	if wfr.Status.ArgoWorkflowName != "" {
		return "", nil
	}

	maxAge := 24 * time.Hour // default
	if wfr.Spec.Timeout != nil {
		maxAge = wfr.Spec.Timeout.Duration
	}

	tenantUID, err := r.resolveTenantUID(ctx, wfr)
	if err != nil {
		// Falls back to the run UID so the stream name is still unique;
		// log + continue so a missing tenant doesn't block the run.
		tenantUID = string(wfr.UID)
	}
	runUID := string(wfr.UID)
	streamName := fmt.Sprintf("keese-tenant-%s-wf-%s", tenantUID, runUID)
	subject := fmt.Sprintf("keese.tenant.%s.wf.%s.>", tenantUID, runUID)

	spec := NatsStreamSpec{
		Name:             streamName,
		Subjects:         []string{subject},
		MaxAge:           maxAge,
		Replicas:         natsStreamReplicas,
		WorkloadOwnerUID: string(wfr.UID),
	}

	return r.Nats.Provision(ctx, spec)
}

// resolveTenantUID looks up the WorkflowRun's parent Workspace, then its
// Tenant, and returns the Tenant CR's UID. This is the canonical key for
// per-tenant NATS subject namespacing (design 03c). Falls back to the
// Workspace UID when no Tenant CR is reachable so the stream still gets
// a stable, scoped name.
func (r *WorkflowRunReconciler) resolveTenantUID(ctx context.Context, wfr *keesev1alpha1.WorkflowRun) (string, error) {
	if wfr.Spec.WorkspaceRef.Name == "" {
		return "", fmt.Errorf("workflowrun %s/%s has no workspaceRef", wfr.Namespace, wfr.Name)
	}
	var ws keesev1alpha1.Workspace
	if err := r.Get(ctx, types.NamespacedName{Namespace: wfr.Namespace, Name: wfr.Spec.WorkspaceRef.Name}, &ws); err != nil {
		return "", fmt.Errorf("get workspace: %w", err)
	}
	if ws.Spec.TenantRef.Name == "" {
		return string(ws.UID), nil
	}
	var tenant keesev1alpha1.Tenant
	if err := r.Get(ctx, types.NamespacedName{Name: ws.Spec.TenantRef.Name}, &tenant); err != nil {
		// Tenant is cluster-scoped; if it's not reachable fall back to
		// the workspace UID — still uniquely scopes the NATS subject.
		return string(ws.UID), nil
	}
	return string(tenant.UID), nil
}

// projectArgoWorkflow projects (or idempotently updates) the Argo Workflow for a WorkflowRun.
func (r *WorkflowRunReconciler) projectArgoWorkflow(ctx context.Context, wf *keesev1alpha1.Workflow, wfr *keesev1alpha1.WorkflowRun) (string, error) {
	argoName := fmt.Sprintf("keese-wfr-%s", wfr.Name)
	audience := fmt.Sprintf("keese-wf-%s", string(wfr.UID))

	// Compose retry limit: min(defaultRetryBudget.Limit, wfr.spec.retryBudget)
	retryLimit := wfr.Spec.RetryBudget
	if wf.Spec.DefaultRetryBudget != nil && wf.Spec.DefaultRetryBudget.Limit < retryLimit {
		retryLimit = wf.Spec.DefaultRetryBudget.Limit
	}

	labels := map[string]string{
		"keese.ai/managed":             "true",
		"keese.ai/workflow-run":        wfr.Name,
		"keese.ai/workflow":            wf.Name,
		"keese.ai/supervision-context": "false",
	}
	if wfr.Spec.SupervisionContext != nil && wfr.Spec.SupervisionContext.RequireApproval {
		labels["keese.ai/supervision-context"] = "true"
		labels["keese.ai/supervisor-ref"] = wfr.Spec.SupervisionContext.ReviewerRef
	}

	spec := ArgoWorkflowSpec{
		Name:                    argoName,
		Namespace:               wfr.Namespace,
		WorkflowTemplateRefName: wf.Status.WorkflowTemplateRef,
		Parameters:              wfr.Spec.Parameters,
		Timeout:                 wfr.Spec.Timeout,
		Labels:                  labels,
		ServiceAccountAudience:  audience,
		RetryLimit:              retryLimit,
	}

	return r.Argo.ProjectWorkflow(ctx, spec)
}

// syncArgoStatus back-projects the Argo Workflow phase and nodes to WorkflowRun.status.
func (r *WorkflowRunReconciler) syncArgoStatus(ctx context.Context, wfr *keesev1alpha1.WorkflowRun) error {
	if wfr.Status.ArgoWorkflowName == "" {
		return nil
	}

	argoStatus, err := r.Argo.GetWorkflowStatus(ctx, wfr.Namespace, wfr.Status.ArgoWorkflowName)
	if err != nil {
		return fmt.Errorf("get Argo workflow status: %w", err)
	}
	if argoStatus == nil {
		return nil
	}

	wfr.Status.ArgoPhase = argoStatus.Phase
	wfr.Status.StartedAt = argoStatus.StartedAt
	wfr.Status.FinishedAt = argoStatus.FinishedAt

	// Map Argo phase → keese phase.
	switch argoStatus.Phase {
	case "Pending", "":
		wfr.Status.Phase = keesev1alpha1.WorkflowRunPhaseProvisioning
	case "Running":
		wfr.Status.Phase = keesev1alpha1.WorkflowRunPhaseRunning
		r.setRunCondition(wfr, conditionTypeReady, metav1.ConditionFalse, "Running", "Argo Workflow is running")
	case "Succeeded":
		wfr.Status.Phase = keesev1alpha1.WorkflowRunPhaseSucceeded
		r.setRunCondition(wfr, conditionTypeReady, metav1.ConditionTrue, "Succeeded", "Argo Workflow completed successfully")
	case "Failed":
		wfr.Status.Phase = keesev1alpha1.WorkflowRunPhaseFailed
		r.setRunCondition(wfr, conditionTypeReady, metav1.ConditionFalse, "Failed", "Argo Workflow failed")
	case "Error":
		wfr.Status.Phase = keesev1alpha1.WorkflowRunPhaseError
		r.setRunCondition(wfr, conditionTypeReady, metav1.ConditionFalse, "Error", "Argo Workflow errored")
	}

	// Mirror nodes.
	nodes := make([]keesev1alpha1.NodeStatus, 0, len(argoStatus.Nodes))
	for _, n := range argoStatus.Nodes {
		nodes = append(nodes, keesev1alpha1.NodeStatus{
			ID:          n.ID,
			Phase:       n.Phase,
			DisplayName: n.DisplayName,
			Message:     n.Message,
			StartedAt:   n.StartedAt,
			FinishedAt:  n.FinishedAt,
		})
	}
	wfr.Status.Nodes = nodes

	// Mirror artifacts.
	artifacts := make([]keesev1alpha1.ArtifactOutput, 0, len(argoStatus.Artifacts))
	for _, a := range argoStatus.Artifacts {
		artifacts = append(artifacts, keesev1alpha1.ArtifactOutput{
			Name:   a.Name,
			Path:   a.Path,
			NodeID: a.NodeID,
		})
	}
	wfr.Status.Artifacts = artifacts

	r.emitRunEvent(wfr, "Normal", ReasonArgoStatusSynced, "Argo phase %s mirrored", argoStatus.Phase)
	return nil
}

// reconcileRunDelete handles WorkflowRun deletion: NATS stream cleanup, finalizer release.
func (r *WorkflowRunReconciler) reconcileRunDelete(ctx context.Context, wfr *keesev1alpha1.WorkflowRun) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Delete NATS JetStream stream.
	if wfr.Status.ArgoWorkflowName != "" {
		tenantUID := string(wfr.UID)
		runUID := string(wfr.UID)
		streamName := fmt.Sprintf("keese-tenant-%s-wf-%s", tenantUID, runUID)
		if delErr := r.NatsDeleter.Delete(ctx, streamName); delErr != nil {
			log.Error(delErr, "failed to delete NATS stream", "stream", streamName)
			r.emitRunEvent(wfr, "Warning", ReasonNATSStreamDeleteFailed, "NATS stream %s delete failed: %v", streamName, delErr)
			return ctrl.Result{}, fmt.Errorf("delete NATS stream: %w", delErr)
		}
		r.emitRunEvent(wfr, "Normal", ReasonWorkflowNATSStreamCleaned, "NATS stream %s cleaned", streamName)

		// Delete the Argo Workflow.
		if delErr := r.Argo.DeleteWorkflow(ctx, wfr.Namespace, wfr.Status.ArgoWorkflowName); delErr != nil {
			log.Error(delErr, "failed to delete Argo Workflow")
		}
	}

	// Clean up ReBAC tuples.
	if rebacErr := r.Rebac.DeleteWorkflowRunTuples(ctx, wfr); rebacErr != nil {
		log.Error(rebacErr, "failed to delete ReBAC tuples on cleanup")
	}

	// Release finalizer.
	controllerutil.RemoveFinalizer(wfr, workflowRunFinalizer)
	if err := r.Update(ctx, wfr); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

// patchRunStatus applies a status-only patch.
func (r *WorkflowRunReconciler) patchRunStatus(ctx context.Context, wfr *keesev1alpha1.WorkflowRun, orig *keesev1alpha1.WorkflowRun) error {
	return r.Status().Patch(ctx, wfr, client.MergeFrom(orig))
}

// setRunCondition upserts a condition on wfr.Status.Conditions.
func (r *WorkflowRunReconciler) setRunCondition(wfr *keesev1alpha1.WorkflowRun, condType string, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&wfr.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: wfr.Generation,
	})
}

// emitRunEvent emits a Kubernetes event if an EventRecorder is configured.
func (r *WorkflowRunReconciler) emitRunEvent(wfr *keesev1alpha1.WorkflowRun, eventtype, reason, messageFmt string, args ...interface{}) {
	if r.EventRecorder != nil {
		r.EventRecorder.Eventf(wfr, eventtype, reason, messageFmt, args...)
	}
}

// SetupWithManager registers the controller and its watches.
func (r *WorkflowRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keesev1alpha1.WorkflowRun{}).
		Named("workflow-workflowrun").
		Complete(r)
}
