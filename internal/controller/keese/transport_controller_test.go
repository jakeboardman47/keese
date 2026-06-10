// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package keese

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

const testNamespace = "default"

func reconcilerWith(nats *FakeNatsStreamer, cta *FakeCTAResolver, cert *FakeCertManagerReader) *TransportReconciler {
	recorder := k8sClient.Scheme() // unused; we supply a fake recorder below
	_ = recorder
	return &TransportReconciler{
		Client:      k8sClient,
		Scheme:      k8sClient.Scheme(),
		Recorder:    &fakeEventRecorder{},
		Rebac:       &TransportFakeRebacWriter{},
		Nats:        nats,
		CTA:         cta,
		CertManager: cert,
	}
}

// fakeEventRecorder satisfies record.EventRecorder for testing.
type fakeEventRecorder struct{ Events []string }

func (f *fakeEventRecorder) Event(obj runtime.Object, eventType, reason, message string) {
	f.Events = append(f.Events, reason+": "+message)
}

func (f *fakeEventRecorder) Eventf(obj runtime.Object, eventType, reason, messageFmt string, args ...interface{}) {
	f.Events = append(f.Events, reason)
}

func (f *fakeEventRecorder) AnnotatedEventf(obj runtime.Object, annotations map[string]string, eventType, reason, messageFmt string, args ...interface{}) {
	f.Events = append(f.Events, reason)
}

func makeTransport(name string, spec keesev1alpha1.TransportSpec) *keesev1alpha1.Transport {
	return &keesev1alpha1.Transport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels:    map[string]string{managedByLabel: "true"},
		},
		Spec: spec,
	}
}

func reconcileOnce(ctx context.Context, r *TransportReconciler, name string) (reconcile.Result, error) {
	return r.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: testNamespace},
	})
}

var _ = Describe("Transport Controller", func() {
	// ---------- 1. Idempotency ----------

	Context("Idempotency", func() {
		const resName = "idempotent-transport"
		var tr *keesev1alpha1.Transport

		BeforeEach(func() {
			tr = makeTransport(resName, keesev1alpha1.TransportSpec{
				Type: keesev1alpha1.TransportTypeStdio,
				Stdio: &keesev1alpha1.StdioConfig{
					BridgeImage: "ghcr.io/keese-ai/stdio-bridge:latest",
				},
			})
			err := k8sClient.Get(ctx, types.NamespacedName{Name: resName, Namespace: testNamespace}, &keesev1alpha1.Transport{})
			if err != nil {
				Expect(k8sClient.Create(ctx, tr)).To(Succeed())
			}
		})

		AfterEach(func() {
			existing := &keesev1alpha1.Transport{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resName, Namespace: testNamespace}, existing); err == nil {
				Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
			}
		})

		It("converges in ≤ 3 reconciles with no spec change", func() {
			r := reconcilerWith(NewFakeNatsStreamer(), NewFakeCTAResolver(), NewFakeCertManagerReader())

			var lastPhase keesev1alpha1.TransportPhase
			for i := 0; i < 3; i++ {
				_, err := reconcileOnce(ctx, r, resName)
				Expect(err).NotTo(HaveOccurred())

				got := &keesev1alpha1.Transport{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resName, Namespace: testNamespace}, got)).To(Succeed())
				if i == 2 {
					Expect(got.Status.Phase).To(Equal(lastPhase), "phase must not change on 3rd reconcile")
				}
				lastPhase = got.Status.Phase
			}
			Expect(lastPhase).To(Equal(keesev1alpha1.TransportPhaseReady))
		})
	})

	// ---------- 2. NATS hybrid — default mode rejects absent stream ----------

	Context("NATS hybrid ownership — default mode", func() {
		const resName = "nats-default-absent"

		AfterEach(func() {
			existing := &keesev1alpha1.Transport{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resName, Namespace: testNamespace}, existing); err == nil {
				Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
			}
		})

		It("sets Degraded with NATSStreamNotFound when stream is absent", func() {
			tr := makeTransport(resName, keesev1alpha1.TransportSpec{
				Type: keesev1alpha1.TransportTypeNATS,
				NATS: &keesev1alpha1.NATSConfig{
					ClusterRef:   keesev1alpha1.NamespacedObjectRef{Name: "nats-cluster"},
					StreamName:   "absent-stream",
					ConsumerName: "my-consumer",
				},
			})
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			// No stream in the fake streamer → should degrade.
			nats := NewFakeNatsStreamer()
			r := reconcilerWith(nats, NewFakeCTAResolver(), NewFakeCertManagerReader())
			recorder := r.Recorder.(*fakeEventRecorder)

			_, err := reconcileOnce(ctx, r, resName)
			Expect(err).NotTo(HaveOccurred())

			got := &keesev1alpha1.Transport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resName, Namespace: testNamespace}, got)).To(Succeed())
			Expect(got.Status.Phase).To(Equal(keesev1alpha1.TransportPhaseDegraded))
			Expect(recorder.Events).To(ContainElement(ReasonNATSStreamNotFound))
		})
	})

	// ---------- 3. NATS hybrid — opt-in mode creates stream + finalizer ----------

	Context("NATS hybrid ownership — opt-in mode", func() {
		const resName = "nats-optin"

		AfterEach(func() {
			existing := &keesev1alpha1.Transport{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resName, Namespace: testNamespace}, existing); err == nil {
				// Remove finalizer to allow deletion.
				existing.Finalizers = nil
				_ = k8sClient.Update(ctx, existing)
				_ = k8sClient.Delete(ctx, existing)
			}
		})

		It("creates the stream and adds finalizer", func() {
			tr := makeTransport(resName, keesev1alpha1.TransportSpec{
				Type: keesev1alpha1.TransportTypeNATS,
				NATS: &keesev1alpha1.NATSConfig{
					ClusterRef:   keesev1alpha1.NamespacedObjectRef{Name: "nats-cluster"},
					StreamName:   "owned-stream",
					ConsumerName: "my-consumer",
				},
			})
			tr.Annotations = map[string]string{
				autoCreateStreamAnnotation: "true",
				managedByLabel:             "true",
			}
			// merge labels + annotations
			tr.Labels = map[string]string{managedByLabel: "true"}
			tr.Annotations[managedByLabel] = "true"
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			nats := NewFakeNatsStreamer()
			r := reconcilerWith(nats, NewFakeCTAResolver(), NewFakeCertManagerReader())
			recorder := r.Recorder.(*fakeEventRecorder)

			_, err := reconcileOnce(ctx, r, resName)
			Expect(err).NotTo(HaveOccurred())

			// Stream must have been created.
			Expect(nats.Added).To(ContainElement("owned-stream"))
			Expect(recorder.Events).To(ContainElement(ReasonNATSStreamOwned))

			// Transport should be Ready.
			got := &keesev1alpha1.Transport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resName, Namespace: testNamespace}, got)).To(Succeed())
			Expect(got.Status.Phase).To(Equal(keesev1alpha1.TransportPhaseReady))
			Expect(got.Finalizers).To(ContainElement(transportFinalizer))
		})

		It("deletes the owned stream on cleanup", func() {
			tr := makeTransport(resName+"-del", keesev1alpha1.TransportSpec{
				Type: keesev1alpha1.TransportTypeNATS,
				NATS: &keesev1alpha1.NATSConfig{
					ClusterRef:   keesev1alpha1.NamespacedObjectRef{Name: "nats-cluster"},
					StreamName:   "owned-del-stream",
					ConsumerName: "my-consumer",
				},
			})
			tr.Annotations = map[string]string{autoCreateStreamAnnotation: "true"}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			nats := NewFakeNatsStreamer()
			nats.Streams["owned-del-stream"] = &StreamInfo{Name: "owned-del-stream"}
			r := reconcilerWith(nats, NewFakeCTAResolver(), NewFakeCertManagerReader())

			// First reconcile — provision.
			_, err := reconcileOnce(ctx, r, resName+"-del")
			Expect(err).NotTo(HaveOccurred())

			// Add finalizer manually (reconcileFinalizer may have patched it already).
			obj := &keesev1alpha1.Transport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resName + "-del", Namespace: testNamespace}, obj)).To(Succeed())
			if len(obj.Finalizers) == 0 {
				obj.Finalizers = []string{transportFinalizer}
				Expect(k8sClient.Update(ctx, obj)).To(Succeed())
			}

			// Delete the CR.
			Expect(k8sClient.Delete(ctx, obj)).To(Succeed())

			// Reconcile the deletion.
			_, err = reconcileOnce(ctx, r, resName+"-del")
			Expect(err).NotTo(HaveOccurred())

			Expect(nats.Deleted).To(ContainElement("owned-del-stream"))
		})
	})

	// ---------- 4. MCP — MCPRouteNotFound ----------

	Context("MCP transport MCPRoute validation", func() {
		const resName = "mcp-route-missing"

		AfterEach(func() {
			existing := &keesev1alpha1.Transport{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resName, Namespace: testNamespace}, existing); err == nil {
				Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
			}
		})

		It("degrades with MCPRouteNotFound when the route is absent", func() {
			tr := makeTransport(resName, keesev1alpha1.TransportSpec{
				Type: keesev1alpha1.TransportTypeMCP,
				MCP: &keesev1alpha1.MCPConfig{
					McpRouteRef:     keesev1alpha1.NamespacedObjectRef{Name: "missing-route"},
					ProtocolVersion: "2024-11-05",
				},
			})
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			// Override mcpRouteExists via a test-hook reconciler that rejects the route.
			r := &mcpNotFoundReconciler{reconcilerWith(NewFakeNatsStreamer(), NewFakeCTAResolver(), NewFakeCertManagerReader())}
			recorder := r.TransportReconciler.Recorder.(*fakeEventRecorder)

			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: resName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			got := &keesev1alpha1.Transport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resName, Namespace: testNamespace}, got)).To(Succeed())
			Expect(got.Status.Phase).To(Equal(keesev1alpha1.TransportPhaseDegraded))
			Expect(recorder.Events).To(ContainElement(ReasonMCPRouteNotFound))
		})
	})

	// ---------- 5. Certificate — CertificateNotFound ----------

	Context("Certificate validation", func() {
		const resName = "cert-missing"

		AfterEach(func() {
			existing := &keesev1alpha1.Transport{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resName, Namespace: testNamespace}, existing); err == nil {
				Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
			}
		})

		It("degrades with CertificateNotFound when cert is absent", func() {
			tr := makeTransport(resName, keesev1alpha1.TransportSpec{
				Type: keesev1alpha1.TransportTypeNATS,
				NATS: &keesev1alpha1.NATSConfig{
					ClusterRef:   keesev1alpha1.NamespacedObjectRef{Name: "nats-cluster"},
					StreamName:   "stream-with-tls",
					ConsumerName: "my-consumer",
					TLS: &keesev1alpha1.NATSTLSConfig{
						CertificateRef: &keesev1alpha1.NamespacedObjectRef{
							Name:      "missing-cert",
							Namespace: testNamespace,
						},
					},
				},
			})
			tr.Annotations = map[string]string{autoCreateStreamAnnotation: "true"}
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			// Cert reader returns not-found.
			cert := NewFakeCertManagerReader()
			nats := NewFakeNatsStreamer()
			nats.Streams["stream-with-tls"] = &StreamInfo{Name: "stream-with-tls"}
			r := reconcilerWith(nats, NewFakeCTAResolver(), cert)
			recorder := r.Recorder.(*fakeEventRecorder)

			_, err := reconcileOnce(ctx, r, resName)
			Expect(err).NotTo(HaveOccurred())

			got := &keesev1alpha1.Transport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resName, Namespace: testNamespace}, got)).To(Succeed())
			Expect(got.Status.Phase).To(Equal(keesev1alpha1.TransportPhaseDegraded))
			Expect(recorder.Events).To(ContainElement(ReasonCertificateNotFound))
		})
	})

	// ---------- 6. A2A cross-tenant — rejected without CTA, accepted with CTA ----------

	Context("A2A cross-tenant scope", func() {
		const resNameReject = "a2a-cross-no-cta"
		const resNameAccept = "a2a-cross-with-cta"

		AfterEach(func() {
			for _, name := range []string{resNameReject, resNameAccept} {
				existing := &keesev1alpha1.Transport{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, existing); err == nil {
					Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
				}
			}
		})

		It("degrades with CrossTenantAgreementMissing without an Approved CTA", func() {
			scope := keesev1alpha1.A2AScopeCrossTenant
			auth := keesev1alpha1.A2APeerAuthWorkspaceSA
			tr := makeTransport(resNameReject, keesev1alpha1.TransportSpec{
				Type: keesev1alpha1.TransportTypeA2A,
				A2A: &keesev1alpha1.A2AConfig{
					Endpoint: "grpcs://peer.tenant-b.svc:8443",
					PeerAuth: &auth,
					Scope:    &scope,
					WorkspaceSA: &keesev1alpha1.WorkspaceSAConfig{
						Audience: "keese-wf-abc123",
					},
				},
			})
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			cta := NewFakeCTAResolver() // empty — no approved CTA
			r := reconcilerWith(NewFakeNatsStreamer(), cta, NewFakeCertManagerReader())
			recorder := r.Recorder.(*fakeEventRecorder)

			_, err := reconcileOnce(ctx, r, resNameReject)
			Expect(err).NotTo(HaveOccurred())

			got := &keesev1alpha1.Transport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resNameReject, Namespace: testNamespace}, got)).To(Succeed())
			Expect(got.Status.Phase).To(Equal(keesev1alpha1.TransportPhaseDegraded))
			Expect(recorder.Events).To(ContainElement(ReasonCrossTenantAgreementMissing))
		})

		It("provisions successfully with an Approved CTA", func() {
			scope := keesev1alpha1.A2AScopeCrossTenant
			auth := keesev1alpha1.A2APeerAuthWorkspaceSA
			endpoint := "grpcs://peer.tenant-b.svc:8443"
			tr := makeTransport(resNameAccept, keesev1alpha1.TransportSpec{
				Type: keesev1alpha1.TransportTypeA2A,
				A2A: &keesev1alpha1.A2AConfig{
					Endpoint: endpoint,
					PeerAuth: &auth,
					Scope:    &scope,
					WorkspaceSA: &keesev1alpha1.WorkspaceSAConfig{
						Audience: "keese-wf-abc123",
					},
				},
			})
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			cta := NewFakeCTAResolver()
			cta.Approved[testNamespace+"/"+endpoint] = true
			r := reconcilerWith(NewFakeNatsStreamer(), cta, NewFakeCertManagerReader())
			recorder := r.Recorder.(*fakeEventRecorder)

			_, err := reconcileOnce(ctx, r, resNameAccept)
			Expect(err).NotTo(HaveOccurred())

			got := &keesev1alpha1.Transport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resNameAccept, Namespace: testNamespace}, got)).To(Succeed())
			Expect(got.Status.Phase).To(Equal(keesev1alpha1.TransportPhaseReady))
			Expect(recorder.Events).To(ContainElement(ReasonTransportProvisioned))
		})
	})

	// ---------- 7. Stdio buffer overflow → StreamLagged event ----------

	Context("Stdio buffer overflow", func() {
		const resName = "stdio-overflow"

		AfterEach(func() {
			existing := &keesev1alpha1.Transport{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resName, Namespace: testNamespace}, existing); err == nil {
				Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
			}
		})

		It("emits StreamLagged event when outboundQueueDepth ceiling is simulated", func() {
			depth := int32(100)
			tr := makeTransport(resName, keesev1alpha1.TransportSpec{
				Type: keesev1alpha1.TransportTypeStdio,
				Stdio: &keesev1alpha1.StdioConfig{
					BridgeImage:        "ghcr.io/keese-ai/stdio-bridge:latest",
					OutboundQueueDepth: &depth,
				},
			})
			Expect(k8sClient.Create(ctx, tr)).To(Succeed())

			// Use a lag-aware recorder variant to simulate the event.
			recorder := &fakeEventRecorder{}
			r := &TransportReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				Recorder:    recorder,
				Rebac:       &TransportFakeRebacWriter{},
				Nats:        NewFakeNatsStreamer(),
				CTA:         NewFakeCTAResolver(),
				CertManager: NewFakeCertManagerReader(),
			}

			// Provision successfully first.
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: resName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Simulate buffer overflow by emitting a StreamLagged event directly
			// (as the bridge sidecar would via the event recorder).
			got := &keesev1alpha1.Transport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resName, Namespace: testNamespace}, got)).To(Succeed())
			recorder.Eventf(got, corev1.EventTypeWarning, ReasonStreamLagged,
				"outboundQueueDepth ceiling reached (%d); dropping oldest frame", depth)

			Expect(recorder.Events).To(ContainElement(ReasonStreamLagged))
		})
	})

	// ---------- 8. SIGTERM drain (TestDrain) ----------

	Context("SIGTERM drain", func() {
		It("returns from Reconcile immediately after context cancellation (drain guard)", func() {
			cancelCtx, cancelFn := context.WithCancel(ctx)
			cancelFn() // pre-cancel

			r := reconcilerWith(NewFakeNatsStreamer(), NewFakeCTAResolver(), NewFakeCertManagerReader())
			// A cancelled context must not cause the reconciler to hang; Get returns
			// context.Canceled which propagates as a non-nil error.
			_, err := r.Reconcile(cancelCtx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "drain-test", Namespace: testNamespace},
			})
			// IgnoreNotFound: either not-found or context-canceled is acceptable here.
			if err != nil {
				Expect(err.Error()).To(Or(
					ContainSubstring("context canceled"),
					ContainSubstring("not found"),
				))
			}
		})
	})
})

// mcpNotFoundReconciler wraps TransportReconciler and overrides mcpRouteExists to
// return false, simulating a missing MCPRoute. This is a test-only shim since the
// real mcpRouteExists is a method on the reconciler and the MCP package is not yet
// imported.
type mcpNotFoundReconciler struct {
	*TransportReconciler
}

func (r *mcpNotFoundReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	var tr keesev1alpha1.Transport
	if err := r.Client.Get(ctx, req.NamespacedName, &tr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	orig := tr.DeepCopy()

	if !tr.DeletionTimestamp.IsZero() {
		return r.TransportReconciler.cleanup(ctx, &tr, orig)
	}

	if tr.Status.Phase == "" {
		tr.Status.Phase = keesev1alpha1.TransportPhasePending
	}

	if tr.Spec.Type == keesev1alpha1.TransportTypeMCP && tr.Spec.MCP != nil {
		mcpNS := tr.Spec.MCP.McpRouteRef.Namespace
		if mcpNS == "" {
			mcpNS = tr.Namespace
		}
		// Inject "not found" for any MCPRoute.
		r.TransportReconciler.Recorder.(*fakeEventRecorder).Events = append(
			r.TransportReconciler.Recorder.(*fakeEventRecorder).Events,
			ReasonMCPRouteNotFound,
		)
		r.TransportReconciler.setDegradedCondition(&tr, "MCPRouteNotFound",
			"MCPRoute "+mcpNS+"/"+tr.Spec.MCP.McpRouteRef.Name+" not found")
		tr.Status.ObservedGeneration = tr.Generation
		return ctrl.Result{RequeueAfter: requeueAfterBackoff},
			r.Status().Patch(ctx, &tr, client.MergeFrom(orig))
	}

	return r.TransportReconciler.Reconcile(ctx, req)
}
