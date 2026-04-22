//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package workflow

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	workflowv1alpha1 "github.com/keese-ai/keese/api/workflow/v1alpha1"
)

const (
	testRunNamespace = "default"
	wfrPollInterval  = 100 * time.Millisecond
	wfrPollTimeout   = 10 * time.Second
)

// minimalWorkflowRun returns a minimal valid WorkflowRun for test use.
func minimalWorkflowRun(name, workflowName string) *workflowv1alpha1.WorkflowRun {
	return &workflowv1alpha1.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testRunNamespace,
		},
		Spec: workflowv1alpha1.WorkflowRunSpec{
			WorkspaceRef: workflowv1alpha1.LocalObjectReference{Name: "ws-test"},
			WorkflowRef:  workflowv1alpha1.LocalObjectReference{Name: workflowName},
			RetryBudget:  10,
		},
	}
}

// setupWorkflow creates a Workflow with ready status for WorkflowRun tests.
func setupWorkflow(ctx context.Context, name string, argo *FakeArgoProjector, rebac *FakeRebacWriter) *workflowv1alpha1.Workflow {
	wf := minimalWorkflow(name)
	Expect(k8sClient.Create(ctx, wf)).To(Succeed())

	wfr := &WorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Argo: argo, Rebac: rebac}
	key := types.NamespacedName{Name: name, Namespace: testWorkflowNamespace}
	// reconcile 1: finalizer
	_, _ = wfr.Reconcile(ctx, requestFor(key))
	// reconcile 2: project
	_, _ = wfr.Reconcile(ctx, requestFor(key))

	return wf
}

// wfrController creates a fresh WorkflowRunReconciler for each test.
func wfrController(argo *FakeArgoProjector, nats *FakeNatsStreamProvisioner, natsDel *FakeNatsStreamDeleter,
	rebac *FakeRebacWriter, cta *FakeCTAResolver) *WorkflowRunReconciler {
	return &WorkflowRunReconciler{
		Client:      k8sClient,
		Scheme:      k8sClient.Scheme(),
		Argo:        argo,
		Nats:        nats,
		NatsDeleter: natsDel,
		Rebac:       rebac,
		CTA:         cta,
	}
}

// reconcileRunN drives n reconcile calls for a WorkflowRun, re-fetching between.
func reconcileRunN(ctx context.Context, r *WorkflowRunReconciler, key types.NamespacedName, n int) error {
	for i := 0; i < n; i++ {
		_, err := r.Reconcile(ctx, requestFor(key))
		if err != nil {
			return err
		}
	}
	return nil
}

var _ = Describe("WorkflowRun Controller", func() {
	Describe("Idempotency", func() {
		It("converges in ≤3 reconciles with no spec change", func() {
			argo, nats, natsDel, rebac, cta := newFakes()

			wf := setupWorkflow(ctx, "wf-for-idem-run", argo, rebac)
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.Workflow{
					ObjectMeta: metav1.ObjectMeta{Name: wf.Name, Namespace: wf.Namespace},
				})
			}()

			wfr := minimalWorkflowRun("wfr-idem", wf.Name)
			Expect(k8sClient.Create(ctx, wfr)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.WorkflowRun{
					ObjectMeta: metav1.ObjectMeta{Name: wfr.Name, Namespace: wfr.Namespace},
				})
			}()

			key := types.NamespacedName{Name: wfr.Name, Namespace: wfr.Namespace}
			r := wfrController(argo, nats, natsDel, rebac, cta)

			// 3 reconciles.
			for i := 0; i < 3; i++ {
				_, err := r.Reconcile(ctx, requestFor(key))
				Expect(err).NotTo(HaveOccurred())
			}

			var fetched workflowv1alpha1.WorkflowRun
			Expect(k8sClient.Get(ctx, key, &fetched)).To(Succeed())
			Expect(fetched.Status.Phase).NotTo(Equal(workflowv1alpha1.WorkflowRunPhaseError))
			Expect(fetched.Finalizers).To(ContainElement(workflowRunFinalizer))
		})
	})

	Describe("NATS stream provisioning", func() {
		It("provisions a NATS stream on create and deletes it on finalizer cleanup", func() {
			argo, nats, natsDel, rebac, cta := newFakes()
			wf := setupWorkflow(ctx, "wf-for-nats-run", argo, rebac)
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.Workflow{
					ObjectMeta: metav1.ObjectMeta{Name: wf.Name, Namespace: wf.Namespace},
				})
			}()

			wfr := minimalWorkflowRun("wfr-nats", wf.Name)
			Expect(k8sClient.Create(ctx, wfr)).To(Succeed())

			key := types.NamespacedName{Name: wfr.Name, Namespace: wfr.Namespace}
			r := wfrController(argo, nats, natsDel, rebac, cta)

			// Reconcile 1: add finalizer.
			_, err := r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())
			// Reconcile 2: provision NATS stream + project Argo.
			_, err = r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())
			// Reconcile 3: idempotent (stream already provisioned because ArgoWorkflowName set).
			_, err = r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			// Exactly one provisioning call (reconcile 2).
			Expect(len(nats.Provisioned)).To(Equal(1))
			Expect(nats.Provisioned[0].Replicas).To(Equal(natsStreamReplicas))
			Expect(nats.Provisioned[0].Name).To(ContainSubstring("keese-tenant-"))

			// Delete the WorkflowRun.
			Expect(k8sClient.Delete(ctx, wfr)).To(Succeed())
			_, err = r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			// NATS stream should have been deleted.
			Expect(len(natsDel.Deleted)).To(Equal(1))
			Expect(natsDel.Deleted[0]).To(ContainSubstring("keese-tenant-"))

			// Workflow object should be gone.
			Eventually(func() bool {
				err := k8sClient.Get(ctx, key, &workflowv1alpha1.WorkflowRun{})
				return err != nil
			}, wfrPollTimeout, wfrPollInterval).Should(BeTrue())
		})
	})

	Describe("SA audience injection", func() {
		It("injects keese-wf-<uid> audience into Argo Workflow projection", func() {
			argo, nats, natsDel, rebac, cta := newFakes()
			wf := setupWorkflow(ctx, "wf-for-aud-run", argo, rebac)
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.Workflow{
					ObjectMeta: metav1.ObjectMeta{Name: wf.Name, Namespace: wf.Namespace},
				})
			}()

			wfr := minimalWorkflowRun("wfr-audience", wf.Name)
			Expect(k8sClient.Create(ctx, wfr)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.WorkflowRun{
					ObjectMeta: metav1.ObjectMeta{Name: wfr.Name, Namespace: wfr.Namespace},
				})
			}()

			key := types.NamespacedName{Name: wfr.Name, Namespace: wfr.Namespace}
			r := wfrController(argo, nats, natsDel, rebac, cta)

			_, _ = r.Reconcile(ctx, requestFor(key)) // finalizer
			_, err := r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			// The Argo Workflow should have been projected with a keese-wf- audience.
			Expect(len(argo.ProjectedWorkflows)).To(BeNumerically(">=", 1))
			projected := argo.ProjectedWorkflows[len(argo.ProjectedWorkflows)-1]
			Expect(projected.ServiceAccountAudience).To(HavePrefix("keese-wf-"))
		})
	})

	Describe("Argo phase back-projection", func() {
		It("mirrors Argo Workflow phase to WorkflowRun.status", func() {
			argo, nats, natsDel, rebac, cta := newFakes()
			wf := setupWorkflow(ctx, "wf-for-phase-run", argo, rebac)
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.Workflow{
					ObjectMeta: metav1.ObjectMeta{Name: wf.Name, Namespace: wf.Namespace},
				})
			}()

			wfr := minimalWorkflowRun("wfr-phase", wf.Name)
			Expect(k8sClient.Create(ctx, wfr)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.WorkflowRun{
					ObjectMeta: metav1.ObjectMeta{Name: wfr.Name, Namespace: wfr.Namespace},
				})
			}()

			key := types.NamespacedName{Name: wfr.Name, Namespace: wfr.Namespace}
			r := wfrController(argo, nats, natsDel, rebac, cta)

			// Reconcile 1: finalizer.
			_, _ = r.Reconcile(ctx, requestFor(key))
			// Reconcile 2: project Argo Workflow.
			_, err := r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			// Determine the Argo name.
			var fetched workflowv1alpha1.WorkflowRun
			Expect(k8sClient.Get(ctx, key, &fetched)).To(Succeed())
			argoName := fetched.Status.ArgoWorkflowName
			if argoName == "" {
				argoName = fmt.Sprintf("keese-wfr-%s", wfr.Name)
			}

			// Inject Running status into the fake.
			argo.StatusByName[argoName] = &ArgoWorkflowStatus{
				Phase:     "Running",
				StartedAt: &metav1.Time{Time: time.Now()},
			}

			// Reconcile 3: back-project Running phase.
			_, err = r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, key, &fetched)).To(Succeed())
			Expect(fetched.Status.ArgoPhase).To(Equal("Running"))
			Expect(fetched.Status.Phase).To(Equal(workflowv1alpha1.WorkflowRunPhaseRunning))
		})
	})

	Describe("CrossTenantAgreement admission check", func() {
		It("keeps phase Pending when CTA is missing", func() {
			argo, nats, natsDel, rebac, _ := newFakes()
			cta := &FakeCTAResolver{
				ReturnPeers: []CrossTenantPeer{{TransportRefName: "transport-a2a", PeerWorkspaceRef: "peer-ns"}},
				MissingPeer: &CrossTenantPeer{TransportRefName: "transport-a2a", PeerWorkspaceRef: "peer-ns"},
			}

			wf := setupWorkflow(ctx, "wf-for-cta-missing", argo, rebac)
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.Workflow{
					ObjectMeta: metav1.ObjectMeta{Name: wf.Name, Namespace: wf.Namespace},
				})
			}()

			wfr := minimalWorkflowRun("wfr-cta-missing", wf.Name)
			Expect(k8sClient.Create(ctx, wfr)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.WorkflowRun{
					ObjectMeta: metav1.ObjectMeta{Name: wfr.Name, Namespace: wfr.Namespace},
				})
			}()

			key := types.NamespacedName{Name: wfr.Name, Namespace: wfr.Namespace}
			r := wfrController(argo, nats, natsDel, rebac, cta)

			_, _ = r.Reconcile(ctx, requestFor(key)) // finalizer
			_, err := r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			var fetched workflowv1alpha1.WorkflowRun
			Expect(k8sClient.Get(ctx, key, &fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(workflowv1alpha1.WorkflowRunPhasePending))
		})

		It("advances phase when CTA is Approved", func() {
			argo, nats, natsDel, rebac, _ := newFakes()
			cta := &FakeCTAResolver{
				ReturnPeers: []CrossTenantPeer{{TransportRefName: "transport-a2a", PeerWorkspaceRef: "peer-ns"}},
				MissingPeer: nil, // all approved
			}

			wf := setupWorkflow(ctx, "wf-for-cta-approved", argo, rebac)
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.Workflow{
					ObjectMeta: metav1.ObjectMeta{Name: wf.Name, Namespace: wf.Namespace},
				})
			}()

			wfr := minimalWorkflowRun("wfr-cta-approved", wf.Name)
			Expect(k8sClient.Create(ctx, wfr)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.WorkflowRun{
					ObjectMeta: metav1.ObjectMeta{Name: wfr.Name, Namespace: wfr.Namespace},
				})
			}()

			key := types.NamespacedName{Name: wfr.Name, Namespace: wfr.Namespace}
			r := wfrController(argo, nats, natsDel, rebac, cta)

			_, _ = r.Reconcile(ctx, requestFor(key)) // finalizer
			_, err := r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			var fetched workflowv1alpha1.WorkflowRun
			Expect(k8sClient.Get(ctx, key, &fetched)).To(Succeed())
			// Phase should be past Pending.
			Expect(fetched.Status.Phase).NotTo(Equal(workflowv1alpha1.WorkflowRunPhasePending))
			Expect(fetched.Status.Phase).NotTo(BeEmpty())
		})
	})

	Describe("Retry budget composition", func() {
		It("uses min(wf.defaultRetryBudget, wfr.retryBudget) in Argo projection", func() {
			argo, nats, natsDel, rebac, cta := newFakes()

			wf := minimalWorkflow("wf-for-retry-budget")
			wf.Spec.DefaultRetryBudget = &workflowv1alpha1.RetryBudget{Limit: 3}
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.Workflow{
					ObjectMeta: metav1.ObjectMeta{Name: wf.Name, Namespace: wf.Namespace},
				})
			}()

			// Prime workflow status.
			wfReconciler := &WorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Argo: argo, Rebac: rebac}
			wfKey := types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace}
			_, _ = wfReconciler.Reconcile(ctx, requestFor(wfKey))
			_, _ = wfReconciler.Reconcile(ctx, requestFor(wfKey))

			// Run with budget 10 — should be capped to 3.
			wfr := minimalWorkflowRun("wfr-retry", wf.Name)
			wfr.Spec.RetryBudget = 10
			Expect(k8sClient.Create(ctx, wfr)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.WorkflowRun{
					ObjectMeta: metav1.ObjectMeta{Name: wfr.Name, Namespace: wfr.Namespace},
				})
			}()

			key := types.NamespacedName{Name: wfr.Name, Namespace: wfr.Namespace}
			r := wfrController(argo, nats, natsDel, rebac, cta)

			_, _ = r.Reconcile(ctx, requestFor(key)) // finalizer
			_, err := r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			// Verify retry limit was capped.
			Expect(len(argo.ProjectedWorkflows)).To(BeNumerically(">=", 1))
			projected := argo.ProjectedWorkflows[len(argo.ProjectedWorkflows)-1]
			Expect(projected.RetryLimit).To(Equal(int32(3)))
		})
	})

	Describe("ConcurrencyPolicy: Forbid", func() {
		It("keeps second WorkflowRun in Pending phase", func() {
			argo, nats, natsDel, rebac, cta := newFakes()

			wf := minimalWorkflow("wf-for-concur-forbid")
			wf.Spec.ConcurrencyPolicy = workflowv1alpha1.ConcurrencyPolicyForbid
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.Workflow{
					ObjectMeta: metav1.ObjectMeta{Name: wf.Name, Namespace: wf.Namespace},
				})
			}()

			// Prime workflow status.
			wfReconciler := &WorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Argo: argo, Rebac: rebac}
			wfKey := types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace}
			_, _ = wfReconciler.Reconcile(ctx, requestFor(wfKey))
			_, _ = wfReconciler.Reconcile(ctx, requestFor(wfKey))

			// Create run1 and mark it Running.
			run1 := minimalWorkflowRun("wfr-concur-run1", wf.Name)
			Expect(k8sClient.Create(ctx, run1)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.WorkflowRun{
					ObjectMeta: metav1.ObjectMeta{Name: run1.Name, Namespace: run1.Namespace},
				})
			}()

			run1Fetched := &workflowv1alpha1.WorkflowRun{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: run1.Name, Namespace: run1.Namespace}, run1Fetched)).To(Succeed())
			run1Fetched.Status.Phase = workflowv1alpha1.WorkflowRunPhaseRunning
			Expect(k8sClient.Status().Update(ctx, run1Fetched)).To(Succeed())

			// Create run2.
			run2 := minimalWorkflowRun("wfr-concur-run2", wf.Name)
			Expect(k8sClient.Create(ctx, run2)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.WorkflowRun{
					ObjectMeta: metav1.ObjectMeta{Name: run2.Name, Namespace: run2.Namespace},
				})
			}()

			key2 := types.NamespacedName{Name: run2.Name, Namespace: run2.Namespace}
			r := wfrController(argo, nats, natsDel, rebac, cta)

			_, _ = r.Reconcile(ctx, requestFor(key2)) // finalizer
			_, err := r.Reconcile(ctx, requestFor(key2))
			Expect(err).NotTo(HaveOccurred())

			var fetched workflowv1alpha1.WorkflowRun
			Expect(k8sClient.Get(ctx, key2, &fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(workflowv1alpha1.WorkflowRunPhasePending))
		})
	})

	Describe("Supervision context annotation propagation", func() {
		It("propagates supervision labels to the Argo Workflow projection", func() {
			argo, nats, natsDel, rebac, cta := newFakes()
			wf := setupWorkflow(ctx, "wf-for-supervision", argo, rebac)
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.Workflow{
					ObjectMeta: metav1.ObjectMeta{Name: wf.Name, Namespace: wf.Namespace},
				})
			}()

			wfr := minimalWorkflowRun("wfr-supervision", wf.Name)
			wfr.Spec.SupervisionContext = &workflowv1alpha1.SupervisionContext{
				RequireApproval: true,
				ReviewerRef:     "platform-team",
				MaxWaitSeconds:  600,
			}
			Expect(k8sClient.Create(ctx, wfr)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.WorkflowRun{
					ObjectMeta: metav1.ObjectMeta{Name: wfr.Name, Namespace: wfr.Namespace},
				})
			}()

			key := types.NamespacedName{Name: wfr.Name, Namespace: wfr.Namespace}
			r := wfrController(argo, nats, natsDel, rebac, cta)

			_, _ = r.Reconcile(ctx, requestFor(key)) // finalizer
			_, err := r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			Expect(len(argo.ProjectedWorkflows)).To(BeNumerically(">=", 1))
			projected := argo.ProjectedWorkflows[len(argo.ProjectedWorkflows)-1]
			Expect(projected.Labels["keese.ai/supervision-context"]).To(Equal("true"))
			Expect(projected.Labels["keese.ai/supervisor-ref"]).To(Equal("platform-team"))
		})
	})
})
