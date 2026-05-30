// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

const (
	workspaceFinalizer = "finalizers.workspace.keese.ai/cleanup"
	fieldOwner         = "keese-workspace-controller"

	// defaultSessionStorage is the PVC size when spec.sessionStorage is unset.
	// TODO(spec-followup): spec does not define a default; using 10Gi per task brief.
	defaultSessionStorage = "10Gi"

	// gatewayEgressPort is the Envoy AI Gateway egress port (rule 05.4).
	gatewayEgressPort = 443
	// natsEgressPort is the NATS JetStream port.
	natsEgressPort = 4222

	// gatewayServiceNamespaceDefault is the default namespace where the Envoy AI
	// Gateway service lives. Override via WorkspaceReconciler.GatewayNamespace
	// (typically wired from the KEESE_GATEWAY_NS env var in cmd/main.go).
	gatewayServiceNamespaceDefault = "keese-system"
	gatewayServiceName             = "envoy-ai-gateway"

	// saTokenExpirationSeconds is the projected SA token TTL (rule 05.3, ≤10m = 600s).
	saTokenExpirationSeconds = 600
)

// WorkspaceReconciler reconciles a Workspace object.
type WorkspaceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Rebac    WorkspaceRebacWriter

	// GatewayNamespace overrides the namespace where the Envoy AI Gateway +
	// NATS services live. Empty → gatewayServiceNamespaceDefault. Wired from
	// the KEESE_GATEWAY_NS env var in cmd/main.go.
	GatewayNamespace string
}

// gatewayNamespace returns the configured gateway namespace, or the default.
func (r *WorkspaceReconciler) gatewayNamespace() string {
	if r.GatewayNamespace != "" {
		return r.GatewayNamespace
	}
	return gatewayServiceNamespaceDefault
}

// +kubebuilder:rbac:groups=keese.ai,resources=workspaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keese.ai,resources=workspaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=workspaces/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile implements the main reconciliation loop for Workspace.
// Idiom: fetch → deepcopy for status patch → handle deletion → ensure desired state → update status.
func (r *WorkspaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var ws keesev1alpha1.Workspace
	if err := r.Get(ctx, req.NamespacedName, &ws); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig := ws.DeepCopy()

	// Handle deletion before anything else (rule 04.10).
	if !ws.DeletionTimestamp.IsZero() {
		return r.cleanup(ctx, &ws, orig)
	}

	// Ensure finalizer is present (we allocate external resources: SA, PVC, NP, FGA tuples).
	if !controllerutil.ContainsFinalizer(&ws, workspaceFinalizer) {
		controllerutil.AddFinalizer(&ws, workspaceFinalizer)
		if err := r.Patch(ctx, &ws, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		// Re-fetch after patch so orig is accurate for the next status patch.
		if err := r.Get(ctx, req.NamespacedName, &ws); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = ws.DeepCopy()
	}

	// Transition phase to Provisioning on first reconcile.
	if ws.Status.Phase == "" {
		ws.Status.Phase = keesev1alpha1.WorkspacePhasePending
	}

	// --- Ensure ServiceAccount ---
	saName := serviceAccountName(&ws)
	sa := buildServiceAccount(&ws, saName)
	if err := r.Apply(ctx, sa); err != nil {
		log.Error(err, "failed to apply ServiceAccount", "name", saName)
		r.setProgressing(&ws, "ServiceAccountFailed", err.Error())
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, r.patchStatus(ctx, &ws, orig)
	}
	r.Recorder.Eventf(&ws, corev1.EventTypeNormal, ReasonServiceAccountEnsured,
		"ServiceAccount %s ensured", saName)
	ws.Status.ServiceAccountName = saName

	// --- Ensure NetworkPolicies (default-deny + egress allowlist) ---
	npDenyName := defaultDenyNPName(&ws)
	npDeny := buildDefaultDenyNetworkPolicy(&ws, npDenyName)
	if err := r.Apply(ctx, npDeny); err != nil {
		log.Error(err, "failed to apply default-deny NetworkPolicy", "name", npDenyName)
		r.setProgressing(&ws, "NetworkPolicyFailed", err.Error())
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, r.patchStatus(ctx, &ws, orig)
	}

	npEgressName := egressNPName(&ws)
	npEgress := buildEgressNetworkPolicy(&ws, npEgressName, r.gatewayNamespace())
	if err := r.Apply(ctx, npEgress); err != nil {
		log.Error(err, "failed to apply egress NetworkPolicy", "name", npEgressName)
		r.setProgressing(&ws, "NetworkPolicyFailed", err.Error())
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, r.patchStatus(ctx, &ws, orig)
	}
	r.Recorder.Eventf(&ws, corev1.EventTypeNormal, ReasonNetworkPolicyEnsured,
		"NetworkPolicies %s and %s ensured", npDenyName, npEgressName)
	ws.Status.NetworkPolicyName = npDenyName
	setCondition(&ws.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.WorkspaceConditionNetworkIsolated,
		Status:             metav1.ConditionTrue,
		Reason:             "NetworkPoliciesApplied",
		Message:            "Default-deny and egress allow NetworkPolicies are in place",
		ObservedGeneration: ws.Generation,
	})

	// --- Ensure PVC for session state ---
	pvcName := sessionPVCName(&ws)
	pvc := buildSessionPVC(&ws, pvcName)
	if err := r.Apply(ctx, pvc); err != nil {
		log.Error(err, "failed to apply session PVC", "name", pvcName)
		r.setProgressing(&ws, "PVCFailed", err.Error())
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, r.patchStatus(ctx, &ws, orig)
	}
	r.Recorder.Eventf(&ws, corev1.EventTypeNormal, ReasonPVCEnsured,
		"Session PVC %s ensured", pvcName)

	// Check PVC is Bound before advancing to Running.
	var existingPVC corev1.PersistentVolumeClaim
	pvcBound := false
	if err := r.Get(ctx, client.ObjectKey{Name: pvcName, Namespace: ws.Namespace}, &existingPVC); err == nil {
		pvcBound = existingPVC.Status.Phase == corev1.ClaimBound
	}
	if pvcBound {
		setCondition(&ws.Status.Conditions, metav1.Condition{
			Type:               keesev1alpha1.WorkspaceConditionSessionStorageReady,
			Status:             metav1.ConditionTrue,
			Reason:             "PVCBound",
			Message:            fmt.Sprintf("PVC %s is Bound", pvcName),
			ObservedGeneration: ws.Generation,
		})
	} else {
		setCondition(&ws.Status.Conditions, metav1.Condition{
			Type:               keesev1alpha1.WorkspaceConditionSessionStorageReady,
			Status:             metav1.ConditionFalse,
			Reason:             "PVCPending",
			Message:            fmt.Sprintf("PVC %s is not yet Bound", pvcName),
			ObservedGeneration: ws.Generation,
		})
	}

	// --- Runtime Bootstrap ---
	// Session-pod-driven: each WorkspaceSession pod runs a `keese-resume`
	// init container that restores any prior SQLite checkpoint from
	// /var/run/keese/session/keese-checkpoints/<uid>/sessions.db* into
	// goose's expected sessions dir before the agent container starts.
	// See sessionInitContainers in workspacesession_controller.go.
	// The provider's SPI Bootstrap method (goose.go) is invoked by tests
	// directly when an executor is wired; in production the init-container
	// path is the canonical wire-up.

	// --- ReBAC tuples ---
	tuples := rebacTuplesFor(&ws)
	if err := r.Rebac.Sync(ctx, tuples); err != nil {
		log.Error(err, "failed to sync ReBAC tuples")
		r.setProgressing(&ws, "RebacSyncFailed", err.Error())
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, r.patchStatus(ctx, &ws, orig)
	}
	r.Recorder.Eventf(&ws, corev1.EventTypeNormal, ReasonRebacTupleWritten,
		"%d ReBAC tuples synced", len(tuples))
	ws.Status.RebacTupleCount = int32(len(tuples)) //nolint:gosec

	// --- Advance FSM ---
	switch ws.Status.Phase {
	case keesev1alpha1.WorkspacePhasePending, keesev1alpha1.WorkspacePhaseProvisioning:
		if pvcBound {
			ws.Status.Phase = keesev1alpha1.WorkspacePhaseRunning
			r.Recorder.Eventf(&ws, corev1.EventTypeNormal, ReasonWorkspaceProvisioned,
				"Workspace %s provisioned", ws.Name)
		} else {
			ws.Status.Phase = keesev1alpha1.WorkspacePhaseProvisioning
		}
	}

	// --- Update status conditions ---
	ws.Status.ObservedGeneration = ws.Generation
	setCondition(&ws.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.WorkspaceConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "ReconcileComplete",
		Message:            "Reconcile completed successfully",
		ObservedGeneration: ws.Generation,
	})

	readyStatus := metav1.ConditionFalse
	readyReason := "Provisioning"
	readyMsg := "Workspace is being provisioned"
	if ws.Status.Phase == keesev1alpha1.WorkspacePhaseRunning {
		readyStatus = metav1.ConditionTrue
		readyReason = "Ready"
		readyMsg = "Workspace is running"
		r.Recorder.Eventf(&ws, corev1.EventTypeNormal, ReasonWorkspaceReady,
			"Workspace %s is ready", ws.Name)
	}
	setCondition(&ws.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.WorkspaceConditionReady,
		Status:             readyStatus,
		Reason:             readyReason,
		Message:            readyMsg,
		ObservedGeneration: ws.Generation,
	})

	return ctrl.Result{}, r.patchStatus(ctx, &ws, orig)
}

// cleanup runs when DeletionTimestamp is set. It drains sessions, removes
// sub-resources, deletes ReBAC tuples, then removes the finalizer.
func (r *WorkspaceReconciler) cleanup(ctx context.Context, ws *keesev1alpha1.Workspace, orig *keesev1alpha1.Workspace) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(ws, workspaceFinalizer) {
		return ctrl.Result{}, nil
	}

	ws.Status.Phase = keesev1alpha1.WorkspacePhaseTerminating
	r.Recorder.Eventf(ws, corev1.EventTypeNormal, ReasonWorkspaceTerminating,
		"Workspace %s is terminating", ws.Name)

	// Delete ReBAC tuples.
	tuples := rebacTuplesFor(ws)
	if err := r.Rebac.Delete(ctx, tuples); err != nil {
		log.Error(err, "failed to delete ReBAC tuples; will retry")
		r.Recorder.Eventf(ws, corev1.EventTypeWarning, ReasonRebacTupleDeleteFailed,
			"ReBAC tuple deletion failed: %v", err)
		_ = r.patchStatus(ctx, ws, orig)
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, nil
	}

	// Delete NetworkPolicies.
	for _, npName := range []string{defaultDenyNPName(ws), egressNPName(ws)} {
		np := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: npName, Namespace: ws.Namespace},
		}
		if err := r.Delete(ctx, np); err != nil && !errors.IsNotFound(err) {
			log.Error(err, "failed to delete NetworkPolicy", "name", npName)
			return ctrl.Result{RequeueAfter: requeueAfterBackoff}, nil
		}
	}

	// Delete PVC.
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: sessionPVCName(ws), Namespace: ws.Namespace},
	}
	if err := r.Delete(ctx, pvc); err != nil && !errors.IsNotFound(err) {
		log.Error(err, "failed to delete session PVC")
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, nil
	}

	// Delete ServiceAccount.
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName(ws), Namespace: ws.Namespace},
	}
	if err := r.Delete(ctx, sa); err != nil && !errors.IsNotFound(err) {
		log.Error(err, "failed to delete ServiceAccount")
		return ctrl.Result{RequeueAfter: requeueAfterBackoff}, nil
	}

	// All cleanup done — remove finalizer.
	controllerutil.RemoveFinalizer(ws, workspaceFinalizer)
	return ctrl.Result{}, r.Patch(ctx, ws, client.MergeFrom(orig))
}

// Apply issues a Server-Side Apply patch with the workspace field owner.
func (r *WorkspaceReconciler) Apply(ctx context.Context, obj client.Object) error {
	return r.Client.Patch(ctx, obj, client.Apply,
		client.FieldOwner(fieldOwner),
		client.ForceOwnership)
}

// patchStatus patches only the status subresource, preserving spec.
func (r *WorkspaceReconciler) patchStatus(ctx context.Context, ws *keesev1alpha1.Workspace, orig *keesev1alpha1.Workspace) error {
	return r.Status().Patch(ctx, ws, client.MergeFrom(orig))
}

// setProgressing sets the Progressing condition to True with the given reason/message.
func (r *WorkspaceReconciler) setProgressing(ws *keesev1alpha1.Workspace, reason, msg string) {
	setCondition(&ws.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.WorkspaceConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: ws.Generation,
	})
	setCondition(&ws.Status.Conditions, metav1.Condition{
		Type:               keesev1alpha1.WorkspaceConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: ws.Generation,
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkspaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Rebac == nil {
		r.Rebac = WorkspaceNoopRebacWriter{}
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("workspace-controller")
	}
	// Predicate-free reconcile. Per
	// docs/designs/26-workspace-managed-predicate-adr.md, the
	// keese.ai/managed=true predicate is permanently dropped:
	// every Workspace in the keese.ai API group is reconciled
	// unconditionally. Suspended-state semantics, if needed
	// later, go in spec — not in a label predicate.
	return ctrl.NewControllerManagedBy(mgr).
		For(&keesev1alpha1.Workspace{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("workspace").
		Complete(r)
}

// --- Resource builders ---

func serviceAccountName(ws *keesev1alpha1.Workspace) string {
	return "ksa-" + string(ws.UID)
}

func defaultDenyNPName(ws *keesev1alpha1.Workspace) string {
	return "keese-workspace-" + string(ws.UID) + "-default-deny"
}

func egressNPName(ws *keesev1alpha1.Workspace) string {
	return "keese-workspace-" + string(ws.UID) + "-egress"
}

func sessionPVCName(ws *keesev1alpha1.Workspace) string {
	return "keese-ws-" + string(ws.UID) + "-session"
}

func buildServiceAccount(ws *keesev1alpha1.Workspace, name string) *corev1.ServiceAccount {
	sa := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ws.Namespace,
			Labels:    resourceLabels(ws),
		},
	}
	return sa
}

// buildDefaultDenyNetworkPolicy creates a fail-closed default-deny NetworkPolicy
// for the workspace namespace (rule 04.17, rule 05.4).
func buildDefaultDenyNetworkPolicy(ws *keesev1alpha1.Workspace, name string) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ws.Namespace,
			Labels:    resourceLabels(ws),
		},
		Spec: networkingv1.NetworkPolicySpec{
			// Select all pods in the namespace with the workspace label.
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"keese.ai/workspace": ws.Name,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			// No Ingress or Egress rules → deny all by default (fail-closed).
		},
	}
}

// buildEgressNetworkPolicy creates an allowlist for egress to the Envoy AI Gateway (:443)
// and NATS (:4222) in the workspace namespace (rule 04.17, rule 05.4, 05.5).
// gatewayNS is the namespace where the in-cluster NATS service runs (also where
// the AI Gateway is deployed in the default topology).
func buildEgressNetworkPolicy(ws *keesev1alpha1.Workspace, name, gatewayNS string) *networkingv1.NetworkPolicy {
	_ = gatewayEgressPort // documented for production use; see comment in egress rule below
	natsPort := intstr.FromInt(natsEgressPort)
	dnsPort := intstr.FromInt(53)
	tcpProto := corev1.ProtocolTCP
	udpProto := corev1.ProtocolUDP

	return &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ws.Namespace,
			Labels:    resourceLabels(ws),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"keese.ai/workspace": ws.Name,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				// Allow DNS to kube-dns (UDP+TCP 53) — without this the
				// agent pod cannot resolve envoy-ai-gateway.keese-system.svc
				// or any other in-cluster Service name.
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &udpProto, Port: &dnsPort},
						{Protocol: &tcpProto, Port: &dnsPort},
					},
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "kube-system",
								},
							},
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"k8s-app": "kube-dns",
								},
							},
						},
					},
				},
				// Allow egress to Envoy AI Gateway pods.
				//
				// We deliberately do NOT pin a port here. Kubernetes
				// NetworkPolicy port matching applies to the destination
				// POD's container port (after kube-proxy DNAT), not the
				// Service port the client dials (rule 04.17 + design
				// 12-network-isolation). The envoy-gateway proxy pod's
				// listener port is chosen by the upstream chart (10443
				// in v1.4.x) and not in our control. Locking to :443
				// here means traffic to the Service IP routes to a pod
				// port that doesn't match the rule and gets dropped.
				//
				// The egress is still constrained by namespace+pod
				// selector, which is the actual security boundary
				// (rule 05.4: "All egress through Envoy AI Gateway,
				// fail-closed"). Production CNIs that support
				// service-port matching (Cilium with EnableServiceTopology,
				// Calico with named ports) can re-pin the port — flagged
				// as TD-P2-X.
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "envoy-gateway-system",
								},
							},
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app.kubernetes.io/managed-by": "envoy-gateway",
								},
							},
						},
					},
				},
				// Allow egress to NATS JetStream :4222.
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcpProto, Port: &natsPort},
					},
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": gatewayNS,
								},
							},
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app.kubernetes.io/name": "nats",
								},
							},
						},
					},
				},
			},
		},
	}
}

func buildSessionPVC(ws *keesev1alpha1.Workspace, name string) *corev1.PersistentVolumeClaim {
	storageSize := resource.MustParse(defaultSessionStorage)
	if ws.Spec.SessionStorage != nil {
		storageSize = *ws.Spec.SessionStorage
	}
	// StorageClassName is intentionally omitted (nil) so the cluster
	// default StorageClass is used. A pointer to an empty string here
	// would mean "no storage class" — provisioning would never bind.
	return &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "PersistentVolumeClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ws.Namespace,
			Labels:    resourceLabels(ws),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageSize,
				},
			},
		},
	}
}

// rebacTuplesFor computes the full desired set of ReBAC tuples for the workspace.
// All tuples are idempotent — Sync is a check-before-write operation.
func rebacTuplesFor(ws *keesev1alpha1.Workspace) []WorkspaceRebacTuple {
	var tuples []WorkspaceRebacTuple
	wsObj := "workspace:" + ws.Name
	tenant := ws.Spec.TenantRef.Name
	saName := serviceAccountName(ws)

	// tenant membership for the workspace SA.
	if tenant != "" {
		tuples = append(tuples, WorkspaceRebacTuple{
			Object:   "tenant:" + tenant,
			Relation: "member",
			User:     "service_account:" + saName,
		})
		// workspace is owned by the tenant.
		tuples = append(tuples, WorkspaceRebacTuple{
			Object:   wsObj,
			Relation: "owner",
			User:     "tenant:" + tenant,
		})
	}

	// Per-user editor tuples.
	for _, editor := range ws.Spec.Editors {
		tuples = append(tuples, WorkspaceRebacTuple{
			Object:   wsObj,
			Relation: "editor",
			User:     "user:" + editor,
		})
	}

	// Per-user viewer tuples.
	for _, viewer := range ws.Spec.Viewers {
		tuples = append(tuples, WorkspaceRebacTuple{
			Object:   wsObj,
			Relation: "viewer",
			User:     "user:" + viewer,
		})
	}

	// Per-tool egress allowlist tuples (D22 ToolBinding). One
	// tuple per spec.egress.allowedTools[] entry. The keese-authz
	// ext_authz Check resolves `tool:<name>#can_call@<subject>` =
	// `tenant_member from allowed_in` — these tuples are the
	// `allowed_in` half. Closes the orphan-tuple gap from
	// TD-P1-01 (the `tool:` type was declared in the FGA model
	// but no controller wrote its tuples).
	if ws.Spec.Egress != nil {
		for _, name := range ws.Spec.Egress.AllowedTools {
			if name == "" {
				continue
			}
			tuples = append(tuples, WorkspaceRebacTuple{
				Object:   "tool:" + name,
				Relation: "allowed_in",
				User:     "workspace:" + ws.Name,
			})
		}
	}

	return tuples
}

func resourceLabels(ws *keesev1alpha1.Workspace) map[string]string {
	return map[string]string{
		"keese.ai/workspace":           ws.Name,
		"app.kubernetes.io/managed-by": fieldOwner,
	}
}
