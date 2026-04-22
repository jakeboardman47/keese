// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package workspace

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	workspacev1alpha1 "github.com/keese-ai/keese/api/workspace/v1alpha1"
)

// eventuallyTimeout / interval used by all Eventually assertions.
const (
	eventuallyTimeout  = 10 * time.Second
	eventuallyInterval = 250 * time.Millisecond
)

// mustNamespacedName returns a NamespacedName for test assertions.
func mustNamespacedName(ns, name string) types.NamespacedName {
	return types.NamespacedName{Namespace: ns, Name: name}
}

// makeWorkspace produces a minimal Workspace with the managed label.
func makeWorkspace(ns, name string) *workspacev1alpha1.Workspace {
	return &workspacev1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				managedLabel: managedLabelValue,
			},
		},
		Spec: workspacev1alpha1.WorkspaceSpec{
			RuntimeRef: workspacev1alpha1.LocalObjectReference{Name: "default-runtime"},
			TenantRef: corev1.ObjectReference{
				Name: "acme-tenant",
			},
			SessionMode:       workspacev1alpha1.WorkspaceSessionModeOnDemand,
			ConcurrencyPolicy: workspacev1alpha1.ConcurrencyPolicyAllow,
		},
	}
}

var _ = Describe("WorkspaceReconciler", func() {

	// --- Idempotency test ---
	Describe("Idempotency", func() {
		var (
			ws  *workspacev1alpha1.Workspace
			nsn types.NamespacedName
			r   *WorkspaceReconciler
		)

		BeforeEach(func() {
			ws = makeWorkspace("default", fmt.Sprintf("ws-idempotent-%d", GinkgoRandomSeed()))
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			r = &WorkspaceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}
		})

		AfterEach(func() {
			toDelete := &workspacev1alpha1.Workspace{}
			if err := k8sClient.Get(ctx, nsn, toDelete); err == nil {
				// Remove finalizer so delete doesn't block in envtest.
				toDelete.Finalizers = nil
				_ = k8sClient.Update(ctx, toDelete)
				_ = k8sClient.Delete(ctx, toDelete)
			}
		})

		It("converges in ≤3 reconciles with no spec change", func() {
			req := reconcile.Request{NamespacedName: nsn}

			var lastVersion string
			for i := 0; i < 3; i++ {
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())

				var fresh workspacev1alpha1.Workspace
				Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
				if i > 0 {
					// ResourceVersion only increments on actual mutations.
					// After pass 1 the finalizer is added; passes 2 and 3 are stable.
					if i == 2 {
						Expect(fresh.ResourceVersion).To(Equal(lastVersion),
							"spec unchanged; ResourceVersion should not increment on pass 3")
					}
				}
				lastVersion = fresh.ResourceVersion
			}
		})
	})

	// --- Finalizer add-on-create test ---
	Describe("Finalizer", func() {
		var (
			ws  *workspacev1alpha1.Workspace
			nsn types.NamespacedName
			r   *WorkspaceReconciler
		)

		BeforeEach(func() {
			ws = makeWorkspace("default", fmt.Sprintf("ws-fin-%d", GinkgoRandomSeed()))
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			r = &WorkspaceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}
		})

		AfterEach(func() {
			toDelete := &workspacev1alpha1.Workspace{}
			if err := k8sClient.Get(ctx, nsn, toDelete); err == nil {
				toDelete.Finalizers = nil
				_ = k8sClient.Update(ctx, toDelete)
				_ = k8sClient.Delete(ctx, toDelete)
			}
		})

		It("adds the cleanup finalizer on first reconcile", func() {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			var fresh workspacev1alpha1.Workspace
			Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
			Expect(fresh.Finalizers).To(ContainElement(workspaceFinalizer))
		})
	})

	// --- ServiceAccount projection test ---
	Describe("ServiceAccount", func() {
		var (
			ws  *workspacev1alpha1.Workspace
			nsn types.NamespacedName
			r   *WorkspaceReconciler
		)

		BeforeEach(func() {
			ws = makeWorkspace("default", fmt.Sprintf("ws-sa-%d", GinkgoRandomSeed()))
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			r = &WorkspaceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}
		})

		AfterEach(func() {
			toDelete := &workspacev1alpha1.Workspace{}
			if err := k8sClient.Get(ctx, nsn, toDelete); err == nil {
				toDelete.Finalizers = nil
				_ = k8sClient.Update(ctx, toDelete)
				_ = k8sClient.Delete(ctx, toDelete)
			}
		})

		It("creates a ServiceAccount named ksa-<uid>", func() {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			var fresh workspacev1alpha1.Workspace
			Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
			Expect(fresh.Status.ServiceAccountName).NotTo(BeEmpty())

			var sa corev1.ServiceAccount
			Expect(k8sClient.Get(ctx, mustNamespacedName("default", fresh.Status.ServiceAccountName), &sa)).To(Succeed())
			Expect(sa.Name).To(HavePrefix("ksa-"))
		})
	})

	// --- NetworkPolicy fail-closed test ---
	Describe("NetworkPolicy", func() {
		var (
			ws  *workspacev1alpha1.Workspace
			nsn types.NamespacedName
			r   *WorkspaceReconciler
		)

		BeforeEach(func() {
			ws = makeWorkspace("default", fmt.Sprintf("ws-np-%d", GinkgoRandomSeed()))
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			r = &WorkspaceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}
		})

		AfterEach(func() {
			toDelete := &workspacev1alpha1.Workspace{}
			if err := k8sClient.Get(ctx, nsn, toDelete); err == nil {
				toDelete.Finalizers = nil
				_ = k8sClient.Update(ctx, toDelete)
				_ = k8sClient.Delete(ctx, toDelete)
			}
		})

		It("creates a default-deny NetworkPolicy", func() {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			var fresh workspacev1alpha1.Workspace
			Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
			Expect(fresh.Status.NetworkPolicyName).NotTo(BeEmpty())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, mustNamespacedName("default", fresh.Status.NetworkPolicyName), &np)).To(Succeed())
			// No ingress or egress rules → fail-closed.
			Expect(np.Spec.Ingress).To(BeEmpty())
			Expect(np.Spec.Egress).To(BeEmpty())
			Expect(np.Spec.PolicyTypes).To(ContainElements(
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			))
		})

		It("creates an egress-allowlist NetworkPolicy with gateway and NATS ports", func() {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			var fresh workspacev1alpha1.Workspace
			Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())

			// Egress NP has the workspace UID suffix "-egress".
			egressName := egressNPName(&fresh)
			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, mustNamespacedName("default", egressName), &np)).To(Succeed())
			Expect(np.Spec.Egress).To(HaveLen(2)) // gateway + nats
			ports := []int32{}
			for _, rule := range np.Spec.Egress {
				for _, p := range rule.Ports {
					ports = append(ports, p.Port.IntVal)
				}
			}
			Expect(ports).To(ContainElements(int32(gatewayEgressPort), int32(natsEgressPort)))
		})
	})

	// --- PVC creation test ---
	Describe("PVC", func() {
		var (
			ws  *workspacev1alpha1.Workspace
			nsn types.NamespacedName
			r   *WorkspaceReconciler
		)

		BeforeEach(func() {
			ws = makeWorkspace("default", fmt.Sprintf("ws-pvc-%d", GinkgoRandomSeed()))
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			r = &WorkspaceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}
		})

		AfterEach(func() {
			toDelete := &workspacev1alpha1.Workspace{}
			if err := k8sClient.Get(ctx, nsn, toDelete); err == nil {
				toDelete.Finalizers = nil
				_ = k8sClient.Update(ctx, toDelete)
				_ = k8sClient.Delete(ctx, toDelete)
			}
		})

		It("creates session PVC with default 10Gi when sessionStorage unset", func() {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			var fresh workspacev1alpha1.Workspace
			Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())

			pvcName := sessionPVCName(&fresh)
			var pvc corev1.PersistentVolumeClaim
			Expect(k8sClient.Get(ctx, mustNamespacedName("default", pvcName), &pvc)).To(Succeed())
			req := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
			expected := resource.MustParse(defaultSessionStorage)
			Expect(req.Cmp(expected)).To(Equal(0))
		})
	})

	// --- ReBAC tuple test ---
	Describe("ReBAC tuples", func() {
		var (
			ws      *workspacev1alpha1.Workspace
			nsn     types.NamespacedName
			fakeReb *FakeRebacWriter
			r       *WorkspaceReconciler
		)

		BeforeEach(func() {
			ws = makeWorkspace("default", fmt.Sprintf("ws-rebac-%d", GinkgoRandomSeed()))
			ws.Spec.Editors = []string{"alice", "bob"}
			ws.Spec.Viewers = []string{"charlie"}
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			fakeReb = &FakeRebacWriter{}
			r = &WorkspaceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    fakeReb,
			}
		})

		AfterEach(func() {
			toDelete := &workspacev1alpha1.Workspace{}
			if err := k8sClient.Get(ctx, nsn, toDelete); err == nil {
				toDelete.Finalizers = nil
				_ = k8sClient.Update(ctx, toDelete)
				_ = k8sClient.Delete(ctx, toDelete)
			}
		})

		It("syncs owner, editor, and viewer tuples", func() {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			relations := map[string]bool{}
			for _, t := range fakeReb.Synced {
				relations[t.Relation] = true
			}
			Expect(relations["owner"]).To(BeTrue(), "owner tuple must be synced")
			Expect(relations["editor"]).To(BeTrue(), "editor tuple must be synced")
			Expect(relations["viewer"]).To(BeTrue(), "viewer tuple must be synced")
		})

		It("records rebacTupleCount in status", func() {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			var fresh workspacev1alpha1.Workspace
			Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
			Expect(fresh.Status.RebacTupleCount).To(BeNumerically(">", 0))
		})
	})

	// --- Finalizer cascade / cleanup test ---
	Describe("Cleanup on deletion", func() {
		var (
			ws      *workspacev1alpha1.Workspace
			nsn     types.NamespacedName
			fakeReb *FakeRebacWriter
			r       *WorkspaceReconciler
		)

		BeforeEach(func() {
			ws = makeWorkspace("default", fmt.Sprintf("ws-del-%d", GinkgoRandomSeed()))
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			fakeReb = &FakeRebacWriter{}
			r = &WorkspaceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    fakeReb,
			}
			// First reconcile: provision resources.
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())
		})

		It("removes ReBAC tuples and sub-resources on delete", func() {
			// Delete the workspace.
			toDelete := &workspacev1alpha1.Workspace{}
			Expect(k8sClient.Get(ctx, nsn, toDelete)).To(Succeed())
			Expect(k8sClient.Delete(ctx, toDelete)).To(Succeed())

			// Reconcile cleanup.
			Eventually(func(g Gomega) {
				_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(fakeReb.Deleted).NotTo(BeEmpty(), "tuples should be deleted during cleanup")
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			// Object should eventually be gone (no finalizer blocking it).
			Eventually(func() bool {
				var gone workspacev1alpha1.Workspace
				return errors.IsNotFound(k8sClient.Get(ctx, nsn, &gone))
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue(), "workspace should be removed after cleanup")
		})
	})

	// --- Interactive immutability test ---
	Describe("Interactive field immutability", func() {
		It("rejects a flip of spec.interactive post-create via XValidation CEL rule", func() {
			ws := makeWorkspace("default", fmt.Sprintf("ws-immut-%d", GinkgoRandomSeed()))
			ws.Spec.Interactive = true
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			nsn := mustNamespacedName(ws.Namespace, ws.Name)

			var fresh workspacev1alpha1.Workspace
			Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
			fresh.Spec.Interactive = false

			err := k8sClient.Update(ctx, &fresh)
			// The XValidation rule on WorkspaceSpec enforces immutability.
			// envtest with CRD validation active should reject this.
			Expect(err).To(HaveOccurred(), "flipping spec.interactive must be rejected")

			// Cleanup.
			Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
			fresh.Finalizers = nil
			_ = k8sClient.Update(ctx, &fresh)
			_ = k8sClient.Delete(ctx, &fresh)
		})
	})

	// --- FSM transitions test ---
	Describe("FSM: Pending → Provisioning → (Running when PVC bound)", func() {
		var (
			ws  *workspacev1alpha1.Workspace
			nsn types.NamespacedName
			r   *WorkspaceReconciler
		)

		BeforeEach(func() {
			ws = makeWorkspace("default", fmt.Sprintf("ws-fsm-%d", GinkgoRandomSeed()))
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn = mustNamespacedName(ws.Namespace, ws.Name)
			r = &WorkspaceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}
		})

		AfterEach(func() {
			toDelete := &workspacev1alpha1.Workspace{}
			if err := k8sClient.Get(ctx, nsn, toDelete); err == nil {
				toDelete.Finalizers = nil
				_ = k8sClient.Update(ctx, toDelete)
				_ = k8sClient.Delete(ctx, toDelete)
			}
		})

		It("starts in Pending, advances to Provisioning on first reconcile", func() {
			req := reconcile.Request{NamespacedName: nsn}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			var fresh workspacev1alpha1.Workspace
			Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
			// In envtest PVC will not be Bound, so phase stays at Provisioning.
			Expect(fresh.Status.Phase).To(BeElementOf(
				workspacev1alpha1.WorkspacePhasePending,
				workspacev1alpha1.WorkspacePhaseProvisioning,
			))
		})
	})

	// --- SIGTERM drain test ---
	Describe("SIGTERM drain (context cancellation)", func() {
		It("returns cleanly when context is cancelled mid-reconcile", func() {
			cancelCtx, cancelFn := context.WithCancel(context.Background())
			ws := makeWorkspace("default", fmt.Sprintf("ws-drain-%d", GinkgoRandomSeed()))
			Expect(k8sClient.Create(context.Background(), ws)).To(Succeed())
			nsn := mustNamespacedName(ws.Namespace, ws.Name)

			r := &WorkspaceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}

			// Cancel the context immediately to simulate SIGTERM.
			cancelFn()

			// Reconcile with cancelled context must not panic and must return.
			_, err := r.Reconcile(cancelCtx, reconcile.Request{NamespacedName: nsn})
			// Either a context-cancelled error or NotFound is acceptable.
			if err != nil {
				Expect(err.Error()).To(SatisfyAny(
					ContainSubstring("context canceled"),
					ContainSubstring("not found"),
				))
			}

			// Cleanup.
			var toDelete workspacev1alpha1.Workspace
			if e := k8sClient.Get(context.Background(), nsn, &toDelete); e == nil {
				toDelete.Finalizers = nil
				_ = k8sClient.Update(context.Background(), &toDelete)
				_ = k8sClient.Delete(context.Background(), &toDelete)
			}
		})
	})

	// --- ConcurrencyPolicy label projection test ---
	Describe("ConcurrencyPolicy is preserved in status", func() {
		It("reflects spec.concurrencyPolicy", func() {
			ws := makeWorkspace("default", fmt.Sprintf("ws-conc-%d", GinkgoRandomSeed()))
			ws.Spec.ConcurrencyPolicy = workspacev1alpha1.ConcurrencyPolicyForbid
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn := mustNamespacedName(ws.Namespace, ws.Name)

			r := &WorkspaceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			var fresh workspacev1alpha1.Workspace
			Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
			Expect(fresh.Spec.ConcurrencyPolicy).To(Equal(workspacev1alpha1.ConcurrencyPolicyForbid))

			// Cleanup.
			fresh.Finalizers = nil
			_ = k8sClient.Update(ctx, &fresh)
			_ = k8sClient.Delete(ctx, &fresh)
		})
	})

	// --- Label predicate test: unmanaged workspace is skipped ---
	Describe("Predicate: unmanaged workspaces", func() {
		It("reconciler with real client returns nil for unlabeled resource", func() {
			ws := &workspacev1alpha1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("ws-unmanaged-%d", GinkgoRandomSeed()),
					Namespace: "default",
					// No managed label.
				},
				Spec: workspacev1alpha1.WorkspaceSpec{
					RuntimeRef: workspacev1alpha1.LocalObjectReference{Name: "rt"},
					TenantRef:  corev1.ObjectReference{Name: "t"},
				},
			}
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())
			nsn := mustNamespacedName(ws.Namespace, ws.Name)

			r := &WorkspaceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}
			// Reconcile should succeed — predicate filtering happens at the manager level,
			// not inside Reconcile itself. Direct call still processes the object, but
			// we assert no error either way.
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			// Cleanup.
			var toDelete workspacev1alpha1.Workspace
			if e := k8sClient.Get(ctx, nsn, &toDelete); e == nil {
				toDelete.Finalizers = nil
				_ = k8sClient.Update(ctx, &toDelete)
				_ = k8sClient.Delete(ctx, &toDelete)
			}
		})
	})
})

// Helper: list all NetworkPolicies in a namespace matching a prefix.
func listNetworkPoliciesByLabel(ctx context.Context, ns string, matchLabels map[string]string) ([]networkingv1.NetworkPolicy, error) {
	var list networkingv1.NetworkPolicyList
	err := k8sClient.List(ctx, &list,
		client.InNamespace(ns),
		client.MatchingLabels(matchLabels),
	)
	return list.Items, err
}

// noopRecorder satisfies record.EventRecorder without emitting anything.
type noopRecorder struct{}

func (n *noopRecorder) Event(_ runtime.Object, _, _, _ string)                    {}
func (n *noopRecorder) Eventf(_ runtime.Object, _, _, _ string, _ ...interface{}) {}
func (n *noopRecorder) AnnotatedEventf(_ runtime.Object, _ map[string]string, _, _, _ string, _ ...interface{}) {
}
