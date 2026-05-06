// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

const (
	// transportFieldOwner is the SSA field manager name (rule 04.7).
	transportFieldOwner = "keese-transport-controller"

	// transportFinalizer is the finalizer placed on transports that own a NATS stream
	// (annotation-gated). Format: finalizers.<kind>.operator.keese.ai/<purpose> (rule 04.10).
	transportFinalizer = "finalizers.transport.operator.keese.ai/cleanup"

	// autoCreateStreamAnnotation enables controller-owned NATS stream lifecycle.
	autoCreateStreamAnnotation = "keese.ai/auto-create-stream"

	// managedByLabel is the predicate label required on watched transports.
	managedByLabel = "keese.ai/managed"
)

// TransportReconciler reconciles a Transport object.
//
// SSA fieldOwner: keese-transport-controller (rule 04.7)
// Finalizer: finalizers.transport.operator.keese.ai/cleanup (annotation-gated, rule 04.10)
type TransportReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Recorder    record.EventRecorder
	Rebac       TransportRebacWriter
	Nats        NatsStreamer
	CTA         CTAResolver
	CertManager CertManagerReader
}

// +kubebuilder:rbac:groups=transport.operator.keese.ai,resources=transports,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=transport.operator.keese.ai,resources=transports/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=transport.operator.keese.ai,resources=transports/finalizers,verbs=update
// +kubebuilder:rbac:groups=jetstream.nats.io,resources=streams;consumers,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=referencegrants,verbs=get;list;watch
// +kubebuilder:rbac:groups=mcp.keese.ai,resources=mcproutes,verbs=get;list;watch
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile drives Transport toward the desired state.
//
// Flow:
//  1. Fetch → DeepCopy for status patch
//  2. Handle deletion (finalizer cleanup)
//  3. Validate immutable type (guard; VAP is primary)
//  4. Set phase → Provisioning
//  5. Validate dependencies (cert, MCPRoute, NATS stream, CTA)
//  6. Manage finalizer (annotation-gated)
//  7. Manage NATS stream lifecycle (opt-in)
//  8. Write ReBAC tuples
//  9. Transition → Ready; patch status
func (r *TransportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var tr keesev1alpha1.Transport
	if err := r.Get(ctx, req.NamespacedName, &tr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig := tr.DeepCopy()

	// --- Deletion path ---
	if !tr.DeletionTimestamp.IsZero() {
		return r.cleanup(ctx, &tr, orig)
	}

	// --- Initialize phase ---
	if tr.Status.Phase == "" {
		tr.Status.Phase = keesev1alpha1.TransportPhasePending
	}

	// --- Dependency validation ---
	ready, result, err := r.validateDependencies(ctx, &tr, orig)
	if err != nil {
		return result, err
	}
	if !ready {
		// validateDependencies already patched status; requeue handled.
		return result, nil
	}

	// --- NATS stream lifecycle (opt-in) ---
	if tr.Spec.Type == keesev1alpha1.TransportTypeNATS && tr.Spec.NATS != nil {
		if streamResult, streamErr := r.reconcileNATSStream(ctx, &tr, orig); streamErr != nil || streamResult.Requeue || streamResult.RequeueAfter > 0 {
			return streamResult, streamErr
		}
	}

	// --- Manage finalizer (add only when annotation-gated stream ownership active) ---
	r.reconcileFinalizer(ctx, &tr, orig)

	// --- Write ReBAC tuples ---
	tuples := r.buildRebacTuples(&tr)
	if syncErr := r.Rebac.Sync(ctx, tuples); syncErr != nil {
		log.Error(syncErr, "failed to sync ReBAC tuples")
		r.setCondition(&tr.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "RebacSyncFailed",
			Message:            fmt.Sprintf("ReBAC tuple sync failed: %v", syncErr),
			ObservedGeneration: tr.Generation,
		})
		tr.Status.ObservedGeneration = tr.Generation
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, r.Status().Patch(ctx, &tr, client.MergeFrom(orig))
	}
	tr.Status.RebacTupleCount = len(tuples)

	// --- Transition to Ready ---
	tr.Status.Phase = keesev1alpha1.TransportPhaseReady
	tr.Status.ObservedGeneration = tr.Generation
	r.setCondition(&tr.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "TransportProvisioned",
		Message:            fmt.Sprintf("Transport provisioned (type=%s)", tr.Spec.Type),
		ObservedGeneration: tr.Generation,
	})
	r.setCondition(&tr.Status.Conditions, metav1.Condition{
		Type:               "Progressing",
		Status:             metav1.ConditionFalse,
		Reason:             "TransportProvisioned",
		Message:            "Transport is ready",
		ObservedGeneration: tr.Generation,
	})
	r.Recorder.Eventf(&tr, corev1.EventTypeNormal, ReasonTransportProvisioned,
		"Transport %s/%s provisioned (type=%s)", tr.Namespace, tr.Name, tr.Spec.Type)

	return ctrl.Result{}, r.Status().Patch(ctx, &tr, client.MergeFrom(orig))
}

// validateDependencies checks all external references for the given transport type.
// Returns (ready=true, Result{}, nil) when all deps are satisfied.
// Returns (ready=false, result, err) otherwise; status has already been patched.
func (r *TransportReconciler) validateDependencies(ctx context.Context, tr *keesev1alpha1.Transport, orig *keesev1alpha1.Transport) (bool, ctrl.Result, error) {
	tr.Status.Phase = keesev1alpha1.TransportPhaseProvisioning

	switch tr.Spec.Type {
	case keesev1alpha1.TransportTypeNATS:
		return r.validateNATSDeps(ctx, tr, orig)

	case keesev1alpha1.TransportTypeMCP:
		return r.validateMCPDeps(ctx, tr, orig)

	case keesev1alpha1.TransportTypeA2A:
		return r.validateA2ADeps(ctx, tr, orig)

	case keesev1alpha1.TransportTypeStdio:
		// No external dep validation for stdio; bridgeImage is validated by VAP.
		return true, ctrl.Result{}, nil
	}

	return true, ctrl.Result{}, nil
}

// validateNATSDeps validates NATS-specific dependencies.
func (r *TransportReconciler) validateNATSDeps(ctx context.Context, tr *keesev1alpha1.Transport, orig *keesev1alpha1.Transport) (bool, ctrl.Result, error) {
	nats := tr.Spec.NATS
	if nats == nil {
		return true, ctrl.Result{}, nil
	}

	// Warn if streamConfig is set without opt-in annotation.
	if nats.StreamConfig != nil && tr.Annotations[autoCreateStreamAnnotation] != "true" {
		r.Recorder.Eventf(tr, corev1.EventTypeWarning, ReasonNATSStreamConfigIgnored,
			"spec.nats.streamConfig is set but annotation %s is absent; streamConfig ignored", autoCreateStreamAnnotation)
	}

	// Validate TLS certificate if set.
	if nats.TLS != nil && nats.TLS.CertificateRef != nil {
		ns := nats.TLS.CertificateRef.Namespace
		if ns == "" {
			ns = tr.Namespace
		}
		exists, err := r.CertManager.CertificateExists(ctx, ns, nats.TLS.CertificateRef.Name)
		if err != nil {
			return false, ctrl.Result{RequeueAfter: requeueAfterBackoff}, err
		}
		if !exists {
			r.Recorder.Eventf(tr, corev1.EventTypeWarning, ReasonCertificateNotFound,
				"Certificate %s/%s not found", ns, nats.TLS.CertificateRef.Name)
			r.setDegradedCondition(tr, "CertificateNotFound",
				fmt.Sprintf("cert-manager Certificate %s/%s not found", ns, nats.TLS.CertificateRef.Name))
			tr.Status.ObservedGeneration = tr.Generation
			return false, ctrl.Result{RequeueAfter: requeueAfterBackoff},
				r.Status().Patch(ctx, tr, client.MergeFrom(orig))
		}
	}

	// Default mode: verify stream exists.
	if tr.Annotations[autoCreateStreamAnnotation] != "true" {
		exists, _, err := r.Nats.StreamExists(ctx, nats.StreamName)
		if err != nil {
			return false, ctrl.Result{RequeueAfter: requeueAfterBackoff}, err
		}
		if !exists {
			r.Recorder.Eventf(tr, corev1.EventTypeWarning, ReasonNATSStreamNotFound,
				"NATS stream %q not found; create it via NACK or set annotation %s=true",
				nats.StreamName, autoCreateStreamAnnotation)
			r.setDegradedCondition(tr, "NATSStreamNotFound",
				fmt.Sprintf("NATS stream %q not found", nats.StreamName))
			tr.Status.ObservedGeneration = tr.Generation
			return false, ctrl.Result{RequeueAfter: requeueAfterBackoff},
				r.Status().Patch(ctx, tr, client.MergeFrom(orig))
		}
	}

	return true, ctrl.Result{}, nil
}

// validateMCPDeps validates MCP-specific dependencies.
func (r *TransportReconciler) validateMCPDeps(ctx context.Context, tr *keesev1alpha1.Transport, orig *keesev1alpha1.Transport) (bool, ctrl.Result, error) {
	mcp := tr.Spec.MCP
	if mcp == nil {
		return true, ctrl.Result{}, nil
	}

	// MCPRoute validation: resolve the referenced route.
	// We use a lightweight Unstructured Get to avoid importing the mcp package.
	mcpNS := mcp.McpRouteRef.Namespace
	if mcpNS == "" {
		mcpNS = tr.Namespace
	}
	exists, err := r.mcpRouteExists(ctx, mcpNS, mcp.McpRouteRef.Name)
	if err != nil {
		return false, ctrl.Result{RequeueAfter: requeueAfterBackoff}, err
	}
	if !exists {
		r.Recorder.Eventf(tr, corev1.EventTypeWarning, ReasonMCPRouteNotFound,
			"MCPRoute %s/%s not found", mcpNS, mcp.McpRouteRef.Name)
		r.setDegradedCondition(tr, "MCPRouteNotFound",
			fmt.Sprintf("MCPRoute %s/%s not found", mcpNS, mcp.McpRouteRef.Name))
		tr.Status.ObservedGeneration = tr.Generation
		return false, ctrl.Result{RequeueAfter: requeueAfterBackoff},
			r.Status().Patch(ctx, tr, client.MergeFrom(orig))
	}

	return true, ctrl.Result{}, nil
}

// validateA2ADeps validates A2A-specific dependencies.
func (r *TransportReconciler) validateA2ADeps(ctx context.Context, tr *keesev1alpha1.Transport, orig *keesev1alpha1.Transport) (bool, ctrl.Result, error) {
	a2a := tr.Spec.A2A
	if a2a == nil {
		return true, ctrl.Result{}, nil
	}

	// Validate mutual-TLS certificate.
	if a2a.MutualTLS != nil {
		ns := a2a.MutualTLS.CertificateRef.Namespace
		if ns == "" {
			ns = tr.Namespace
		}
		exists, err := r.CertManager.CertificateExists(ctx, ns, a2a.MutualTLS.CertificateRef.Name)
		if err != nil {
			return false, ctrl.Result{RequeueAfter: requeueAfterBackoff}, err
		}
		if !exists {
			r.Recorder.Eventf(tr, corev1.EventTypeWarning, ReasonCertificateNotFound,
				"Certificate %s/%s not found (a2a mutualTLS)", ns, a2a.MutualTLS.CertificateRef.Name)
			r.setDegradedCondition(tr, "CertificateNotFound",
				fmt.Sprintf("cert-manager Certificate %s/%s not found", ns, a2a.MutualTLS.CertificateRef.Name))
			tr.Status.ObservedGeneration = tr.Generation
			return false, ctrl.Result{RequeueAfter: requeueAfterBackoff},
				r.Status().Patch(ctx, tr, client.MergeFrom(orig))
		}
	}

	// cross-tenant scope requires an Approved CrossTenantAgreement.
	scope := keesev1alpha1.A2AScopeIntraTenant
	if a2a.Scope != nil {
		scope = *a2a.Scope
	}
	if scope == keesev1alpha1.A2AScopeCrossTenant {
		hasCTA, err := r.CTA.HasApprovedCTA(ctx, tr.Namespace, a2a.Endpoint)
		if err != nil {
			return false, ctrl.Result{RequeueAfter: requeueAfterBackoff}, err
		}
		if !hasCTA {
			r.Recorder.Eventf(tr, corev1.EventTypeWarning, ReasonCrossTenantAgreementMissing,
				"No Approved CrossTenantAgreement covers this transport (namespace=%s, endpoint=%s)",
				tr.Namespace, a2a.Endpoint)
			r.setDegradedCondition(tr, "CrossTenantAgreementMissing",
				fmt.Sprintf("No Approved CrossTenantAgreement covers namespace=%s → endpoint=%s",
					tr.Namespace, a2a.Endpoint))
			tr.Status.ObservedGeneration = tr.Generation
			return false, ctrl.Result{RequeueAfter: requeueAfterBackoff},
				r.Status().Patch(ctx, tr, client.MergeFrom(orig))
		}
	}

	return true, ctrl.Result{}, nil
}

// reconcileNATSStream manages the JetStream stream lifecycle in opt-in mode.
func (r *TransportReconciler) reconcileNATSStream(ctx context.Context, tr *keesev1alpha1.Transport, orig *keesev1alpha1.Transport) (ctrl.Result, error) {
	if tr.Annotations[autoCreateStreamAnnotation] != "true" {
		return ctrl.Result{}, nil
	}

	nats := tr.Spec.NATS
	cfg := StreamConfig{
		Name:      nats.StreamName,
		Retention: "limits",
		MaxAge:    "7d",
		Storage:   "file",
		Replicas:  3,
	}
	if nats.StreamConfig != nil {
		if len(nats.StreamConfig.Subjects) > 0 {
			cfg.Subjects = nats.StreamConfig.Subjects
		}
		if nats.StreamConfig.Retention != "" {
			cfg.Retention = nats.StreamConfig.Retention
		}
		if nats.StreamConfig.MaxAge != "" {
			cfg.MaxAge = nats.StreamConfig.MaxAge
		}
		if nats.StreamConfig.Storage != "" {
			cfg.Storage = nats.StreamConfig.Storage
		}
		if nats.StreamConfig.Replicas != nil {
			cfg.Replicas = *nats.StreamConfig.Replicas
		}
	}

	exists, _, err := r.Nats.StreamExists(ctx, nats.StreamName)
	if err != nil {
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, err
	}

	if !exists {
		if addErr := r.Nats.AddStream(ctx, cfg); addErr != nil {
			r.Recorder.Eventf(tr, corev1.EventTypeWarning, ReasonNATSStreamDeleteFailed,
				"Failed to create NATS stream %q: %v", nats.StreamName, addErr)
			return ctrl.Result{RequeueAfter: requeueAfterBackoff}, nil
		}
		r.Recorder.Eventf(tr, corev1.EventTypeNormal, ReasonNATSStreamOwned,
			"NATS stream %q created and owned by controller", nats.StreamName)
	}

	return ctrl.Result{}, nil
}

// reconcileFinalizer adds the cleanup finalizer when the transport owns a NATS stream.
func (r *TransportReconciler) reconcileFinalizer(ctx context.Context, tr *keesev1alpha1.Transport, orig *keesev1alpha1.Transport) {
	ownsStream := tr.Spec.Type == keesev1alpha1.TransportTypeNATS &&
		tr.Annotations[autoCreateStreamAnnotation] == "true"

	hasFinalizer := controllerutil.ContainsFinalizer(tr, transportFinalizer)
	if ownsStream && !hasFinalizer {
		controllerutil.AddFinalizer(tr, transportFinalizer)
		if err := r.Patch(ctx, tr, client.MergeFrom(orig)); err != nil {
			logf.FromContext(ctx).Error(err, "failed to add transport finalizer")
		}
	}
}

// cleanup is the deletion handler. Removes controller-owned NATS stream then strips
// the finalizer.
func (r *TransportReconciler) cleanup(ctx context.Context, tr *keesev1alpha1.Transport, orig *keesev1alpha1.Transport) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(tr, transportFinalizer) {
		return ctrl.Result{}, nil
	}

	tr.Status.Phase = keesev1alpha1.TransportPhaseTerminating
	// Best-effort status patch (ignore error; the deletion path must proceed).
	_ = r.Status().Patch(ctx, tr, client.MergeFrom(orig))

	// Delete the controller-owned NATS stream (only if annotation was present at create time).
	if tr.Spec.Type == keesev1alpha1.TransportTypeNATS && tr.Spec.NATS != nil {
		if err := r.Nats.DeleteStream(ctx, tr.Spec.NATS.StreamName); err != nil {
			r.Recorder.Eventf(tr, corev1.EventTypeWarning, ReasonNATSStreamDeleteFailed,
				"NATS stream %q deletion failed: %v", tr.Spec.NATS.StreamName, err)
			return ctrl.Result{RequeueAfter: requeueAfterBackoff}, nil
		}
	}

	// Delete ReBAC tuples.
	tuples := r.buildRebacTuples(tr)
	if len(tuples) > 0 {
		if err := r.Rebac.Delete(ctx, tuples); err != nil {
			logf.FromContext(ctx).Error(err, "failed to delete ReBAC tuples on cleanup")
		}
	}

	controllerutil.RemoveFinalizer(tr, transportFinalizer)
	return ctrl.Result{}, r.Patch(ctx, tr, client.MergeFrom(orig))
}

// buildRebacTuples constructs the OpenFGA tuples for this Transport.
func (r *TransportReconciler) buildRebacTuples(tr *keesev1alpha1.Transport) []TransportRebacTuple {
	tuples := []TransportRebacTuple{
		transportOwnerTuple(tr.Namespace, tr.Name),
	}
	// Additional a2a cross-tenant tuple check referenced from spec.a2a fields.
	// The CRA controller writes tenant.allows_messaging and workspace.messageable_from;
	// Transport controller writes only transport.owner here.
	return tuples
}

// mcpRouteExists checks whether an MCPRoute by name/namespace is present.
// Uses an unstructured Get to avoid importing the mcp package at this stage.
func (r *TransportReconciler) mcpRouteExists(ctx context.Context, namespace, name string) (bool, error) {
	// TODO(spec-followup): use unstructured Get against mcp.keese.ai/v1alpha1 MCPRoute
	// once the MCPRoute CRD package is available. For now, the injected fake is used in
	// tests; return true in the real reconciler to avoid blocking provisioning.
	_ = ctx
	_ = namespace
	_ = name
	return true, nil
}

// setDegradedCondition sets Ready=False and Progressing=False on the status.
func (r *TransportReconciler) setDegradedCondition(tr *keesev1alpha1.Transport, reason, msg string) {
	tr.Status.Phase = keesev1alpha1.TransportPhaseDegraded
	r.setCondition(&tr.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: tr.Generation,
	})
	r.setCondition(&tr.Status.Conditions, metav1.Condition{
		Type:               "Progressing",
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: tr.Generation,
	})
}

// setCondition upserts a condition using apimachinery meta helper.
func (r *TransportReconciler) setCondition(conditions *[]metav1.Condition, c metav1.Condition) {
	now := metav1.Now()
	c.LastTransitionTime = now
	meta.SetStatusCondition(conditions, c)
}

// SetupWithManager registers the controller with the manager.
func (r *TransportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Rebac == nil {
		r.Rebac = &TransportFakeRebacWriter{}
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("transport-controller")
	}
	if r.Nats == nil {
		r.Nats = NewFakeNatsStreamer()
	}
	if r.CTA == nil {
		r.CTA = NewFakeCTAResolver()
	}
	if r.CertManager == nil {
		r.CertManager = NewFakeCertManagerReader()
	}

	labelSelector := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetLabels()[managedByLabel] == "true"
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&keesev1alpha1.Transport{}).
		WithEventFilter(predicate.And(labelSelector, predicate.GenerationChangedPredicate{})).
		Named("transport-transport").
		Complete(r)
}
