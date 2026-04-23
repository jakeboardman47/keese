// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package workspace

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	workspacev1alpha1 "github.com/keese-ai/keese/api/workspace/v1alpha1"
)

// makeInteractiveWorkspace returns a Workspace with spec.interactive=true.
func makeInteractiveWorkspace(ns, name string) *workspacev1alpha1.Workspace {
	ws := makeWorkspace(ns, name)
	ws.Spec.Interactive = true
	return ws
}

// makeSession returns a minimal WorkspaceSession for testing.
func makeSession(ns, name, wsName, subject, sessionName string, mode workspacev1alpha1.SessionMode) *workspacev1alpha1.WorkspaceSession {
	return &workspacev1alpha1.WorkspaceSession{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: workspacev1alpha1.WorkspaceSessionSpec{
			WorkspaceRef:  wsName,
			AttachSubject: subject,
			SessionName:   sessionName,
			Mode:          mode,
		},
	}
}

// reconcileN drives n reconcile passes for the given session via the given reconciler.
func reconcileN(ctx context.Context, r *WorkspaceSessionReconciler, nsn types.NamespacedName, n int) {
	for i := 0; i < n; i++ {
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
		Expect(err).NotTo(HaveOccurred())
	}
}

var _ = Describe("WorkspaceSessionReconciler", func() {

	// --- Spec 1: Idempotency ---
	Describe("Idempotency", func() {
		It("converges in ≤3 reconciles with no spec change", func() {
			seed := GinkgoRandomSeed()
			wsName := fmt.Sprintf("ws-sess-idemp-%d", seed)
			ws := makeInteractiveWorkspace("default", wsName)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			sess := makeSession("default",
				fmt.Sprintf("sess-idemp-%d", seed),
				wsName, "user:alice@example.com", "default",
				workspacev1alpha1.SessionModeShared,
			)
			Expect(k8sClient.Create(ctx, sess)).To(Succeed())
			nsn := types.NamespacedName{Namespace: sess.Namespace, Name: sess.Name}

			r := &WorkspaceSessionReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}

			var lastVersion string
			for i := 0; i < 3; i++ {
				_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
				Expect(err).NotTo(HaveOccurred())
				var fresh workspacev1alpha1.WorkspaceSession
				Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
				if i == 2 {
					Expect(fresh.ResourceVersion).To(Equal(lastVersion),
						"no spec change → ResourceVersion stable on pass 3")
				}
				lastVersion = fresh.ResourceVersion
			}

			// Cleanup.
			for _, obj := range []ctrlclient.Object{sess, ws} {
				obj.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, obj)
				_ = k8sClient.Delete(ctx, obj)
			}
		})
	})

	// --- Spec 2: Reject non-interactive parent Workspace ---
	Describe("Non-interactive Workspace rejection", func() {
		It("sets Ready=False with NonInteractiveWorkspace reason when Workspace.spec.interactive=false", func() {
			seed := GinkgoRandomSeed()
			wsName := fmt.Sprintf("ws-nointeract-%d", seed)
			// Non-interactive workspace (default: false).
			ws := makeWorkspace("default", wsName)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			sess := makeSession("default",
				fmt.Sprintf("sess-nointeract-%d", seed),
				wsName, "user:alice@example.com", "default",
				workspacev1alpha1.SessionModeShared,
			)
			Expect(k8sClient.Create(ctx, sess)).To(Succeed())
			nsn := types.NamespacedName{Namespace: sess.Namespace, Name: sess.Name}

			r := &WorkspaceSessionReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}

			// Two passes: first adds finalizer, second hits the non-interactive check.
			reconcileN(ctx, r, nsn, 2)

			var fresh workspacev1alpha1.WorkspaceSession
			Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())

			var readyCond *metav1.Condition
			for i := range fresh.Status.Conditions {
				if fresh.Status.Conditions[i].Type == sessionConditionReady {
					readyCond = &fresh.Status.Conditions[i]
				}
			}
			Expect(readyCond).NotTo(BeNil(), "Ready condition must be set")
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal("NonInteractiveWorkspace"))

			// Cleanup.
			for _, obj := range []ctrlclient.Object{sess, ws} {
				obj.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, obj)
				_ = k8sClient.Delete(ctx, obj)
			}
		})
	})

	// --- Spec 3: Mode shared — reuses parent pod ---
	Describe("Mode shared: reuses Workspace primary pod", func() {
		It("does not create a new pod; copies podRef from Workspace status", func() {
			seed := GinkgoRandomSeed()
			wsName := fmt.Sprintf("ws-shared-%d", seed)
			ws := makeInteractiveWorkspace("default", wsName)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			// Simulate a primary pod already recorded by the Workspace controller.
			var freshWS workspacev1alpha1.Workspace
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: wsName}, &freshWS)).To(Succeed())
			origWS := freshWS.DeepCopy()
			freshWS.Status.PodRef = &corev1.LocalObjectReference{Name: "primary-pod-stub"}
			Expect(k8sClient.Status().Patch(ctx, &freshWS, ctrlclient.MergeFrom(origWS))).To(Succeed())

			sess := makeSession("default",
				fmt.Sprintf("sess-shared-%d", seed),
				wsName, "user:alice@example.com", "default",
				workspacev1alpha1.SessionModeShared,
			)
			Expect(k8sClient.Create(ctx, sess)).To(Succeed())
			nsn := types.NamespacedName{Namespace: sess.Namespace, Name: sess.Name}

			r := &WorkspaceSessionReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}

			reconcileN(ctx, r, nsn, 2)

			var freshSess workspacev1alpha1.WorkspaceSession
			Expect(k8sClient.Get(ctx, nsn, &freshSess)).To(Succeed())
			Expect(freshSess.Status.PodRef).NotTo(BeNil(), "session must reference a pod")
			Expect(freshSess.Status.PodRef.Name).To(Equal("primary-pod-stub"))

			// Verify no extra pod was created by this controller.
			var podList corev1.PodList
			Expect(k8sClient.List(ctx, &podList,
				ctrlclient.InNamespace("default"),
				ctrlclient.MatchingLabels{"keese.ai/session": sess.Name},
			)).To(Succeed())
			Expect(podList.Items).To(BeEmpty(), "shared mode must not create a new pod")

			// Cleanup.
			for _, obj := range []ctrlclient.Object{sess, ws} {
				obj.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, obj)
				_ = k8sClient.Delete(ctx, obj)
			}
		})
	})

	// --- Spec 4: Mode per-user — creates pod keyed by subject ---
	Describe("Mode per-user: pod keyed by subject", func() {
		It("creates a named pod; second session for same subject shares the same pod name", func() {
			seed := GinkgoRandomSeed()
			wsName := fmt.Sprintf("ws-peruser-%d", seed)
			ws := makeInteractiveWorkspace("default", wsName)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			subject := "user:bob@example.com"
			sess1 := makeSession("default",
				fmt.Sprintf("sess-pu-1-%d", seed), wsName, subject, "work",
				workspacev1alpha1.SessionModePerUser,
			)
			Expect(k8sClient.Create(ctx, sess1)).To(Succeed())
			nsn1 := types.NamespacedName{Namespace: sess1.Namespace, Name: sess1.Name}

			r := &WorkspaceSessionReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}

			reconcileN(ctx, r, nsn1, 2)

			var fresh1 workspacev1alpha1.WorkspaceSession
			Expect(k8sClient.Get(ctx, nsn1, &fresh1)).To(Succeed())
			Expect(fresh1.Status.PodRef).NotTo(BeNil())
			pod1Name := fresh1.Status.PodRef.Name

			// Second session for same subject should yield identical pod name.
			sess2 := makeSession("default",
				fmt.Sprintf("sess-pu-2-%d", seed), wsName, subject, "review",
				workspacev1alpha1.SessionModePerUser,
			)
			Expect(k8sClient.Create(ctx, sess2)).To(Succeed())
			nsn2 := types.NamespacedName{Namespace: sess2.Namespace, Name: sess2.Name}
			reconcileN(ctx, r, nsn2, 2)

			var fresh2 workspacev1alpha1.WorkspaceSession
			Expect(k8sClient.Get(ctx, nsn2, &fresh2)).To(Succeed())
			Expect(fresh2.Status.PodRef).NotTo(BeNil())
			Expect(fresh2.Status.PodRef.Name).To(Equal(pod1Name),
				"same subject → same pod name in per-user mode")

			// Cleanup.
			for _, obj := range []ctrlclient.Object{sess1, sess2, ws} {
				obj.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, obj)
				_ = k8sClient.Delete(ctx, obj)
			}
		})
	})

	// --- Spec 5: Mode per-attach — creates ephemeral pod per session ---
	Describe("Mode per-attach: unique pod per session", func() {
		It("creates a distinct pod for each session UID", func() {
			seed := GinkgoRandomSeed()
			wsName := fmt.Sprintf("ws-perattach-%d", seed)
			ws := makeInteractiveWorkspace("default", wsName)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			subject := "user:carol@example.com"
			sess1 := makeSession("default",
				fmt.Sprintf("sess-pa-1-%d", seed), wsName, subject, "default",
				workspacev1alpha1.SessionModePerAttach,
			)
			sess2 := makeSession("default",
				fmt.Sprintf("sess-pa-2-%d", seed), wsName, subject, "default",
				workspacev1alpha1.SessionModePerAttach,
			)
			Expect(k8sClient.Create(ctx, sess1)).To(Succeed())
			Expect(k8sClient.Create(ctx, sess2)).To(Succeed())

			r := &WorkspaceSessionReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}

			nsn1 := types.NamespacedName{Namespace: "default", Name: sess1.Name}
			nsn2 := types.NamespacedName{Namespace: "default", Name: sess2.Name}
			reconcileN(ctx, r, nsn1, 2)
			reconcileN(ctx, r, nsn2, 2)

			var f1, f2 workspacev1alpha1.WorkspaceSession
			Expect(k8sClient.Get(ctx, nsn1, &f1)).To(Succeed())
			Expect(k8sClient.Get(ctx, nsn2, &f2)).To(Succeed())
			Expect(f1.Status.PodRef).NotTo(BeNil())
			Expect(f2.Status.PodRef).NotTo(BeNil())
			Expect(f1.Status.PodRef.Name).NotTo(Equal(f2.Status.PodRef.Name),
				"per-attach mode must create distinct pods for distinct session UIDs")

			// Cleanup.
			for _, obj := range []ctrlclient.Object{sess1, sess2, ws} {
				obj.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, obj)
				_ = k8sClient.Delete(ctx, obj)
			}
		})
	})

	// --- Spec 6: AttachPolicy Reuse vs New plumbing ---
	// AttachPolicy is on the parent Workspace. The reconciler inherits the pod
	// determined by Mode; Reuse vs New affects the Workspace controller (not
	// directly this reconciler), so we verify that the field is readable and
	// the session reconciler does not fail when it is set to either value.
	Describe("AttachPolicy field is not blocking", func() {
		It("reconciles successfully regardless of Workspace.spec.attachPolicy", func() {
			for _, policy := range []workspacev1alpha1.AttachPolicy{
				workspacev1alpha1.AttachPolicyNew,
				workspacev1alpha1.AttachPolicyReuse,
			} {
				seed := GinkgoRandomSeed()
				policyLower := strings.ToLower(string(policy))
				wsName := fmt.Sprintf("ws-ap-%s-%d", policyLower, seed)
				ws := makeInteractiveWorkspace("default", wsName)
				ws.Spec.AttachPolicy = policy
				Expect(k8sClient.Create(ctx, ws)).To(Succeed())

				sess := makeSession("default",
					fmt.Sprintf("sess-ap-%s-%d", policyLower, seed),
					wsName, "user:dana@example.com", "default",
					workspacev1alpha1.SessionModePerAttach,
				)
				Expect(k8sClient.Create(ctx, sess)).To(Succeed())
				nsn := types.NamespacedName{Namespace: sess.Namespace, Name: sess.Name}

				r := &WorkspaceSessionReconciler{
					Client:   k8sClient,
					Scheme:   k8sClient.Scheme(),
					Recorder: &noopRecorder{},
					Rebac:    &FakeRebacWriter{},
				}
				reconcileN(ctx, r, nsn, 2)

				var fresh workspacev1alpha1.WorkspaceSession
				Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
				Expect(fresh.Status.PodRef).NotTo(BeNil(),
					"pod must be provisioned for AttachPolicy=%s", policy)

				// Cleanup.
				for _, obj := range []ctrlclient.Object{sess, ws} {
					obj.SetFinalizers(nil)
					_ = k8sClient.Update(ctx, obj)
					_ = k8sClient.Delete(ctx, obj)
				}
			}
		})
	})

	// --- Spec 7: Idle eviction after attachGraceSeconds ---
	Describe("Idle eviction", func() {
		It("transitions to Draining then Evicted when grace period expires", func() {
			seed := GinkgoRandomSeed()
			wsName := fmt.Sprintf("ws-idle-%d", seed)
			ws := makeInteractiveWorkspace("default", wsName)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			sess := makeSession("default",
				fmt.Sprintf("sess-idle-%d", seed),
				wsName, "user:eve@example.com", "default",
				workspacev1alpha1.SessionModePerAttach,
			)
			sess.Spec.AttachGraceSeconds = 1 // 1s grace for fast test
			Expect(k8sClient.Create(ctx, sess)).To(Succeed())
			nsn := types.NamespacedName{Namespace: sess.Namespace, Name: sess.Name}

			r := &WorkspaceSessionReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}

			// Provision session and advance to Active manually via status patch.
			reconcileN(ctx, r, nsn, 2)

			var freshSess workspacev1alpha1.WorkspaceSession
			Expect(k8sClient.Get(ctx, nsn, &freshSess)).To(Succeed())
			origSess := freshSess.DeepCopy()

			// Inject Active phase + a LastActivityAt that is already expired.
			freshSess.Status.Phase = workspacev1alpha1.WorkspaceSessionPhaseActive
			expiredTime := metav1.NewTime(time.Now().Add(-5 * time.Second))
			freshSess.Status.LastActivityAt = &expiredTime
			Expect(k8sClient.Status().Patch(ctx, &freshSess, ctrlclient.MergeFrom(origSess))).To(Succeed())

			// Reconcile — should detect idle timeout.
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			var afterIdle workspacev1alpha1.WorkspaceSession
			Expect(k8sClient.Get(ctx, nsn, &afterIdle)).To(Succeed())
			// Phase should be Draining (or Evicted if the drain completed in the same pass).
			Expect(afterIdle.Status.Phase).To(BeElementOf(
				workspacev1alpha1.WorkspaceSessionPhaseDraining,
				workspacev1alpha1.WorkspaceSessionPhaseEvicted,
			), "session should start draining or be evicted after grace period")

			// Cleanup.
			for _, obj := range []ctrlclient.Object{sess, ws} {
				obj.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, obj)
				_ = k8sClient.Delete(ctx, obj)
			}
		})
	})

	// --- Spec 8: Finalizer cascade — delete session tears down pod + tuple ---
	Describe("Finalizer cascade on delete", func() {
		It("deletes session pod and removes ReBAC tuple on WorkspaceSession delete", func() {
			seed := GinkgoRandomSeed()
			wsName := fmt.Sprintf("ws-fin-sess-%d", seed)
			ws := makeInteractiveWorkspace("default", wsName)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			sess := makeSession("default",
				fmt.Sprintf("sess-fin-%d", seed),
				wsName, "user:frank@example.com", "default",
				workspacev1alpha1.SessionModePerAttach,
			)
			Expect(k8sClient.Create(ctx, sess)).To(Succeed())
			nsn := types.NamespacedName{Namespace: sess.Namespace, Name: sess.Name}

			fakeReb := &FakeRebacWriter{}
			r := &WorkspaceSessionReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    fakeReb,
			}

			// Provision.
			reconcileN(ctx, r, nsn, 2)

			var provisionedSess workspacev1alpha1.WorkspaceSession
			Expect(k8sClient.Get(ctx, nsn, &provisionedSess)).To(Succeed())
			Expect(provisionedSess.Status.PodRef).NotTo(BeNil())
			podName := provisionedSess.Status.PodRef.Name

			// Delete the WorkspaceSession.
			Expect(k8sClient.Delete(ctx, &provisionedSess)).To(Succeed())

			// Reconcile cleanup loop.
			Eventually(func(g Gomega) {
				_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(fakeReb.Deleted).NotTo(BeEmpty(), "ReBAC tuple must be deleted on cleanup")
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			// Pod should be gone.
			Eventually(func() bool {
				var pod corev1.Pod
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Namespace: "default", Name: podName,
				}, &pod))
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue(),
				"session pod must be deleted after finalizer cleanup")

			// WorkspaceSession itself should be gone.
			Eventually(func() bool {
				var gone workspacev1alpha1.WorkspaceSession
				return errors.IsNotFound(k8sClient.Get(ctx, nsn, &gone))
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue(),
				"WorkspaceSession must be removed after finalizer is cleared")

			// Cleanup workspace.
			ws.SetFinalizers(nil)
			_ = k8sClient.Update(ctx, ws)
			_ = k8sClient.Delete(ctx, ws)
		})
	})

	// --- Spec 9: SIGTERM drain (context cancellation) ---
	Describe("SIGTERM drain (context cancellation)", func() {
		It("returns cleanly when context is cancelled mid-reconcile", func() {
			cancelCtx, cancelFn := context.WithCancel(context.Background())
			seed := GinkgoRandomSeed()
			ws := makeInteractiveWorkspace("default", fmt.Sprintf("ws-drain-sess-%d", seed))
			Expect(k8sClient.Create(context.Background(), ws)).To(Succeed())

			sess := makeSession("default",
				fmt.Sprintf("sess-drain-%d", seed),
				ws.Name, "user:grace@example.com", "default",
				workspacev1alpha1.SessionModePerAttach,
			)
			Expect(k8sClient.Create(context.Background(), sess)).To(Succeed())
			nsn := types.NamespacedName{Namespace: sess.Namespace, Name: sess.Name}

			r := &WorkspaceSessionReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}

			// Cancel the context immediately.
			cancelFn()
			_, err := r.Reconcile(cancelCtx, reconcile.Request{NamespacedName: nsn})
			if err != nil {
				Expect(err.Error()).To(SatisfyAny(
					ContainSubstring("context canceled"),
					ContainSubstring("not found"),
				))
			}

			// Cleanup.
			for _, obj := range []ctrlclient.Object{sess, ws} {
				obj.SetFinalizers(nil)
				_ = k8sClient.Update(context.Background(), obj)
				_ = k8sClient.Delete(context.Background(), obj)
			}
		})
	})
})
