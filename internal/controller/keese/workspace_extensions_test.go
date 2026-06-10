// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package keese

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// CH3: the Workspace reconciler wires the extension:E#enabled_in@workspace:W
// tuple. Linkage: a RuntimeExtension is enabled in a Workspace when both
// reference the same AgentRuntime via spec.runtimeRef (see
// workspace_extensions.go). EH11's revisit_when_enabled_in_wired depends on
// these assertions.
var _ = Describe("WorkspaceReconciler extension enabled_in wiring (CH3)", func() {
	var (
		ws       *keesev1alpha1.Workspace
		ext      *keesev1alpha1.RuntimeExtension
		wsNSN    types.NamespacedName
		fakeRT   *RuntimeFakeRebacWriter
		recorder *record.FakeRecorder
		r        *WorkspaceReconciler
	)

	BeforeEach(func() {
		seed := GinkgoRandomSeed()

		// RuntimeExtension bound to the shared default-runtime AgentRuntime
		// fixture created in BeforeSuite. Same runtimeRef as the Workspace below.
		ext = &keesev1alpha1.RuntimeExtension{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("ext-enabled-%d", seed),
				Namespace: "default",
			},
			Spec: keesev1alpha1.RuntimeExtensionSpec{
				RuntimeRef: keesev1alpha1.RuntimeRef{Name: "default-runtime"},
			},
		}
		Expect(k8sClient.Create(ctx, ext)).To(Succeed())

		ws = makeWorkspace("default", fmt.Sprintf("ws-enabled-%d", seed))
		// makeWorkspace already sets RuntimeRef.Name = "default-runtime".
		Expect(k8sClient.Create(ctx, ws)).To(Succeed())
		wsNSN = mustNamespacedName(ws.Namespace, ws.Name)

		fakeRT = NewRuntimeFakeRebacWriter()
		// Buffered FakeRecorder so Eventf never blocks; we drain it for assertions.
		recorder = record.NewFakeRecorder(64)
		r = &WorkspaceReconciler{
			Client:       k8sClient,
			Scheme:       k8sClient.Scheme(),
			Recorder:     recorder,
			Rebac:        &WorkspaceFakeRebacWriter{},
			RuntimeRebac: fakeRT,
		}
	})

	AfterEach(func() {
		toDelete := &keesev1alpha1.Workspace{}
		if err := k8sClient.Get(ctx, wsNSN, toDelete); err == nil {
			toDelete.Finalizers = nil
			_ = k8sClient.Update(ctx, toDelete)
			_ = k8sClient.Delete(ctx, toDelete)
		}
		extDel := &keesev1alpha1.RuntimeExtension{}
		extNSN := mustNamespacedName(ext.Namespace, ext.Name)
		if err := k8sClient.Get(ctx, extNSN, extDel); err == nil {
			extDel.Finalizers = nil
			_ = k8sClient.Update(ctx, extDel)
			_ = k8sClient.Delete(ctx, extDel)
		}
	})

	// drainHasReason reports whether any buffered event carries the given reason.
	drainHasReason := func(reason string) bool {
		found := false
		for {
			select {
			case ev := <-recorder.Events:
				// FakeRecorder formats events as "<Type> <Reason> <message>".
				if strings.Contains(ev, reason) {
					found = true
				}
			default:
				return found
			}
		}
	}

	It("writes the enabled_in tuple + ExtensionTupleWritten event on bind", func() {
		req := reconcile.Request{NamespacedName: wsNSN}
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		Expect(fakeRT.EnabledInTuples[ext.Name]).To(HaveKey(ws.Name),
			"enabled_in tuple extension:%s#enabled_in@workspace:%s must be written", ext.Name, ws.Name)
		Expect(drainHasReason(ReasonExtensionTupleWritten)).To(BeTrue(),
			"ExtensionTupleWritten event must be emitted")
	})

	It("is idempotent over ≥3 reconciles (single enabled_in tuple)", func() {
		req := reconcile.Request{NamespacedName: wsNSN}
		for i := 0; i < 3; i++ {
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
		}
		// The fake stores enabled_in as a set keyed by workspace name, so a
		// re-written tuple does not duplicate. Exactly one binding for this ext.
		Expect(fakeRT.EnabledInTuples[ext.Name]).To(HaveLen(1))
		Expect(fakeRT.EnabledInTuples[ext.Name]).To(HaveKey(ws.Name))
	})

	It("removes the enabled_in tuple + ExtensionTupleDeleted event on workspace finalize", func() {
		req := reconcile.Request{NamespacedName: wsNSN}
		// Bind first.
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(fakeRT.EnabledInTuples[ext.Name]).To(HaveKey(ws.Name))
		_ = drainHasReason(ReasonExtensionTupleWritten) // clear the write event

		// Delete the workspace → triggers the cleanup/finalize path.
		toDelete := &keesev1alpha1.Workspace{}
		Expect(k8sClient.Get(ctx, wsNSN, toDelete)).To(Succeed())
		Expect(k8sClient.Delete(ctx, toDelete)).To(Succeed())

		Eventually(func(g Gomega) {
			_, err := r.Reconcile(ctx, req)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(fakeRT.EnabledInTuples[ext.Name]).NotTo(HaveKey(ws.Name),
				"enabled_in tuple must be removed on finalize")
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Expect(drainHasReason(ReasonExtensionTupleDeleted)).To(BeTrue(),
			"ExtensionTupleDeleted event must be emitted")
	})

	It("does not bind an extension referencing a different runtime", func() {
		// An extension pointing at a different AgentRuntime is NOT enabled in
		// this workspace — no tuple should be written for it.
		other := &keesev1alpha1.RuntimeExtension{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("ext-other-%d", GinkgoRandomSeed()),
				Namespace: "default",
			},
			Spec: keesev1alpha1.RuntimeExtensionSpec{
				RuntimeRef: keesev1alpha1.RuntimeRef{Name: "some-other-runtime"},
			},
		}
		Expect(k8sClient.Create(ctx, other)).To(Succeed())
		defer func() {
			fresh := &keesev1alpha1.RuntimeExtension{}
			if e := k8sClient.Get(ctx, mustNamespacedName(other.Namespace, other.Name), fresh); e == nil {
				fresh.Finalizers = nil
				_ = k8sClient.Update(ctx, fresh)
				_ = k8sClient.Delete(ctx, fresh)
			}
		}()

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: wsNSN})
		Expect(err).NotTo(HaveOccurred())

		Expect(fakeRT.EnabledInTuples[ext.Name]).To(HaveKey(ws.Name),
			"matching extension is bound")
		Expect(fakeRT.EnabledInTuples).NotTo(HaveKey(other.Name),
			"non-matching extension must not be bound")
	})
})
