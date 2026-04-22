// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package workspace

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	workspacev1alpha1 "github.com/keese-ai/keese/api/workspace/v1alpha1"
)

func makeWorkspaceShare(ns, name, workspaceName, targetNS string) *workspacev1alpha1.WorkspaceShare {
	return &workspacev1alpha1.WorkspaceShare{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: workspacev1alpha1.WorkspaceShareSpec{
			WorkspaceRef:    workspacev1alpha1.LocalObjectReference{Name: workspaceName},
			TargetNamespace: targetNS,
			ReadOnly:        true,
			Grantees:        []string{"dave", "eve"},
		},
	}
}

var _ = Describe("WorkspaceShareReconciler", func() {

	// --- Basic reconcile test ---
	Describe("Reconcile without backing Workspace", func() {
		var (
			share *workspacev1alpha1.WorkspaceShare
			nsn   types.NamespacedName
			r     *WorkspaceShareReconciler
		)

		BeforeEach(func() {
			share = makeWorkspaceShare("default",
				fmt.Sprintf("wss-basic-%d", GinkgoRandomSeed()),
				"nonexistent-ws",
				"target-ns",
			)
			Expect(k8sClient.Create(ctx, share)).To(Succeed())
			nsn = types.NamespacedName{Namespace: share.Namespace, Name: share.Name}
			r = &WorkspaceShareReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}
		})

		AfterEach(func() {
			toDelete := &workspacev1alpha1.WorkspaceShare{}
			if err := k8sClient.Get(ctx, nsn, toDelete); err == nil {
				toDelete.Finalizers = nil
				_ = k8sClient.Update(ctx, toDelete)
				_ = k8sClient.Delete(ctx, toDelete)
			}
		})

		It("adds the cleanup finalizer on first reconcile", func() {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			var fresh workspacev1alpha1.WorkspaceShare
			Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
			Expect(fresh.Finalizers).To(ContainElement(workspaceShareFinalizer))
		})

		It("sets Progressing condition when Workspace is not found", func() {
			// First reconcile adds finalizer and re-reads.
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile hits the WorkspaceNotFound path.
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
			Expect(err).NotTo(HaveOccurred())

			var fresh workspacev1alpha1.WorkspaceShare
			Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
			var progressing *metav1.Condition
			for i := range fresh.Status.Conditions {
				if fresh.Status.Conditions[i].Type == workspacev1alpha1.WorkspaceShareConditionProgressing {
					progressing = &fresh.Status.Conditions[i]
				}
			}
			Expect(progressing).NotTo(BeNil())
			Expect(progressing.Status).To(Equal(metav1.ConditionTrue))
			Expect(progressing.Reason).To(Equal("WorkspaceNotFound"))
		})
	})

	// --- ReBAC tuples test with backing Workspace ---
	// Uses the running manager (not a per-test reconciler) and asserts via status.
	Describe("ReBAC tuples with backing Workspace", func() {
		It("sets Ready=True and RebacTupleCount>0 after manager reconciles", func() {
			// Unique per-It names to avoid cross-test collisions.
			suffix := fmt.Sprintf("%d-ready", GinkgoParallelProcess())
			wsName := fmt.Sprintf("ws-for-share-%s", suffix)
			ws := makeWorkspace("default", wsName)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			share := makeWorkspaceShare("default",
				fmt.Sprintf("wss-rebac-%s", suffix),
				wsName,
				"target-ns",
			)
			Expect(k8sClient.Create(ctx, share)).To(Succeed())
			wssNSN := types.NamespacedName{Namespace: share.Namespace, Name: share.Name}

			// Wait for the running manager to drive the share to Ready.
			Eventually(func(g Gomega) {
				var fresh workspacev1alpha1.WorkspaceShare
				g.Expect(k8sClient.Get(ctx, wssNSN, &fresh)).To(Succeed())
				g.Expect(fresh.Status.RebacTupleCount).To(BeNumerically(">", 0))
				var ready *metav1.Condition
				for i := range fresh.Status.Conditions {
					if fresh.Status.Conditions[i].Type == workspacev1alpha1.WorkspaceShareConditionReady {
						ready = &fresh.Status.Conditions[i]
					}
				}
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			// Cleanup.
			for _, obj := range []ctrlclient.Object{share, ws} {
				obj.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, obj)
				_ = k8sClient.Delete(ctx, obj)
			}
		})
	})

	// --- Cleanup / finalizer cascade test ---
	Describe("Cleanup on deletion", func() {
		var (
			ws      *workspacev1alpha1.Workspace
			share   *workspacev1alpha1.WorkspaceShare
			wssNSN  types.NamespacedName
			fakeReb *FakeRebacWriter
			r       *WorkspaceShareReconciler
		)

		BeforeEach(func() {
			seed := GinkgoRandomSeed()
			wsName := fmt.Sprintf("ws-del-share-%d", seed)
			ws = makeWorkspace("default", wsName)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			share = makeWorkspaceShare("default",
				fmt.Sprintf("wss-del-%d", seed),
				wsName,
				"target-ns",
			)
			Expect(k8sClient.Create(ctx, share)).To(Succeed())
			wssNSN = types.NamespacedName{Namespace: share.Namespace, Name: share.Name}

			fakeReb = &FakeRebacWriter{}
			r = &WorkspaceShareReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    fakeReb,
			}

			// Provision via two reconciles.
			for i := 0; i < 2; i++ {
				_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: wssNSN})
				Expect(err).NotTo(HaveOccurred())
			}
		})

		AfterEach(func() {
			for _, obj := range []ctrlclient.Object{ws} {
				obj.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, obj)
				_ = k8sClient.Delete(ctx, obj)
			}
		})

		It("deletes ReBAC tuples and removes finalizer on delete", func() {
			toDelete := &workspacev1alpha1.WorkspaceShare{}
			Expect(k8sClient.Get(ctx, wssNSN, toDelete)).To(Succeed())
			Expect(k8sClient.Delete(ctx, toDelete)).To(Succeed())

			Eventually(func(g Gomega) {
				_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: wssNSN})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(fakeReb.Deleted).NotTo(BeEmpty(), "tuples must be deleted")
			}, 10*time.Second, 250*time.Millisecond).Should(Succeed())

			Eventually(func() bool {
				var gone workspacev1alpha1.WorkspaceShare
				return errors.IsNotFound(k8sClient.Get(ctx, wssNSN, &gone))
			}, 10*time.Second, 250*time.Millisecond).Should(BeTrue())
		})
	})

	// --- Editor tuples for non-ReadOnly share ---
	// Verified via rebacTuplesForShare unit logic (pure function, no envtest needed).
	Describe("Editor tuples for non-ReadOnly share", func() {
		It("rebacTuplesForShare returns editor relation when ReadOnly=false", func() {
			share := &workspacev1alpha1.WorkspaceShare{
				Spec: workspacev1alpha1.WorkspaceShareSpec{
					WorkspaceRef:    workspacev1alpha1.LocalObjectReference{Name: "test-ws"},
					TargetNamespace: "ns-b",
					ReadOnly:        false,
					Grantees:        []string{"frank"},
				},
			}
			share.Name = "test-share"
			ws := &workspacev1alpha1.Workspace{}
			ws.Name = "test-ws"

			tuples := rebacTuplesForShare(share, ws)
			relations := map[string]bool{}
			for _, t := range tuples {
				relations[t.Relation] = true
			}
			Expect(relations["editor"]).To(BeTrue(), "non-ReadOnly share must produce editor tuples")
		})

		It("rebacTuplesForShare returns viewer relation when ReadOnly=true", func() {
			share := &workspacev1alpha1.WorkspaceShare{
				Spec: workspacev1alpha1.WorkspaceShareSpec{
					WorkspaceRef:    workspacev1alpha1.LocalObjectReference{Name: "test-ws"},
					TargetNamespace: "ns-b",
					ReadOnly:        true,
					Grantees:        []string{"dave"},
				},
			}
			share.Name = "test-share"
			ws := &workspacev1alpha1.Workspace{}
			ws.Name = "test-ws"

			tuples := rebacTuplesForShare(share, ws)
			relations := map[string]bool{}
			for _, t := range tuples {
				relations[t.Relation] = true
			}
			Expect(relations["viewer"]).To(BeTrue(), "ReadOnly share must produce viewer tuples")
		})
	})

	// --- Idempotency ---
	Describe("Share idempotency", func() {
		It("converges in ≤3 reconciles with no spec change", func() {
			seed := GinkgoRandomSeed()
			wsName := fmt.Sprintf("ws-share-idemp-%d", seed)
			ws := makeWorkspace("default", wsName)
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			share := makeWorkspaceShare("default",
				fmt.Sprintf("wss-idemp-%d", seed),
				wsName,
				"target-ns",
			)
			Expect(k8sClient.Create(ctx, share)).To(Succeed())
			nsn := types.NamespacedName{Namespace: share.Namespace, Name: share.Name}

			r := &WorkspaceShareReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &noopRecorder{},
				Rebac:    &FakeRebacWriter{},
			}

			var lastVersion string
			for i := 0; i < 3; i++ {
				_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nsn})
				Expect(err).NotTo(HaveOccurred())
				var fresh workspacev1alpha1.WorkspaceShare
				Expect(k8sClient.Get(ctx, nsn, &fresh)).To(Succeed())
				if i == 2 {
					Expect(fresh.ResourceVersion).To(Equal(lastVersion),
						"no spec change → ResourceVersion stable on pass 3")
				}
				lastVersion = fresh.ResourceVersion
			}

			// Cleanup.
			for _, obj := range []ctrlclient.Object{share, ws} {
				obj.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, obj)
				_ = k8sClient.Delete(ctx, obj)
			}
		})
	})
})

