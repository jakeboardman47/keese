// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
	policyv1alpha1 "github.com/keese-ai/keese/api/policy/v1alpha1"
	"github.com/keese-ai/keese/internal/runtime/providers/adkpython"
)

const (
	sessionFinalizer      = "finalizers.workspacesession.keese.ai/cleanup"
	sessionFieldOwner     = "keese-workspacesession-controller"
	sessionRequeueBackoff = 5 * time.Second

	// Annotation keys used to carry per-session context from other controllers.
	//
	// AnnotationWorkflowRunUID is set by the Workflow controller on a
	// WorkspaceSession when the session is spawned inside a WorkflowRun pod
	// tree. The value is the WorkflowRun .metadata.uid; it drives the
	// keese-wf-<uid> projected SA token audience (design 04b §workflowRun).
	AnnotationWorkflowRunUID = "keese.ai/workflowrun-uid"

	// AnnotationSupervisorUID is set on the parent Workspace when
	// Workspace.spec.supervisorRef is configured (design 23 / design 04b
	// §supervisor). The value is the workspace UID used to scope the
	// keese-supervisor-<ws-uid> projected SA token audience.
	// This annotation is read from Workspace.metadata.annotations at
	// pod-build time so that no API field change is required before the
	// SupervisorRef field lands in v1alpha1 (TD-P2-15 scope constraint).
	AnnotationSupervisorUID = "keese.ai/supervisor-uid"

	// Token mount paths (rule 05.7 — projected files, never env var values).
	tokenMountDir        = "/var/run/keese/tokens"
	tokenPathEgress      = tokenMountDir + "/egress"
	tokenPathWorkflowRun = tokenMountDir + "/workflowRun"
	tokenPathSupervisor  = tokenMountDir + "/supervisor"

	// sessionConditionReady is the Ready condition type for WorkspaceSession.
	sessionConditionReady = "Ready"
	// sessionConditionProgressing is the Progressing condition type for WorkspaceSession.
	sessionConditionProgressing = "Progressing"
	// sessionConditionAttached is the Attached condition type for WorkspaceSession.
	sessionConditionAttached = "Attached"

	// sessionConditionTokenBudgetWithinLimit is set True when the tenant's TokenBudget
	// is present and consumed < limit, or absent (unlimited default — see checkTokenBudget).
	sessionConditionTokenBudgetWithinLimit = "TokenBudgetWithinLimit"

	// sessionConditionTokenBudgetExceeded is set True when the tenant's TokenBudget
	// is present and consumed >= limit, causing pod provisioning to be refused.
	sessionConditionTokenBudgetExceeded = "TokenBudgetExceeded"
)

// WorkspaceSessionReconciler reconciles a WorkspaceSession object.
// SSA fieldOwner: keese-workspacesession-controller (rule 04.7)
//
// Finalizers managed:
//   - finalizers.workspacesession.keese.ai/cleanup
//     Steps: Draining → delete Pod → remove session-scoped OpenFGA tuples → remove finalizer.
type WorkspaceSessionReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Rebac    WorkspaceRebacWriter
}

// +kubebuilder:rbac:groups=keese.ai,resources=workspacesessions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keese.ai,resources=workspacesessions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=workspacesessions/finalizers,verbs=update
// +kubebuilder:rbac:groups=keese.ai,resources=workspaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=policy.keese.ai,resources=tokenbudgets,verbs=get;list;watch

// Reconcile is the main reconciliation loop for WorkspaceSession.
// Idiom: fetch → DeepCopy for status patch → handle deletion → ensure desired state → update status.
func (r *WorkspaceSessionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var sess keesev1alpha1.WorkspaceSession
	if err := r.Get(ctx, req.NamespacedName, &sess); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig := sess.DeepCopy()

	// Handle deletion before anything else (rule 04.10).
	if !sess.DeletionTimestamp.IsZero() {
		return r.cleanupSession(ctx, &sess, orig)
	}

	// Ensure finalizer (external resources: pod, OpenFGA tuples).
	if !controllerutil.ContainsFinalizer(&sess, sessionFinalizer) {
		controllerutil.AddFinalizer(&sess, sessionFinalizer)
		if err := r.Patch(ctx, &sess, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding session finalizer: %w", err)
		}
		// Re-fetch so orig is accurate for subsequent status patches.
		if err := r.Get(ctx, req.NamespacedName, &sess); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		orig = sess.DeepCopy()
	}

	// --- Prerequisite: parent Workspace must exist and be interactive ---
	var ws keesev1alpha1.Workspace
	wsKey := client.ObjectKey{Name: sess.Spec.WorkspaceRef, Namespace: sess.Namespace}
	if err := r.Get(ctx, wsKey, &ws); err != nil {
		if errors.IsNotFound(err) {
			log.Info("parent Workspace not found; requeuing", "workspace", sess.Spec.WorkspaceRef)
			r.setSessionProgressing(&sess, "WorkspaceNotFound",
				fmt.Sprintf("Workspace %s not found", sess.Spec.WorkspaceRef))
			return ctrl.Result{RequeueAfter: sessionRequeueBackoff}, r.patchSessionStatus(ctx, &sess, orig)
		}
		return ctrl.Result{}, err
	}

	if !ws.Spec.Interactive {
		log.Info("parent Workspace is not interactive; rejecting attach",
			"workspace", ws.Name, "session", sess.Name)
		r.Recorder.Eventf(&sess, corev1.EventTypeWarning,
			ReasonSessionAttachRejectedNonInteractive,
			"Workspace %s does not have spec.interactive=true; attach rejected", ws.Name)
		setSessionCondition(&sess.Status.Conditions, metav1.Condition{
			Type:               sessionConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "NonInteractiveWorkspace",
			Message:            fmt.Sprintf("Workspace %s does not have spec.interactive=true", ws.Name),
			ObservedGeneration: sess.Generation,
		})
		sess.Status.ObservedGeneration = sess.Generation
		// Do not requeue — spec is immutable, will never become valid without a new object.
		return ctrl.Result{}, r.patchSessionStatus(ctx, &sess, orig)
	}

	// --- Transition phase from zero to Pending on first reconcile ---
	if sess.Status.Phase == "" {
		sess.Status.Phase = keesev1alpha1.WorkspaceSessionPhasePending
	}

	// --- Resolve the AgentRuntime referenced by the parent Workspace ---
	ar, err := resolveAgentRuntime(ctx, r.Client, ws.Spec.RuntimeRef.Name)
	if err != nil {
		log.Info("AgentRuntime resolution failed; requeuing", "err", err.Error())
		r.setSessionProgressing(&sess, "AgentRuntimeNotFound", err.Error())
		return ctrl.Result{RequeueAfter: sessionRequeueBackoff}, r.patchSessionStatus(ctx, &sess, orig)
	}

	// --- TokenBudget gate (TD-P2-14) ---
	// Reads TokenBudget.status (written by the policy/tokenbudget controller) to decide
	// whether pod provisioning is permitted. Rule 04.4: reading a different controller's
	// status is fine — only the TokenBudget controller writes TokenBudget.status.
	budgetExceeded, budgetMsg, budgetErr := r.checkTokenBudget(ctx, &sess, &ws)
	if budgetErr != nil {
		log.Error(budgetErr, "TokenBudget lookup error; requeuing")
		r.setSessionProgressing(&sess, "TokenBudgetLookupFailed", budgetErr.Error())
		return ctrl.Result{RequeueAfter: sessionRequeueBackoff}, r.patchSessionStatus(ctx, &sess, orig)
	}
	if budgetExceeded {
		r.Recorder.Eventf(&sess, corev1.EventTypeWarning, ReasonTokenBudgetExceeded, "%s", budgetMsg)
		setSessionCondition(&sess.Status.Conditions, metav1.Condition{
			Type:               sessionConditionTokenBudgetExceeded,
			Status:             metav1.ConditionTrue,
			Reason:             "BudgetExhausted",
			Message:            budgetMsg,
			ObservedGeneration: sess.Generation,
		})
		setSessionCondition(&sess.Status.Conditions, metav1.Condition{
			Type:               sessionConditionTokenBudgetWithinLimit,
			Status:             metav1.ConditionFalse,
			Reason:             "BudgetExhausted",
			Message:            budgetMsg,
			ObservedGeneration: sess.Generation,
		})
		setSessionCondition(&sess.Status.Conditions, metav1.Condition{
			Type:               sessionConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "TokenBudgetExceeded",
			Message:            budgetMsg,
			ObservedGeneration: sess.Generation,
		})
		// Evict an existing pod if one is running — the budget has been crossed.
		if sess.Status.PodRef != nil && sess.Spec.Mode != keesev1alpha1.SessionModeShared {
			evictPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sess.Status.PodRef.Name,
					Namespace: sess.Namespace,
				},
			}
			if delErr := r.Delete(ctx, evictPod); delErr != nil && !errors.IsNotFound(delErr) {
				log.Error(delErr, "failed to evict session pod on budget exceed", "pod", sess.Status.PodRef.Name)
			} else {
				r.Recorder.Eventf(&sess, corev1.EventTypeWarning, ReasonSessionEvicted,
					"Session pod %s evicted due to TokenBudget exceeded", sess.Status.PodRef.Name)
				sess.Status.Phase = keesev1alpha1.WorkspaceSessionPhaseEvicted
			}
		}
		sess.Status.ObservedGeneration = sess.Generation
		sess.Status.LastReconcileTime = metav1.Now()
		return ctrl.Result{}, r.patchSessionStatus(ctx, &sess, orig)
	}
	// Budget within limit (or no budget — unlimited).
	setSessionCondition(&sess.Status.Conditions, metav1.Condition{
		Type:               sessionConditionTokenBudgetWithinLimit,
		Status:             metav1.ConditionTrue,
		Reason:             "WithinLimit",
		Message:            budgetMsg,
		ObservedGeneration: sess.Generation,
	})
	setSessionCondition(&sess.Status.Conditions, metav1.Condition{
		Type:               sessionConditionTokenBudgetExceeded,
		Status:             metav1.ConditionFalse,
		Reason:             "WithinLimit",
		Message:            budgetMsg,
		ObservedGeneration: sess.Generation,
	})

	// --- Ensure pod based on Mode ---
	podName, result, err := r.ensurePod(ctx, &sess, &ws, ar)
	if err != nil || result.RequeueAfter > 0 || result.Requeue {
		_ = r.patchSessionStatus(ctx, &sess, orig)
		return result, err
	}

	// --- Advance FSM ---
	switch sess.Status.Phase {
	case keesev1alpha1.WorkspaceSessionPhasePending:
		// Move to Attaching once pod provisioned.
		if podName != "" {
			sess.Status.Phase = keesev1alpha1.WorkspaceSessionPhaseAttaching
			r.Recorder.Eventf(&sess, corev1.EventTypeNormal, ReasonSessionAttaching,
				"Session %s is attaching via pod %s", sess.Name, podName)
		}

	case keesev1alpha1.WorkspaceSessionPhaseAttaching:
		// Advance to Active once the pod is Running.
		if podName != "" {
			var pod corev1.Pod
			if err := r.Get(ctx, client.ObjectKey{Name: podName, Namespace: sess.Namespace}, &pod); err == nil {
				if pod.Status.Phase == corev1.PodRunning {
					sess.Status.Phase = keesev1alpha1.WorkspaceSessionPhaseActive
					now := metav1.Now()
					if sess.Status.AttachedAt == nil {
						sess.Status.AttachedAt = &now
					}
					r.Recorder.Eventf(&sess, corev1.EventTypeNormal, ReasonSessionActive,
						"Session %s is active on pod %s", sess.Name, podName)

					// Write ReBAC attached_by tuple.
					if rebacErr := WriteSessionAttachedBy(ctx, r.Rebac,
						string(sess.UID), sess.Spec.AttachSubject); rebacErr != nil {
						log.Error(rebacErr, "failed to write attached_by tuple")
						r.setSessionProgressing(&sess, "RebacSyncFailed", rebacErr.Error())
						return ctrl.Result{RequeueAfter: sessionRequeueBackoff},
							r.patchSessionStatus(ctx, &sess, orig)
					}
					r.Recorder.Eventf(&sess, corev1.EventTypeNormal,
						ReasonSessionAttachedByTupleWritten,
						"ReBAC attached_by tuple written for subject %s", sess.Spec.AttachSubject)
				} else if pod.Status.Phase == corev1.PodSucceeded && isNonInteractiveRecipe(&ws) {
					// Non-interactive recipe ran to completion — success path.
					sess.Status.Phase = keesev1alpha1.WorkspaceSessionPhaseCompleted
					r.Recorder.Eventf(&sess, corev1.EventTypeNormal, ReasonSessionCompleted,
						"Recipe %q completed; pod %s exited 0", ws.Spec.RecipeRef.Name, podName)
				} else if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
					// Pod terminated unexpectedly.
					if sess.Spec.PreserveOnPodFailure {
						r.setSessionProgressing(&sess, "PodFailed",
							fmt.Sprintf("pod %s terminated in phase %s", podName, pod.Status.Phase))
					} else {
						sess.Status.Phase = keesev1alpha1.WorkspaceSessionPhaseEvicted
						r.Recorder.Eventf(&sess, corev1.EventTypeWarning, ReasonSessionEvicted,
							"Pod %s terminated; session evicted", podName)
					}
				} else {
					// Pod still pending — requeue to wait.
					return ctrl.Result{RequeueAfter: sessionRequeueBackoff},
						r.patchSessionStatus(ctx, &sess, orig)
				}
			}
		}

	case keesev1alpha1.WorkspaceSessionPhaseActive:
		// Idle eviction: when attachGraceSeconds > 0 and LastActivityAt has been
		// updated past the grace window, transition to Draining.
		//
		// LastActivityAt source: the ACP bridge sidecar (design 08b §Sidecar)
		// SSA-patches status.lastActivityAt on every ACP frame it forwards.
		// Until that sidecar lands, LastActivityAt stays nil and idle eviction
		// is effectively a no-op — the session lives until explicit delete or
		// pod failure. This is intentional: kuttl tests + the demo today never
		// exercise idle eviction, and a "no bridge → no eviction" failure mode
		// is safer than "no bridge → immediate eviction." The reconciler is
		// already correct; the bridge is the missing piece, not this code.
		if sess.Spec.AttachGraceSeconds > 0 && sess.Status.LastActivityAt != nil {
			grace := time.Duration(sess.Spec.AttachGraceSeconds) * time.Second
			if time.Since(sess.Status.LastActivityAt.Time) > grace {
				sess.Status.Phase = keesev1alpha1.WorkspaceSessionPhaseDraining
				r.Recorder.Eventf(&sess, corev1.EventTypeNormal, ReasonSessionDraining,
					"Session %s idle grace exceeded; draining", sess.Name)
				return ctrl.Result{RequeueAfter: sessionRequeueBackoff},
					r.patchSessionStatus(ctx, &sess, orig)
			}
		}

	case keesev1alpha1.WorkspaceSessionPhaseDraining:
		// Tear down the pod then move to Evicted.
		if podName != "" {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: sess.Namespace},
			}
			if err := r.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
				log.Error(err, "failed to delete session pod", "pod", podName)
				return ctrl.Result{RequeueAfter: sessionRequeueBackoff},
					r.patchSessionStatus(ctx, &sess, orig)
			}
			r.Recorder.Eventf(&sess, corev1.EventTypeNormal, ReasonSessionPodTornDown,
				"Session pod %s torn down", podName)
		}
		sess.Status.Phase = keesev1alpha1.WorkspaceSessionPhaseEvicted
		r.Recorder.Eventf(&sess, corev1.EventTypeNormal, ReasonSessionEvicted,
			"Session %s evicted after drain", sess.Name)
	}

	// --- Update status conditions ---
	sess.Status.ObservedGeneration = sess.Generation
	now := metav1.Now()
	sess.Status.LastReconcileTime = now

	readyStatus := metav1.ConditionFalse
	readyReason := "NotActive"
	readyMsg := fmt.Sprintf("session phase: %s", sess.Status.Phase)
	if sess.Status.Phase == keesev1alpha1.WorkspaceSessionPhaseActive {
		readyStatus = metav1.ConditionTrue
		readyReason = "Active"
		readyMsg = "Session is active"
	}
	setSessionCondition(&sess.Status.Conditions, metav1.Condition{
		Type:               sessionConditionReady,
		Status:             readyStatus,
		Reason:             readyReason,
		Message:            readyMsg,
		ObservedGeneration: sess.Generation,
	})
	setSessionCondition(&sess.Status.Conditions, metav1.Condition{
		Type:               sessionConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "ReconcileComplete",
		Message:            "Reconcile completed successfully",
		ObservedGeneration: sess.Generation,
	})

	return ctrl.Result{}, r.patchSessionStatus(ctx, &sess, orig)
}

// ensurePod provisions (or identifies) the pod backing this session based on Mode.
// Returns the pod name, a requeue Result if needed, and an error.
func (r *WorkspaceSessionReconciler) ensurePod(
	ctx context.Context,
	sess *keesev1alpha1.WorkspaceSession,
	ws *keesev1alpha1.Workspace,
	ar *keesev1alpha1.AgentRuntime,
) (string, ctrl.Result, error) {
	log := logf.FromContext(ctx)

	switch sess.Spec.Mode {
	case keesev1alpha1.SessionModeShared:
		// Shared mode: reuse the Workspace's primary pod (tracked in ws.Status.PodRef).
		// We do not provision a new pod — the Workspace controller owns the pod.
		if ws.Status.PodRef != nil {
			sess.Status.PodRef = &keesev1alpha1.PodRef{
				Name: ws.Status.PodRef.Name,
			}
			return ws.Status.PodRef.Name, ctrl.Result{}, nil
		}
		// Workspace pod not yet available — requeue.
		r.setSessionProgressing(sess, "WorkspacePodNotReady", "waiting for Workspace primary pod")
		return "", ctrl.Result{RequeueAfter: sessionRequeueBackoff}, nil

	case keesev1alpha1.SessionModePerUser:
		// Per-user: one pod per (workspace, subject) pair.
		podName := perUserPodName(ws, sess.Spec.AttachSubject)
		if err := r.applySessionPod(ctx, sess, ws, ar, podName); err != nil {
			log.Error(err, "failed to apply per-user pod", "pod", podName)
			r.setSessionProgressing(sess, "PodProvisionFailed", err.Error())
			return "", ctrl.Result{RequeueAfter: sessionRequeueBackoff}, nil
		}
		sess.Status.PodRef = &keesev1alpha1.PodRef{Name: podName}
		r.Recorder.Eventf(sess, corev1.EventTypeNormal, ReasonSessionPodProvisioned,
			"per-user pod %s provisioned", podName)
		return podName, ctrl.Result{}, nil

	case keesev1alpha1.SessionModePerAttach:
		// Per-attach: one ephemeral pod per session UID.
		podName := perAttachPodName(sess)
		if err := r.applySessionPod(ctx, sess, ws, ar, podName); err != nil {
			log.Error(err, "failed to apply per-attach pod", "pod", podName)
			r.setSessionProgressing(sess, "PodProvisionFailed", err.Error())
			return "", ctrl.Result{RequeueAfter: sessionRequeueBackoff}, nil
		}
		sess.Status.PodRef = &keesev1alpha1.PodRef{Name: podName}
		r.Recorder.Eventf(sess, corev1.EventTypeNormal, ReasonSessionPodProvisioned,
			"per-attach pod %s provisioned", podName)
		return podName, ctrl.Result{}, nil

	default:
		// Unknown mode — should never happen given the enum validation.
		return "", ctrl.Result{}, fmt.Errorf("unknown session mode %q", sess.Spec.Mode)
	}
}

// applySessionPod issues a Server-Side Apply for the session-backing Pod.
func (r *WorkspaceSessionReconciler) applySessionPod(
	ctx context.Context,
	sess *keesev1alpha1.WorkspaceSession,
	ws *keesev1alpha1.Workspace,
	ar *keesev1alpha1.AgentRuntime,
	podName string,
) error {
	pod := buildSessionPodObject(sess, ws, ar, podName)
	return r.Client.Patch(ctx, pod, client.Apply,
		client.FieldOwner(sessionFieldOwner),
		client.ForceOwnership)
}

// cleanupSession runs when DeletionTimestamp is set.
// It tears down the session pod, deletes ReBAC tuples, then removes the finalizer.
func (r *WorkspaceSessionReconciler) cleanupSession(
	ctx context.Context,
	sess *keesev1alpha1.WorkspaceSession,
	orig *keesev1alpha1.WorkspaceSession,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(sess, sessionFinalizer) {
		return ctrl.Result{}, nil
	}

	sess.Status.Phase = keesev1alpha1.WorkspaceSessionPhaseTerminating
	_ = r.patchSessionStatus(ctx, sess, orig)
	// Refresh orig after status patch to avoid stale base for finalizer patch.
	if err := r.Get(ctx, client.ObjectKeyFromObject(sess), sess); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig = sess.DeepCopy()

	// Delete the session pod (if we provisioned one — not shared mode).
	if sess.Status.PodRef != nil && sess.Spec.Mode != keesev1alpha1.SessionModeShared {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sess.Status.PodRef.Name,
				Namespace: sess.Namespace,
			},
		}
		if err := r.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
			log.Error(err, "failed to delete session pod during cleanup")
			return ctrl.Result{RequeueAfter: sessionRequeueBackoff}, nil
		}
		r.Recorder.Eventf(sess, corev1.EventTypeNormal, ReasonSessionPodTornDown,
			"Session pod %s torn down during cleanup", sess.Status.PodRef.Name)
	}

	// Delete ReBAC attached_by tuple.
	if err := DeleteSessionAttachedBy(ctx, r.Rebac,
		string(sess.UID), sess.Spec.AttachSubject); err != nil {
		log.Error(err, "failed to delete attached_by tuple; will retry")
		return ctrl.Result{RequeueAfter: sessionRequeueBackoff}, nil
	}

	// All cleanup done — remove finalizer.
	controllerutil.RemoveFinalizer(sess, sessionFinalizer)
	return ctrl.Result{}, r.Patch(ctx, sess, client.MergeFrom(orig))
}

// patchSessionStatus patches only the status subresource.
func (r *WorkspaceSessionReconciler) patchSessionStatus(
	ctx context.Context,
	sess *keesev1alpha1.WorkspaceSession,
	orig *keesev1alpha1.WorkspaceSession,
) error {
	return r.Status().Patch(ctx, sess, client.MergeFrom(orig))
}

// setSessionProgressing sets the Progressing condition to True.
func (r *WorkspaceSessionReconciler) setSessionProgressing(
	sess *keesev1alpha1.WorkspaceSession,
	reason, msg string,
) {
	setSessionCondition(&sess.Status.Conditions, metav1.Condition{
		Type:               sessionConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: sess.Generation,
	})
	setSessionCondition(&sess.Status.Conditions, metav1.Condition{
		Type:               sessionConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: sess.Generation,
	})
}

// SetupWithManager sets up the controller with the Manager.
//
// Watches:
//   - WorkspaceSession (primary). Predicate fires on generation
//     changes (spec edits) AND any annotation update whose key
//     matches `keese.ai/poke*` so operators can force a reconcile
//     without bumping the spec.
//   - Pod (owned). When the per-user session pod is deleted out
//     from under the controller (kubectl delete pod, node drain,
//     OOM, cluster eviction) the parent WorkspaceSession requeues
//     and recreates the pod. Without this, status drifts to
//     Ready=True with no live pod and the only recovery is delete +
//     reapply (the pain we hit repeatedly during TD-P1-02 verify).
//   - PersistentVolumeClaim (owned). Same rationale for the session
//     PVC (recipeRef updates, manual delete during demo).
func (r *WorkspaceSessionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Rebac == nil {
		r.Rebac = WorkspaceNoopRebacWriter{}
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("workspacesession-controller")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&keesev1alpha1.WorkspaceSession{},
			builder.WithPredicates(predicate.Or(
				predicate.GenerationChangedPredicate{},
				newPokeAnnotationPredicate(),
			))).
		Owns(&corev1.Pod{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Named("workspace-workspacesession").
		Complete(r)
}

// newPokeAnnotationPredicate fires when any annotation whose key
// starts with `keese.ai/poke` changes. Useful for forcing a
// reconcile without bumping spec generation (e.g.,
// `kubectl annotate workspacesession my-session keese.ai/poke=$(date +%s)`).
func newPokeAnnotationPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return false
			}
			return pokeAnnotationDelta(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations())
		},
	}
}

func pokeAnnotationDelta(oldA, newA map[string]string) bool {
	if len(oldA) == 0 && len(newA) == 0 {
		return false
	}
	for k, v := range newA {
		if !strings.HasPrefix(k, "keese.ai/poke") {
			continue
		}
		if oldA[k] != v {
			return true
		}
	}
	for k := range oldA {
		if !strings.HasPrefix(k, "keese.ai/poke") {
			continue
		}
		if _, ok := newA[k]; !ok {
			return true
		}
	}
	return false
}

// checkTokenBudget fetches the TokenBudget scoped to the session's tenant and
// determines whether the budget is exceeded.
//
// Lookup strategy: list all TokenBudgets in the session's namespace and find the
// first one whose spec.scope.tenant.name matches ws.spec.tenantRef.name. A
// workspace-scoped TokenBudget matching ws.Name is also considered (tenant-scoped
// takes precedence if both exist).
//
// Default (no TokenBudget found): UNLIMITED — returns (false, "no TokenBudget for
// tenant <name>; defaulting to unlimited", nil). This is intentional: operators
// who have not configured a budget should not have sessions silently blocked.
// Document this default with a warning log so it is visible in controller logs.
//
// Returns (exceeded bool, humanReadableMessage string, lookupError error).
// lookupError is non-nil only on API errors — budget-exceeded is a normal
// false-positive-safe result, never an error.
func (r *WorkspaceSessionReconciler) checkTokenBudget(
	ctx context.Context,
	sess *keesev1alpha1.WorkspaceSession,
	ws *keesev1alpha1.Workspace,
) (bool, string, error) {
	log := logf.FromContext(ctx)

	tenantName := ws.Spec.TenantRef.Name

	var tbList policyv1alpha1.TokenBudgetList
	if err := r.List(ctx, &tbList, client.InNamespace(sess.Namespace)); err != nil {
		return false, "", fmt.Errorf("listing TokenBudgets: %w", err)
	}

	var matched *policyv1alpha1.TokenBudget
	for i := range tbList.Items {
		tb := &tbList.Items[i]
		if tb.Spec.Scope.Tenant != nil && tb.Spec.Scope.Tenant.Name == tenantName {
			matched = tb
			break // tenant-scoped takes precedence over workspace-scoped
		}
		if matched == nil && tb.Spec.Scope.Workspace != nil && tb.Spec.Scope.Workspace.Name == ws.Name {
			matched = tb
		}
	}

	if matched == nil {
		// No TokenBudget configured for this tenant — default to unlimited.
		// This is the intentional safe default: operators who have not
		// configured a budget should not have sessions blocked silently.
		msg := fmt.Sprintf("no TokenBudget for tenant %q; defaulting to unlimited", tenantName)
		log.Info("TokenBudget not found; applying unlimited default", "tenant", tenantName)
		return false, msg, nil
	}

	// Phase Exhausted or SoftExhausted with ExhaustionMode=hard → gate.
	// SoftExhausted with mode=soft or mode=disabled → warn only (no gate).
	// Disabled mode → never gate regardless of phase.
	switch matched.Spec.ExhaustionMode {
	case policyv1alpha1.ExhaustionModeDisabled:
		return false,
			fmt.Sprintf("TokenBudget %q exhaustion mode is disabled; unlimited", matched.Name),
			nil
	case policyv1alpha1.ExhaustionModeSoft:
		if matched.Status.Phase == policyv1alpha1.TokenBudgetPhaseExhausted ||
			matched.Status.Phase == policyv1alpha1.TokenBudgetPhaseSoftExhausted {
			// Soft mode: log and emit event but do not block.
			log.Info("TokenBudget soft-exhausted; session allowed to proceed",
				"budget", matched.Name, "phase", matched.Status.Phase)
			return false,
				fmt.Sprintf("TokenBudget %q soft-exhausted (phase=%s); session allowed (soft mode)",
					matched.Name, matched.Status.Phase),
				nil
		}
	case policyv1alpha1.ExhaustionModeHard:
		if matched.Status.Phase == policyv1alpha1.TokenBudgetPhaseExhausted {
			return true,
				fmt.Sprintf("TokenBudget %q exhausted (phase=%s, mode=hard); pod provisioning refused",
					matched.Name, matched.Status.Phase),
				nil
		}
	}

	return false,
		fmt.Sprintf("TokenBudget %q within limit (phase=%s)", matched.Name, matched.Status.Phase),
		nil
}

// --- Resource builders ---

// perUserPodName returns a deterministic pod name for the (workspace, subject) pair.
// Format: ws-<workspace-uid-prefix>-sess-<subject-hash-8>
func perUserPodName(ws *keesev1alpha1.Workspace, subject string) string {
	h := sha256.Sum256([]byte(subject))
	return fmt.Sprintf("ws-%s-sess-%x", shortUID(string(ws.UID)), h[:4])
}

// perAttachPodName returns a deterministic pod name for this session UID.
// Format: sess-<session-uid-prefix>
func perAttachPodName(sess *keesev1alpha1.WorkspaceSession) string {
	return fmt.Sprintf("sess-%s", shortUID(string(sess.UID)))
}

// shortUID returns the first 8 characters of a UID string (safe for pod names).
func shortUID(uid string) string {
	if len(uid) > 8 {
		return uid[:8]
	}
	return uid
}

// buildSessionPodObject constructs the Pod SSA object for a session.
//
// Runtime discriminator (E1 T5): when the resolved AgentRuntime selects the
// ADK Python provider (spec.implementation.adkPython != nil), the PodSpec is
// rendered by internal/runtime/providers/adkpython; otherwise the goose path
// runs unchanged. The ObjectMeta (name, namespace, labels, owner ref) is
// provider-agnostic and shared, so SSA ownership + GC behave identically.
//
// The pod (either provider) never carries upstream credentials (rule 05.2);
// the gateway injects them via BSP. Memory is stored under a subPath of the
// session PVC for the demo path; replacing this with a Memory-CR-resolved PVC
// is tracked in TD-P1-09.
func buildSessionPodObject(
	sess *keesev1alpha1.WorkspaceSession,
	ws *keesev1alpha1.Workspace,
	ar *keesev1alpha1.AgentRuntime,
	podName string,
) *corev1.Pod {
	tenantName := ws.Spec.TenantRef.Name
	meta := sessionPodObjectMeta(sess, ws, podName, tenantName)

	// E1 T5 discriminator: the ADK Python provider gets its own single-
	// container pod template (non-secret env, projected egress token, CA
	// bundle, hardened SecurityContext — rule 05). goose (and every other
	// provider) falls through to the unchanged goose path below.
	if ar.Spec.Implementation.AdkPython != nil {
		return &corev1.Pod{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
			ObjectMeta: meta,
			Spec: adkpython.BuildPodSpec(adkpython.PodInputFromCRs(
				ws, ar, serviceAccountName(ws), sessionPVCName(ws),
			)),
		}
	}

	return &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: meta,
		Spec:       buildGooseSessionPodSpec(sess, ws, ar, tenantName),
	}
}

// sessionPodObjectMeta builds the provider-agnostic Pod ObjectMeta (labels +
// owner ref) shared by every runtime's pod template.
func sessionPodObjectMeta(
	sess *keesev1alpha1.WorkspaceSession,
	ws *keesev1alpha1.Workspace,
	podName, tenantName string,
) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      podName,
		Namespace: sess.Namespace,
		Labels: map[string]string{
			"keese.ai/workspace":           ws.Name,
			"keese.ai/session":             sess.Name,
			"keese.ai/session-mode":        string(sess.Spec.Mode),
			"keese.ai/tenant":              tenantName,
			"app.kubernetes.io/managed-by": sessionFieldOwner,
		},
		// Owner reference keeps the pod garbage-collected when the WorkspaceSession is deleted.
		OwnerReferences: []metav1.OwnerReference{
			{
				APIVersion:         keesev1alpha1.GroupVersion.String(),
				Kind:               "WorkspaceSession",
				Name:               sess.Name,
				UID:                sess.UID,
				Controller:         ptr(true),
				BlockOwnerDeletion: ptr(true),
			},
		},
	}
}

// buildGooseSessionPodSpec renders the goose runtime PodSpec — the original,
// unchanged goose single-container template. Split out of buildSessionPodObject
// so the T5 discriminator routes ADK Python to its own renderer without
// touching the goose path.
func buildGooseSessionPodSpec(
	sess *keesev1alpha1.WorkspaceSession,
	ws *keesev1alpha1.Workspace,
	ar *keesev1alpha1.AgentRuntime,
	tenantName string,
) corev1.PodSpec {
	saName := serviceAccountName(ws)
	pvcName := sessionPVCName(ws)

	tgps := int64(60) // align with operator drain budget (design 18)
	saTokenExpiry := int64(saTokenExpirationSeconds)

	return corev1.PodSpec{
		RestartPolicy:                 corev1.RestartPolicyNever,
		ServiceAccountName:            saName,
		AutomountServiceAccountToken:  ptr(false), // SA token comes via projected volume only
		TerminationGracePeriodSeconds: &tgps,
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: ptr(true),
			// Explicit numeric UID — kubelet's runAsNonRoot check
			// rejects non-numeric image users like "goose". 1000 is
			// the conventional non-root demo UID and matches the
			// upstream goose image's named user.
			RunAsUser:  ptr(int64(1000)),
			RunAsGroup: ptr(int64(1000)),
			FSGroup:    ptr(int64(1000)),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Volumes:        sessionPodVolumes(sess, ws, pvcName, tenantName, saTokenExpiry),
		InitContainers: sessionInitContainers(ws, ar),
		Containers: []corev1.Container{
			{
				Name:            "agent",
				Image:           ar.Spec.Implementation.Goose.Image,
				ImagePullPolicy: corev1.PullIfNotPresent,
				// Command + Args branch on interactive vs non-interactive:
				//   - interactive (Workspace.spec.interactive == true): keep
				//     the container alive with `sleep infinity` so a user
				//     can `kubectl exec` and run goose interactively. The
				//     ACP bridge (TD-P1-02 follow-on) will replace this.
				//   - non-interactive (interactive == false && recipeRef != nil):
				//     run `goose run --recipe …` as PID 1 so the pod exits
				//     PodSucceeded on completion and the controller can
				//     mark Phase=Completed.
				Command:      sessionAgentCommand(ws),
				Args:         sessionAgentArgs(ws),
				Env:          sessionPodEnv(sess, ws, tenantName),
				VolumeMounts: sessionPodVolumeMounts(ws),
				SecurityContext: &corev1.SecurityContext{
					RunAsNonRoot:             ptr(true),
					ReadOnlyRootFilesystem:   ptr(true),
					AllowPrivilegeEscalation: ptr(false),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
				// Drain hook: write the SQLite checkpoint and JSON marker file to
				// the session PVC before kubelet deletes the pod (TD-P1-02).
				// /usr/local/bin/keese-drain is the runtime-drain sidecar
				// entrypoint bundled into the goose runtime image.
				// Mirrors AgentRuntime.Drain: checkpoints WAL, writes
				// /var/run/keese/session/sessions/<uid>/draining atomically.
				// Budget: 25 s (terminationGracePeriodSeconds 60 − 30 s for
				// goose SIGTERM drain − 5 s kubelet buffer).
				Lifecycle: &corev1.Lifecycle{
					PreStop: &corev1.LifecycleHandler{
						Exec: &corev1.ExecAction{
							Command: []string{
								"/usr/local/bin/keese-drain",
								"--pvc-root=/var/run/keese/session",
								"--timeout=25s",
							},
						},
					},
				},
			},
		},
	}
}

// ptrQuantity is a small helper to construct a *resource.Quantity from a string.
func ptrQuantity(s string) *resource.Quantity {
	q := resource.MustParse(s)
	return &q
}

// recipeMountPath is where the workspace's recipe ConfigMap is mounted in
// the session pod. Matches sessionPodVolumeMounts; KEESE_RECIPE_PATH env
// var (set in sessionPodEnv) points at the same file.
const recipeMountPath = "/var/run/keese/recipes/recipe.yaml"

// sessionAgentCommand returns the container Command for the agent.
//
// Branching:
//   - non-interactive AND recipeRef set → ["/usr/local/bin/goose"]
//     (Args adds `run --recipe <path>`). Pod exits with PodSucceeded.
//   - otherwise → ["/bin/sh", "-c"] with sleep-infinity Args, so users can
//     `kubectl exec` and run goose interactively.
func sessionAgentCommand(ws *keesev1alpha1.Workspace) []string {
	if isNonInteractiveRecipe(ws) {
		return []string{"/usr/local/bin/goose"}
	}
	return []string{"/bin/sh", "-c"}
}

// sessionAgentArgs returns the matching Args slice for sessionAgentCommand.
func sessionAgentArgs(ws *keesev1alpha1.Workspace) []string {
	if isNonInteractiveRecipe(ws) {
		return []string{"run", "--recipe", recipeMountPath}
	}
	return []string{"echo 'goose runtime ready; attach via kubectl exec'; exec sleep infinity"}
}

// isNonInteractiveRecipe reports whether the workspace is the non-
// interactive batch path (recipeRef set AND interactive=false).
func isNonInteractiveRecipe(ws *keesev1alpha1.Workspace) bool {
	if ws == nil || ws.Spec.RecipeRef == nil || ws.Spec.RecipeRef.Name == "" {
		return false
	}
	return !ws.Spec.Interactive
}

// gatewayCAConfigMapName is the in-namespace ConfigMap that mirrors
// the AI Gateway's serving CA cert. The dev/bootstrap/aigateway/
// install (or a per-tenant infra-bootstrap step) is responsible for
// keeping the mirror up to date. Without this volume the session
// pod's TLS to the gateway fails with curl exit 77 (no trust root).
const gatewayCAConfigMapName = "keese-aigateway-ca"

// sessionPodVolumes builds the per-session pod's volume slice.
//
// Always-on: session PVC, projected SA token (egress), scratch /tmp, gateway-CA ConfigMap.
//
// Projected SA token sources (design 04b iter-3):
//   - egress (always): keese-egress-<tenant>       → Envoy AI Gateway
//   - workflowRun (conditional): keese-wf-<uid>    → NATS bridge; only when
//     the WorkspaceSession carries annotation keese.ai/workflowrun-uid.
//   - supervisor (conditional): keese-supervisor-<ws-uid> → ACP supervisor bridge
//     (design 23); only when the parent Workspace carries annotation
//     keese.ai/supervisor-uid (set by the operator when supervisorRef is configured).
//
// Conditional volumes: recipe ConfigMap (only when Workspace.spec.recipeRef is set).
func sessionPodVolumes(
	sess *keesev1alpha1.WorkspaceSession,
	ws *keesev1alpha1.Workspace,
	pvcName, tenantName string,
	saTokenExpiry int64,
) []corev1.Volume {
	// Build the projected SA token sources. Egress is always present.
	tokenSources := []corev1.VolumeProjection{
		{
			ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
				Audience:          fmt.Sprintf("keese-egress-%s", tenantName),
				ExpirationSeconds: &saTokenExpiry,
				Path:              "egress",
			},
		},
	}

	// workflowRun projection: conditional on annotation keese.ai/workflowrun-uid
	// set by the Workflow controller when this session is inside a WorkflowRun.
	if sess != nil {
		if wfUID := sess.Annotations[AnnotationWorkflowRunUID]; wfUID != "" {
			tokenSources = append(tokenSources, corev1.VolumeProjection{
				ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
					Audience:          fmt.Sprintf("keese-wf-%s", wfUID),
					ExpirationSeconds: &saTokenExpiry,
					Path:              "workflowRun",
				},
			})
		}
	}

	// supervisor projection: conditional on annotation keese.ai/supervisor-uid
	// set on the Workspace when Workspace.spec.supervisorRef is configured.
	// The annotation value is the workspace UID used to scope the audience
	// (design 04b §supervisor; API field deferred to next API bump).
	if ws != nil {
		if supUID := ws.Annotations[AnnotationSupervisorUID]; supUID != "" {
			tokenSources = append(tokenSources, corev1.VolumeProjection{
				ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
					Audience:          fmt.Sprintf("keese-supervisor-%s", supUID),
					ExpirationSeconds: &saTokenExpiry,
					Path:              "supervisor",
				},
			})
		}
	}

	vols := []corev1.Volume{
		{
			Name: "session",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		},
		{
			Name: "sa-token",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: tokenSources,
				},
			},
		},
		{
			// Writable scratch — readOnlyRootFilesystem requires this for /tmp.
			Name: "scratch",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					SizeLimit: ptrQuantity("256Mi"),
				},
			},
		},
		{
			// Gateway CA bundle mirrored from keese-system into the
			// workspace namespace so the agent's curl/Go-TLS stack
			// trusts the gateway's serving cert without --insecure.
			// `optional: true` so a missing CM doesn't block pod
			// creation — agent will fall back to no-mount and emit
			// a clear TLS error in logs.
			Name: "gateway-ca",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: gatewayCAConfigMapName,
					},
					Optional: ptr(true),
				},
			},
		},
	}
	if ws != nil && ws.Spec.RecipeRef != nil && ws.Spec.RecipeRef.Name != "" {
		vols = append(vols, corev1.Volume{
			Name: "recipe",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: ws.Spec.RecipeRef.Name,
					},
				},
			},
		})
	}
	return vols
}

// sessionPodEnv builds the goose container's env slice. Always-on:
// GOOSE_PROVIDER/MODEL, ANTHROPIC_BASE_URL/HOST (path-prefixed with
// /anthropic so goose's hardcoded /v1/messages lands at the AI
// Gateway's extProc Anthropic-schema entry), SSL_CERT_FILE pointing
// at the mirrored gateway CA, KEESE_SESSION_ID/TENANT/WORKSPACE,
// HOME on the writable session PVC subdir. Conditional:
// KEESE_RECIPE_PATH when Workspace.spec.recipeRef is set.
// sessionPodEnv builds the env vars for the goose runtime container.
// The first arg (sess) is needed so the function can read TD-P2-15
// annotations to conditionally publish token-path env vars.
func sessionPodEnv(sess *keesev1alpha1.WorkspaceSession, ws *keesev1alpha1.Workspace, tenantName string) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "GOOSE_PROVIDER", Value: "anthropic"},
		{Name: "GOOSE_MODEL", Value: "claude-opus-4-7"},
		{Name: "ANTHROPIC_BASE_URL", Value: "https://envoy-ai-gateway.keese-system.svc:443/anthropic"},
		{Name: "ANTHROPIC_HOST", Value: "https://envoy-ai-gateway.keese-system.svc:443/anthropic"},
		{Name: "SSL_CERT_FILE", Value: "/var/run/keese/ca/ca.crt"},
		{
			Name: "KEESE_SESSION_ID",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
		{Name: "KEESE_TENANT", Value: tenantName},
		{Name: "KEESE_WORKSPACE", Value: ws.Name},
		// HOME on the writable session PVC subdir so goose can
		// write ~/.local/state/goose/... under readOnlyRootFilesystem.
		{Name: "HOME", Value: "/var/run/keese/session/home"},
		// Egress audience is unconditional (rule 05.3); the file is always projected.
		{Name: "KEESE_EGRESS_TOKEN_PATH", Value: tokenPathEgress},
	}
	if ws.Spec.RecipeRef != nil && ws.Spec.RecipeRef.Name != "" {
		env = append(env, corev1.EnvVar{
			Name:  "KEESE_RECIPE_PATH",
			Value: "/var/run/keese/recipes/recipe.yaml",
		})
	}
	// TD-P2-15: token-path env vars are conditional on the projected-volume
	// shape — only set when the corresponding token source is actually
	// projected (matches the workflowRun + supervisor projection logic in
	// sessionPodVolumes).
	if sess != nil && sess.Annotations[AnnotationWorkflowRunUID] != "" {
		env = append(env, corev1.EnvVar{
			Name:  "KEESE_WORKFLOWRUN_TOKEN_PATH",
			Value: tokenPathWorkflowRun,
		})
	}
	if ws.Annotations[AnnotationSupervisorUID] != "" {
		env = append(env, corev1.EnvVar{
			Name:  "KEESE_SUPERVISOR_TOKEN_PATH",
			Value: tokenPathSupervisor,
		})
	}
	return env
}

// sessionPodVolumeMounts pairs with sessionPodVolumes — same gating
// on the optional recipe mount.
func sessionPodVolumeMounts(ws *keesev1alpha1.Workspace) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{Name: "session", MountPath: "/var/run/keese/session"},
		{Name: "session", MountPath: "/var/run/keese/memory", SubPath: "memory"},
		{Name: "sa-token", MountPath: "/var/run/keese/tokens", ReadOnly: true},
		{Name: "scratch", MountPath: "/tmp"},
		{Name: "gateway-ca", MountPath: "/var/run/keese/ca", ReadOnly: true},
	}
	if ws != nil && ws.Spec.RecipeRef != nil && ws.Spec.RecipeRef.Name != "" {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "recipe",
			MountPath: "/var/run/keese/recipes",
			ReadOnly:  true,
		})
	}
	return mounts
}

// sessionInitContainers builds the initContainer slice for the session pod.
//
// keese-resume is the only init container today: it copies any prior SQLite
// checkpoint from /var/run/keese/session/keese-checkpoints/<uid>/sessions.db*
// back into goose's expected sessions dir before the agent container starts.
// This implements demo green criterion 6 (resume across pod replacement)
// without operator-side wiring; the operation is idempotent and a no-op
// when no checkpoint exists.
func sessionInitContainers(ws *keesev1alpha1.Workspace, ar *keesev1alpha1.AgentRuntime) []corev1.Container {
	resumeScript := `set -e
CKPT_ROOT=/var/run/keese/session/keese-checkpoints
SDIR=/var/run/keese/session/home/.local/share/goose/sessions
mkdir -p "$SDIR"
if [ ! -d "$CKPT_ROOT" ]; then
  echo "keese-resume: no checkpoints dir; fresh session"
  exit 0
fi
latest=$(ls -t "$CKPT_ROOT"/*/sessions.db 2>/dev/null | head -n1 || true)
if [ -z "$latest" ]; then
  echo "keese-resume: no prior checkpoint; fresh session"
  exit 0
fi
echo "keese-resume: restoring from $(dirname $latest)"
cp -f "$(dirname $latest)/"sessions.db* "$SDIR/" 2>/dev/null || true
ls -la "$SDIR/" >&2 || true
`
	return []corev1.Container{
		{
			Name:            "keese-resume",
			Image:           ar.Spec.Implementation.Goose.Image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-c"},
			Args:            []string{resumeScript},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "session", MountPath: "/var/run/keese/session"},
			},
			SecurityContext: &corev1.SecurityContext{
				RunAsNonRoot:             ptr(true),
				ReadOnlyRootFilesystem:   ptr(true),
				AllowPrivilegeEscalation: ptr(false),
				Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10m"),
					corev1.ResourceMemory: resource.MustParse("32Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
			},
		},
	}
}

// ptr returns a pointer to the given value (generic helper).
func ptr[T any](v T) *T { return &v }

// setSessionCondition upserts a condition into a WorkspaceSession condition slice.
// Reuses the shared helper pattern from workspace_controller.go but scoped to avoid
// a naming collision with setCondition (which takes *[]metav1.Condition — same signature,
// actually usable here directly).
func setSessionCondition(conditions *[]metav1.Condition, c metav1.Condition) {
	setCondition(conditions, c)
}
