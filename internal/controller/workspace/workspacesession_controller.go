// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package workspace

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	runtimev1alpha1 "github.com/keese-ai/keese/api/runtime/v1alpha1"
	workspacev1alpha1 "github.com/keese-ai/keese/api/workspace/v1alpha1"
)

const (
	sessionFinalizer      = "finalizers.workspacesession.operator.keese.ai/cleanup"
	sessionFieldOwner     = "keese-workspacesession-controller"
	sessionRequeueBackoff = 5 * time.Second

	// sessionConditionReady is the Ready condition type for WorkspaceSession.
	sessionConditionReady = "Ready"
	// sessionConditionProgressing is the Progressing condition type for WorkspaceSession.
	sessionConditionProgressing = "Progressing"
	// sessionConditionAttached is the Attached condition type for WorkspaceSession.
	sessionConditionAttached = "Attached"
)

// WorkspaceSessionReconciler reconciles a WorkspaceSession object.
// SSA fieldOwner: keese-workspacesession-controller (rule 04.7)
//
// Finalizers managed:
//   - finalizers.workspacesession.operator.keese.ai/cleanup
//     Steps: Draining → delete Pod → remove session-scoped OpenFGA tuples → remove finalizer.
type WorkspaceSessionReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Rebac    RebacWriter
}

// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspacesessions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspacesessions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspacesessions/finalizers,verbs=update
// +kubebuilder:rbac:groups=workspace.operator.keese.ai,resources=workspaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile is the main reconciliation loop for WorkspaceSession.
// Idiom: fetch → DeepCopy for status patch → handle deletion → ensure desired state → update status.
func (r *WorkspaceSessionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var sess workspacev1alpha1.WorkspaceSession
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
	var ws workspacev1alpha1.Workspace
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
		sess.Status.Phase = workspacev1alpha1.WorkspaceSessionPhasePending
	}

	// --- Resolve the AgentRuntime referenced by the parent Workspace ---
	ar, err := resolveAgentRuntime(ctx, r.Client, ws.Spec.RuntimeRef.Name)
	if err != nil {
		log.Info("AgentRuntime resolution failed; requeuing", "err", err.Error())
		r.setSessionProgressing(&sess, "AgentRuntimeNotFound", err.Error())
		return ctrl.Result{RequeueAfter: sessionRequeueBackoff}, r.patchSessionStatus(ctx, &sess, orig)
	}

	// --- Ensure pod based on Mode ---
	podName, result, err := r.ensurePod(ctx, &sess, &ws, ar)
	if err != nil || result.RequeueAfter > 0 || result.Requeue {
		_ = r.patchSessionStatus(ctx, &sess, orig)
		return result, err
	}

	// --- Advance FSM ---
	switch sess.Status.Phase {
	case workspacev1alpha1.WorkspaceSessionPhasePending:
		// Move to Attaching once pod provisioned.
		if podName != "" {
			sess.Status.Phase = workspacev1alpha1.WorkspaceSessionPhaseAttaching
			r.Recorder.Eventf(&sess, corev1.EventTypeNormal, ReasonSessionAttaching,
				"Session %s is attaching via pod %s", sess.Name, podName)
		}

	case workspacev1alpha1.WorkspaceSessionPhaseAttaching:
		// Advance to Active once the pod is Running.
		if podName != "" {
			var pod corev1.Pod
			if err := r.Get(ctx, client.ObjectKey{Name: podName, Namespace: sess.Namespace}, &pod); err == nil {
				if pod.Status.Phase == corev1.PodRunning {
					sess.Status.Phase = workspacev1alpha1.WorkspaceSessionPhaseActive
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
				} else if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
					// Pod terminated unexpectedly.
					if sess.Spec.PreserveOnPodFailure {
						r.setSessionProgressing(&sess, "PodFailed",
							fmt.Sprintf("pod %s terminated in phase %s", podName, pod.Status.Phase))
					} else {
						sess.Status.Phase = workspacev1alpha1.WorkspaceSessionPhaseEvicted
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

	case workspacev1alpha1.WorkspaceSessionPhaseActive:
		// Check idle eviction: if attachGraceSeconds > 0, evict after idle period.
		// The controller uses LastActivityAt as the idle clock; when the ACP bridge
		// updates that field, the controller resets the eviction timer.
		// TODO(spec-followup): ACP bridge writes LastActivityAt via server-side apply;
		// for now we evict only when attachGraceSeconds > 0 and LastActivityAt is set.
		if sess.Spec.AttachGraceSeconds > 0 && sess.Status.LastActivityAt != nil {
			grace := time.Duration(sess.Spec.AttachGraceSeconds) * time.Second
			if time.Since(sess.Status.LastActivityAt.Time) > grace {
				sess.Status.Phase = workspacev1alpha1.WorkspaceSessionPhaseDraining
				r.Recorder.Eventf(&sess, corev1.EventTypeNormal, ReasonSessionDraining,
					"Session %s idle grace exceeded; draining", sess.Name)
				return ctrl.Result{RequeueAfter: sessionRequeueBackoff},
					r.patchSessionStatus(ctx, &sess, orig)
			}
		}

	case workspacev1alpha1.WorkspaceSessionPhaseDraining:
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
		sess.Status.Phase = workspacev1alpha1.WorkspaceSessionPhaseEvicted
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
	if sess.Status.Phase == workspacev1alpha1.WorkspaceSessionPhaseActive {
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
	sess *workspacev1alpha1.WorkspaceSession,
	ws *workspacev1alpha1.Workspace,
	ar *runtimev1alpha1.AgentRuntime,
) (string, ctrl.Result, error) {
	log := logf.FromContext(ctx)

	switch sess.Spec.Mode {
	case workspacev1alpha1.SessionModeShared:
		// Shared mode: reuse the Workspace's primary pod (tracked in ws.Status.PodRef).
		// We do not provision a new pod — the Workspace controller owns the pod.
		if ws.Status.PodRef != nil {
			sess.Status.PodRef = &workspacev1alpha1.PodRef{
				Name: ws.Status.PodRef.Name,
			}
			return ws.Status.PodRef.Name, ctrl.Result{}, nil
		}
		// Workspace pod not yet available — requeue.
		r.setSessionProgressing(sess, "WorkspacePodNotReady", "waiting for Workspace primary pod")
		return "", ctrl.Result{RequeueAfter: sessionRequeueBackoff}, nil

	case workspacev1alpha1.SessionModePerUser:
		// Per-user: one pod per (workspace, subject) pair.
		podName := perUserPodName(ws, sess.Spec.AttachSubject)
		if err := r.applySessionPod(ctx, sess, ws, ar, podName); err != nil {
			log.Error(err, "failed to apply per-user pod", "pod", podName)
			r.setSessionProgressing(sess, "PodProvisionFailed", err.Error())
			return "", ctrl.Result{RequeueAfter: sessionRequeueBackoff}, nil
		}
		sess.Status.PodRef = &workspacev1alpha1.PodRef{Name: podName}
		r.Recorder.Eventf(sess, corev1.EventTypeNormal, ReasonSessionPodProvisioned,
			"per-user pod %s provisioned", podName)
		return podName, ctrl.Result{}, nil

	case workspacev1alpha1.SessionModePerAttach:
		// Per-attach: one ephemeral pod per session UID.
		podName := perAttachPodName(sess)
		if err := r.applySessionPod(ctx, sess, ws, ar, podName); err != nil {
			log.Error(err, "failed to apply per-attach pod", "pod", podName)
			r.setSessionProgressing(sess, "PodProvisionFailed", err.Error())
			return "", ctrl.Result{RequeueAfter: sessionRequeueBackoff}, nil
		}
		sess.Status.PodRef = &workspacev1alpha1.PodRef{Name: podName}
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
	sess *workspacev1alpha1.WorkspaceSession,
	ws *workspacev1alpha1.Workspace,
	ar *runtimev1alpha1.AgentRuntime,
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
	sess *workspacev1alpha1.WorkspaceSession,
	orig *workspacev1alpha1.WorkspaceSession,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(sess, sessionFinalizer) {
		return ctrl.Result{}, nil
	}

	sess.Status.Phase = workspacev1alpha1.WorkspaceSessionPhaseTerminating
	_ = r.patchSessionStatus(ctx, sess, orig)
	// Refresh orig after status patch to avoid stale base for finalizer patch.
	if err := r.Get(ctx, client.ObjectKeyFromObject(sess), sess); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig = sess.DeepCopy()

	// Delete the session pod (if we provisioned one — not shared mode).
	if sess.Status.PodRef != nil && sess.Spec.Mode != workspacev1alpha1.SessionModeShared {
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
	sess *workspacev1alpha1.WorkspaceSession,
	orig *workspacev1alpha1.WorkspaceSession,
) error {
	return r.Status().Patch(ctx, sess, client.MergeFrom(orig))
}

// setSessionProgressing sets the Progressing condition to True.
func (r *WorkspaceSessionReconciler) setSessionProgressing(
	sess *workspacev1alpha1.WorkspaceSession,
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
func (r *WorkspaceSessionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Rebac == nil {
		r.Rebac = &FakeRebacWriter{}
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("workspacesession-controller")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&workspacev1alpha1.WorkspaceSession{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("workspace-workspacesession").
		Complete(r)
}

// --- Resource builders ---

// perUserPodName returns a deterministic pod name for the (workspace, subject) pair.
// Format: ws-<workspace-uid-prefix>-sess-<subject-hash-8>
func perUserPodName(ws *workspacev1alpha1.Workspace, subject string) string {
	h := sha256.Sum256([]byte(subject))
	return fmt.Sprintf("ws-%s-sess-%x", shortUID(string(ws.UID)), h[:4])
}

// perAttachPodName returns a deterministic pod name for this session UID.
// Format: sess-<session-uid-prefix>
func perAttachPodName(sess *workspacev1alpha1.WorkspaceSession) string {
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
// The pod runs the goose runtime image referenced by the AgentRuntime, with
// projected SA token + session/memory PVC mounts. Upstream credentials never
// land on this pod (rule 05.2); the gateway injects them via BSP. Memory is
// stored under a subPath of the session PVC for the demo path; replacing this
// with a Memory-CR-resolved PVC is tracked in TD-P1-09.
func buildSessionPodObject(
	sess *workspacev1alpha1.WorkspaceSession,
	ws *workspacev1alpha1.Workspace,
	ar *runtimev1alpha1.AgentRuntime,
	podName string,
) *corev1.Pod {
	saName := serviceAccountName(ws)
	pvcName := sessionPVCName(ws)
	tenantName := ws.Spec.TenantRef.Name

	tgps := int64(60) // align with operator drain budget (design 18)
	saTokenExpiry := int64(saTokenExpirationSeconds)

	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
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
					APIVersion:         workspacev1alpha1.GroupVersion.String(),
					Kind:               "WorkspaceSession",
					Name:               sess.Name,
					UID:                sess.UID,
					Controller:         ptr(true),
					BlockOwnerDeletion: ptr(true),
				},
			},
		},
		Spec: corev1.PodSpec{
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
			Volumes: []corev1.Volume{
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
							Sources: []corev1.VolumeProjection{
								{
									ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
										Audience:          fmt.Sprintf("keese-egress-%s", tenantName),
										ExpirationSeconds: &saTokenExpiry,
										Path:              "egress",
									},
								},
							},
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
			},
			Containers: []corev1.Container{
				{
					Name:            "agent",
					Image:           ar.Spec.Implementation.Goose.Image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					// Long-lived ready-to-attach container. We keep `sleep
					// infinity` (not `goose session`) because goose's
					// session command requires a TTY/stdin — it exits
					// immediately under a non-interactive container start.
					// The user / future ACP bridge attaches via
					// `kubectl exec` and runs goose interactively. With
					// the AI Gateway live, the `goose run` invocation
					// inside the exec works against
					// $ANTHROPIC_BASE_URL. (TD-P1-02 ACP bridge replaces
					// this with a real attach handshake.)
					Command: []string{"/bin/sh", "-c"},
					Args:    []string{"echo 'goose runtime ready; attach via kubectl exec'; exec sleep infinity"},
					Env: []corev1.EnvVar{
						{Name: "GOOSE_PROVIDER", Value: "anthropic"},
						{Name: "GOOSE_MODEL", Value: "claude-opus-4-7"},
						{
							Name:  "ANTHROPIC_BASE_URL",
							Value: "https://envoy-ai-gateway.keese-system.svc:443",
						},
						{
							Name: "KEESE_SESSION_ID",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
							},
						},
						{Name: "KEESE_TENANT", Value: tenantName},
						{Name: "KEESE_WORKSPACE", Value: ws.Name},
						// HOME points to a writable subPath on the session PVC
						// so goose can write ~/.local/state/goose/... without
						// needing to disable readOnlyRootFilesystem.
						{Name: "HOME", Value: "/var/run/keese/session/home"},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "session",
							MountPath: "/var/run/keese/session",
						},
						{
							Name:      "session",
							MountPath: "/var/run/keese/memory",
							SubPath:   "memory",
						},
						{
							Name:      "sa-token",
							MountPath: "/var/run/keese/tokens",
							ReadOnly:  true,
						},
						{
							Name:      "scratch",
							MountPath: "/tmp",
						},
					},
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
					// Best-effort drain on SIGTERM. The full Drain/Resume SPI
					// (TD-P1-02) replaces this with a real checkpoint call.
					Lifecycle: &corev1.Lifecycle{
						PreStop: &corev1.LifecycleHandler{
							Exec: &corev1.ExecAction{
								Command: []string{"/bin/sh", "-c", "kill -TERM 1 && sleep 25"},
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

// ptr returns a pointer to the given value (generic helper).
func ptr[T any](v T) *T { return &v }

// setSessionCondition upserts a condition into a WorkspaceSession condition slice.
// Reuses the shared helper pattern from workspace_controller.go but scoped to avoid
// a naming collision with setCondition (which takes *[]metav1.Condition — same signature,
// actually usable here directly).
func setSessionCondition(conditions *[]metav1.Condition, c metav1.Condition) {
	setCondition(conditions, c)
}
