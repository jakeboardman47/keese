// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	eventingv1 "knative.dev/eventing/pkg/apis/eventing/v1"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

const (
	workflowFinalizer             = "finalizers.workflow.keese.ai/cascade"
	workflowFieldOwner            = "keese-workflow-controller"
	conditionTypeReady            = "Ready"
	conditionTypeProgressing      = "Progressing"
	conditionTypeTriggerProjected = "TriggerProjected"
)

// WorkflowReconciler reconciles a Workflow object.
type WorkflowReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Argo   ArgoProjector
	Rebac  WorkflowRebacWriter
	// LauncherImage is the container image used in CronJob / HTTPRoute launcher pods.
	// Defaults to "ghcr.io/keese-ai/keese:dev" when empty. In production, cmd/main.go
	// should inject the operator's own image via the RELATED_IMAGE_WF_LAUNCHER env var.
	LauncherImage string
	EventRecorder interface {
		Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{})
	}
}

// +kubebuilder:rbac:groups=keese.ai,resources=workflows,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keese.ai,resources=workflows/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=workflows/finalizers,verbs=update
// +kubebuilder:rbac:groups=keese.ai,resources=workflowruns,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=eventing.knative.dev,resources=triggers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete

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
//  8. Derive status.runCount from the live WorkflowRuns.
//  9. Patch status.
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

	// 8. Derive runCount from the live WorkflowRuns owned by this Workflow.
	//    This is pure derived state (rule 04.4): it is recomputed from the
	//    cluster on every reconcile and never read back to drive the next
	//    reconcile decision. Semantics: count of WorkflowRuns currently
	//    existing that reference this Workflow (see countWorkflowRuns).
	runCount, countErr := r.countWorkflowRuns(ctx, &wf)
	if countErr != nil {
		// Non-fatal: a transient list error must not block projection/status
		// convergence. Leave runCount unchanged and requeue.
		log.Error(countErr, "failed to count WorkflowRuns for runCount")
		return ctrl.Result{RequeueAfter: requeueAfterDuration}, nil
	}
	wf.Status.RunCount = runCount

	// 9. Status
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

// reconcileTrigger projects a single trigger to its backing resource via SSA.
//
// Mapping:
//   - Cron           → batch/v1.CronJob  (wf-launcher container creates a WorkflowRun)
//   - KnativeTrigger → eventing.knative.dev/v1.Trigger  (subscriber = wf-launcher Service)
//   - NATSSubscription → no CRD projected (KEDA dep-conflict; documented in go.mod TODO);
//     sets TriggerProjected=False/KEDAUnavailable so the condition is observable.
//   - HTTPWebhook    → gateway.networking.k8s.io/v1.HTTPRoute  (routes to wf-launcher Service)
//
// All resources are labeled keese.ai/managed=true and carry an owner reference so
// garbage collection cascades when the Workflow is deleted.
func (r *WorkflowReconciler) reconcileTrigger(ctx context.Context, wf *keesev1alpha1.Workflow, trigger *keesev1alpha1.WorkflowTrigger) error {
	switch trigger.Type {
	case keesev1alpha1.TriggerTypeCron:
		return r.reconcileCronTrigger(ctx, wf, trigger)
	case keesev1alpha1.TriggerTypeKnativeTrigger:
		return r.reconcileKnativeTrigger(ctx, wf, trigger)
	case keesev1alpha1.TriggerTypeNATSSubscription:
		// KEDA ScaledObject is blocked by a dep-conflict (see go.mod TODO).
		// Set an observable condition and return nil — non-fatal, no CRD projected.
		r.setCondition(wf, conditionTypeTriggerProjected, metav1.ConditionFalse,
			ReasonTriggerKEDAUnavailable,
			"NATSSubscription trigger requires KEDA ScaledObject; KEDA dependency "+
				"conflict pending upstream resolution (see go.mod TODO(dep-conflict))")
		return nil
	case keesev1alpha1.TriggerTypeHTTPWebhook:
		return r.reconcileHTTPWebhookTrigger(ctx, wf, trigger)
	default:
		return fmt.Errorf("unknown trigger type %q", trigger.Type)
	}
}

// reconcileCronTrigger SSA-projects a batch/v1.CronJob that creates a WorkflowRun CR
// on each schedule tick. The launcher container is the operator image itself invoked as
// the keese-wf-launcher sub-binary.
func (r *WorkflowReconciler) reconcileCronTrigger(ctx context.Context, wf *keesev1alpha1.Workflow, trigger *keesev1alpha1.WorkflowTrigger) error {
	cfg := trigger.Cron
	if cfg == nil {
		return fmt.Errorf("trigger type Cron requires spec.triggers[].cron to be set")
	}

	launcherImage := r.LauncherImage
	if launcherImage == "" {
		launcherImage = "ghcr.io/keese-ai/keese:dev"
	}

	suspend := cfg.Suspend
	cj := &batchv1.CronJob{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "CronJob",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cronJobName(wf),
			Namespace: wf.Namespace,
			Labels: map[string]string{
				managedLabel:          managedLabelValue,
				"keese.ai/workflow":   wf.Name,
				"keese.ai/managed-by": workflowFieldOwner,
			},
		},
		Spec: batchv1.CronJobSpec{
			Schedule:          cfg.Schedule,
			TimeZone:          ptrString(cfg.Timezone),
			Suspend:           &suspend,
			ConcurrencyPolicy: batchv1.ForbidConcurrent,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								managedLabel:          managedLabelValue,
								"keese.ai/workflow":   wf.Name,
								"keese.ai/managed-by": workflowFieldOwner,
							},
						},
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyOnFailure,
							Containers: []corev1.Container{
								{
									Name:    "wf-launcher",
									Image:   launcherImage,
									Command: []string{"keese-wf-launcher"},
									Args: []string{
										"--workspace", wf.Spec.WorkspaceRef.Name,
										"--namespace", wf.Namespace,
										"--session-name", "wf-" + wf.Name,
										"--cleanup",
									},
									SecurityContext: &corev1.SecurityContext{
										ReadOnlyRootFilesystem:   ptr[bool](true),
										AllowPrivilegeEscalation: ptr[bool](false),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetOwnerReference(wf, cj, r.Scheme); err != nil {
		return fmt.Errorf("set owner ref on CronJob: %w", err)
	}

	if err := r.Patch(ctx, cj, client.Apply,
		client.FieldOwner(workflowFieldOwner),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("SSA CronJob %s/%s: %w", cj.Namespace, cj.Name, err)
	}

	r.setCondition(wf, conditionTypeTriggerProjected, metav1.ConditionTrue,
		ReasonTriggerCronJobReady,
		fmt.Sprintf("CronJob %s/%s projected (schedule: %s)", cj.Namespace, cj.Name, cfg.Schedule))
	return nil
}

// reconcileKnativeTrigger SSA-projects a Knative eventing/v1.Trigger that subscribes
// to a named Broker and forwards matching CloudEvents to the wf-launcher Service.
func (r *WorkflowReconciler) reconcileKnativeTrigger(ctx context.Context, wf *keesev1alpha1.Workflow, trigger *keesev1alpha1.WorkflowTrigger) error {
	cfg := trigger.KnativeTrigger
	if cfg == nil {
		return fmt.Errorf("trigger type KnativeTrigger requires spec.triggers[].knativeTrigger to be set")
	}

	kt := &eventingv1.Trigger{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "eventing.knative.dev/v1",
			Kind:       "Trigger",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      knativeTriggerName(wf),
			Namespace: wf.Namespace,
			Labels: map[string]string{
				managedLabel:          managedLabelValue,
				"keese.ai/workflow":   wf.Name,
				"keese.ai/managed-by": workflowFieldOwner,
			},
		},
		Spec: eventingv1.TriggerSpec{
			Broker: cfg.BrokerRef,
			Subscriber: duckv1.Destination{
				Ref: &duckv1.KReference{
					APIVersion: "v1",
					Kind:       "Service",
					Name:       wfLauncherServiceName(wf),
					Namespace:  wf.Namespace,
				},
			},
		},
	}

	// Map optional CloudEvent attribute filter.
	if len(cfg.Filter) > 0 {
		attrs := make(eventingv1.TriggerFilterAttributes, len(cfg.Filter))
		for k, v := range cfg.Filter {
			attrs[k] = v
		}
		kt.Spec.Filter = &eventingv1.TriggerFilter{Attributes: attrs}
	}

	if err := controllerutil.SetOwnerReference(wf, kt, r.Scheme); err != nil {
		return fmt.Errorf("set owner ref on Knative Trigger: %w", err)
	}

	if err := r.Patch(ctx, kt, client.Apply,
		client.FieldOwner(workflowFieldOwner),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("SSA Knative Trigger %s/%s: %w", kt.Namespace, kt.Name, err)
	}

	r.setCondition(wf, conditionTypeTriggerProjected, metav1.ConditionTrue,
		ReasonTriggerKnativeTriggerReady,
		fmt.Sprintf("Knative Trigger %s/%s projected (broker: %s)", kt.Namespace, kt.Name, cfg.BrokerRef))
	return nil
}

// reconcileHTTPWebhookTrigger SSA-projects a gateway.networking.k8s.io/v1.HTTPRoute
// that routes incoming POST requests at cfg.Path to the wf-launcher Service.
func (r *WorkflowReconciler) reconcileHTTPWebhookTrigger(ctx context.Context, wf *keesev1alpha1.Workflow, trigger *keesev1alpha1.WorkflowTrigger) error {
	cfg := trigger.HTTPWebhook
	if cfg == nil {
		return fmt.Errorf("trigger type HTTPWebhook requires spec.triggers[].httpWebhook to be set")
	}

	pathExact := gatewayv1.PathMatchExact
	postMethod := gatewayv1.HTTPMethod("POST")
	svcName := gatewayv1.ObjectName(wfLauncherServiceName(wf))
	svcPort := gatewayv1.PortNumber(8080)

	hr := &gatewayv1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1",
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      httpRouteName(wf),
			Namespace: wf.Namespace,
			Labels: map[string]string{
				managedLabel:          managedLabelValue,
				"keese.ai/workflow":   wf.Name,
				"keese.ai/managed-by": workflowFieldOwner,
			},
		},
		Spec: gatewayv1.HTTPRouteSpec{
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  &pathExact,
								Value: ptr[string](cfg.Path),
							},
							Method: &postMethod,
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: svcName,
									Port: &svcPort,
								},
							},
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetOwnerReference(wf, hr, r.Scheme); err != nil {
		return fmt.Errorf("set owner ref on HTTPRoute: %w", err)
	}

	if err := r.Patch(ctx, hr, client.Apply,
		client.FieldOwner(workflowFieldOwner),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("SSA HTTPRoute %s/%s: %w", hr.Namespace, hr.Name, err)
	}

	r.setCondition(wf, conditionTypeTriggerProjected, metav1.ConditionTrue,
		ReasonTriggerHTTPRouteReady,
		fmt.Sprintf("HTTPRoute %s/%s projected (path: %s)", hr.Namespace, hr.Name, cfg.Path))
	return nil
}

// reconcileOutput projects a single output sink. Output projections (Knative Sink,
// NATS publish, S3, GitHub PR) are deferred to a follow-on TD item; this function
// is a documented intentional no-op so the reconciler loop does not block.
func (r *WorkflowReconciler) reconcileOutput(_ context.Context, _ *keesev1alpha1.Workflow, _ *keesev1alpha1.WorkflowOutput) error {
	// Output projections (KnativeSink / NATSPublish / S3 / GitHubPR) are in scope
	// for a separate TD item (TD-P2-10). This no-op is intentional and not a stub —
	// the output CRD fields are validated by the API server at admission time.
	return nil
}

// ---- name helpers -------------------------------------------------------

// cronJobName returns the deterministic CronJob name for a Workflow.
func cronJobName(wf *keesev1alpha1.Workflow) string {
	return fmt.Sprintf("keese-wf-%s-cron", wf.Name)
}

// knativeTriggerName returns the deterministic Knative Trigger name for a Workflow.
func knativeTriggerName(wf *keesev1alpha1.Workflow) string {
	return fmt.Sprintf("keese-wf-%s-trigger", wf.Name)
}

// httpRouteName returns the deterministic HTTPRoute name for a Workflow.
func httpRouteName(wf *keesev1alpha1.Workflow) string {
	return fmt.Sprintf("keese-wf-%s-webhook", wf.Name)
}

// wfLauncherServiceName returns the deterministic wf-launcher Service name for a Workflow.
// The Service itself is not projected by this controller; it is expected to be provisioned
// out-of-band (e.g. by Helm or a separate infra chart) and named according to this convention.
func wfLauncherServiceName(wf *keesev1alpha1.Workflow) string {
	return fmt.Sprintf("keese-wf-%s-launcher", wf.Name)
}

// ptrString returns nil for an empty string (for optional CronJob TimeZone field).
func ptrString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// countWorkflowRuns returns the number of WorkflowRuns that reference wf via
// spec.workflowRef. WorkflowRuns are linked to their Workflow by name within
// the same namespace (the same linkage used by reconcileDelete's cascade check
// and the WorkflowRun concurrency check), so the count is namespace-scoped.
//
// Semantics (rule 04.4 — derived state): this counts the WorkflowRuns that
// currently exist for the Workflow. The owning type's RunCount field is
// documented as "total number of WorkflowRuns created", but a monotonic
// created-counter would require reading the prior status value to increment it
// — that is exactly the spec/status coupling rule 04.4 forbids, and it could
// not decrease when a run is garbage-collected. We therefore expose a purely
// derived live count: it is recomputed from the cluster every reconcile and
// updates both up (run created) and down (run deleted). The owner watch in
// SetupWithManager keeps it fresh by requeueing the Workflow on WorkflowRun
// create/delete.
func (r *WorkflowReconciler) countWorkflowRuns(ctx context.Context, wf *keesev1alpha1.Workflow) (int64, error) {
	var runList keesev1alpha1.WorkflowRunList
	if err := r.List(ctx, &runList, client.InNamespace(wf.Namespace)); err != nil {
		return 0, fmt.Errorf("list workflowruns: %w", err)
	}
	var count int64
	for i := range runList.Items {
		if runList.Items[i].Spec.WorkflowRef.Name == wf.Name {
			count++
		}
	}
	return count, nil
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
//
// In addition to the primary Workflow watch, it watches WorkflowRuns so that
// status.runCount stays fresh: a WorkflowRun create or delete requeues the
// Workflow it references. The watch uses a create/delete-only predicate —
// WorkflowRun spec/status updates do not change the count, so requeueing on
// them would be wasted work. The primary Workflow watch keeps
// GenerationChangedPredicate so status-only writes (including the runCount
// patch itself) do not retrigger reconciliation (rule 04.4: status must not
// feed the next reconcile).
func (r *WorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// runOwnerMapper maps a WorkflowRun event to a reconcile request for the
	// Workflow named in spec.workflowRef (same-namespace linkage).
	runOwnerMapper := handler.EnqueueRequestsFromMapFunc(
		func(_ context.Context, obj client.Object) []reconcile.Request {
			run, ok := obj.(*keesev1alpha1.WorkflowRun)
			if !ok || run.Spec.WorkflowRef.Name == "" {
				return nil
			}
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Namespace: run.Namespace,
					Name:      run.Spec.WorkflowRef.Name,
				},
			}}
		},
	)

	// createDeleteOnly fires only on WorkflowRun create/delete — the two events
	// that actually change the count.
	createDeleteOnly := predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return true },
		UpdateFunc:  func(event.UpdateEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&keesev1alpha1.Workflow{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&keesev1alpha1.WorkflowRun{}, runOwnerMapper, builder.WithPredicates(createDeleteOnly)).
		Named("workflow-workflow").
		Complete(r)
}
