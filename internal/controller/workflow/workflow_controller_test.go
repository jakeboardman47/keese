//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	workflowv1alpha1 "github.com/keese-ai/keese/api/workflow/v1alpha1"
)

const (
	testWorkflowNamespace = "default"
	pollInterval          = 100 * time.Millisecond
	pollTimeout           = 10 * time.Second
)

// minimalWorkflow returns a minimal valid Workflow for test use.
func minimalWorkflow(name string) *workflowv1alpha1.Workflow {
	return &workflowv1alpha1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testWorkflowNamespace,
		},
		Spec: workflowv1alpha1.WorkflowSpec{
			Entrypoint: "step-one",
			Templates: []workflowv1alpha1.WorkflowTemplateStep{
				{
					Name:  "step-one",
					Image: "alpine:3.18",
				},
			},
		},
	}
}

// requestFor builds a reconcile.Request from a NamespacedName.
func requestFor(key types.NamespacedName) ctrl.Request {
	return ctrl.Request{NamespacedName: key}
}

// reconcileN drives n reconcile calls on a fresh-fetched key each time.
// It re-fetches between calls but passes the fresh object's current resourceVersion implicitly
// via the in-cluster API (the reconciler re-fetches inside Reconcile).
func reconcileN(ctx context.Context, r *WorkflowReconciler, key types.NamespacedName, n int) {
	for i := 0; i < n; i++ {
		_, err := r.Reconcile(ctx, requestFor(key))
		if err != nil {
			// Conflicts on finalizer add are transient; wait briefly and retry.
			time.Sleep(50 * time.Millisecond)
			_, _ = r.Reconcile(ctx, requestFor(key))
		}
	}
}

var _ = Describe("Workflow Controller", func() {
	Describe("Idempotency", func() {
		It("converges in ≤3 reconciles with no spec change", func() {
			argo, _, _, rebac, _ := newFakes()
			r := &WorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Argo: argo, Rebac: rebac}

			wf := minimalWorkflow("wf-idempotent")
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.Workflow{
					ObjectMeta: metav1.ObjectMeta{Name: wf.Name, Namespace: wf.Namespace},
				})
			}()

			key := types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace}

			// Reconcile 1: adds finalizer, returns Requeue.
			_, err := r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			// Reconcile 2: projects WorkflowTemplate + sets status.
			_, err = r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			// Reconcile 3: idempotent — no change.
			_, err = r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			var fetched workflowv1alpha1.Workflow
			Expect(k8sClient.Get(ctx, key, &fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(workflowv1alpha1.WorkflowPhaseReady))
			// At least 2 ProjectWorkflowTemplate calls (reconcile 2 + 3).
			Expect(len(argo.ProjectedTemplates)).To(BeNumerically(">=", 2))
		})
	})

	Describe("Argo WorkflowTemplate projection", func() {
		It("sets WorkflowTemplateRef in status after projection", func() {
			argo, _, _, rebac, _ := newFakes()
			argo.ReturnTemplateName = "argo-wft-wf-template-proj"
			r := &WorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Argo: argo, Rebac: rebac}

			wf := minimalWorkflow("wf-template-proj")
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.Workflow{
					ObjectMeta: metav1.ObjectMeta{Name: wf.Name, Namespace: wf.Namespace},
				})
			}()

			key := types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace}
			// reconcile 1: finalizer add
			_, _ = r.Reconcile(ctx, requestFor(key))
			// reconcile 2: projection
			_, err := r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			var fetched workflowv1alpha1.Workflow
			Expect(k8sClient.Get(ctx, key, &fetched)).To(Succeed())
			Expect(fetched.Status.WorkflowTemplateRef).To(Equal("argo-wft-wf-template-proj"))
			Expect(len(argo.ProjectedTemplates)).To(BeNumerically(">=", 1))
		})
	})

	Describe("Trigger projection", func() {
		DescribeTable("reconciles trigger types without error",
			func(triggerType workflowv1alpha1.TriggerType, trigger workflowv1alpha1.WorkflowTrigger) {
				argo, _, _, rebac, _ := newFakes()
				r := &WorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Argo: argo, Rebac: rebac}

				name := fmt.Sprintf("wf-trig-%s", strings.ToLower(string(triggerType)))
				wf := minimalWorkflow(name)
				wf.Spec.Triggers = []workflowv1alpha1.WorkflowTrigger{trigger}
				Expect(k8sClient.Create(ctx, wf)).To(Succeed())
				defer func() {
					_ = k8sClient.Delete(ctx, &workflowv1alpha1.Workflow{
						ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testWorkflowNamespace},
					})
					// Ensure the name variable is captured (not the loop var) — it's already captured by closure.
				}()

				key := types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace}
				_, _ = r.Reconcile(ctx, requestFor(key)) // finalizer
				_, err := r.Reconcile(ctx, requestFor(key))
				Expect(err).NotTo(HaveOccurred())

				var fetched workflowv1alpha1.Workflow
				Expect(k8sClient.Get(ctx, key, &fetched)).To(Succeed())
				Expect(fetched.Status.Phase).To(Equal(workflowv1alpha1.WorkflowPhaseReady))
			},
			Entry("Cron trigger",
				workflowv1alpha1.TriggerTypeCron,
				workflowv1alpha1.WorkflowTrigger{
					Type: workflowv1alpha1.TriggerTypeCron,
					Cron: &workflowv1alpha1.CronTrigger{Schedule: "0 * * * *"},
				},
			),
			Entry("KnativeTrigger trigger",
				workflowv1alpha1.TriggerTypeKnativeTrigger,
				workflowv1alpha1.WorkflowTrigger{
					Type:           workflowv1alpha1.TriggerTypeKnativeTrigger,
					KnativeTrigger: &workflowv1alpha1.KnativeTriggerConfig{BrokerRef: "default-broker"},
				},
			),
			Entry("HTTPWebhook trigger",
				workflowv1alpha1.TriggerTypeHTTPWebhook,
				workflowv1alpha1.WorkflowTrigger{
					Type:        workflowv1alpha1.TriggerTypeHTTPWebhook,
					HTTPWebhook: &workflowv1alpha1.HTTPWebhookConfig{Path: "/trigger"},
				},
			),
		)
	})

	Describe("Finalizer cascade blocks active WorkflowRuns", func() {
		It("delays deletion while WorkflowRuns are non-terminal", func() {
			argo, _, _, rebac, _ := newFakes()
			r := &WorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Argo: argo, Rebac: rebac}

			wf := minimalWorkflow("wf-cascade-block")
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			key := types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace}

			// Reconcile 1: add finalizer.
			_, _ = r.Reconcile(ctx, requestFor(key))
			// Reconcile 2: project.
			_, _ = r.Reconcile(ctx, requestFor(key))

			// Create an in-flight WorkflowRun.
			wfr := &workflowv1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "run-blocking-cascade",
					Namespace: testWorkflowNamespace,
				},
				Spec: workflowv1alpha1.WorkflowRunSpec{
					WorkspaceRef: workflowv1alpha1.LocalObjectReference{Name: "ws-test"},
					WorkflowRef:  workflowv1alpha1.LocalObjectReference{Name: wf.Name},
					RetryBudget:  5,
				},
			}
			Expect(k8sClient.Create(ctx, wfr)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, wfr) }()

			wfrCopy := wfr.DeepCopy()
			wfrCopy.Status.Phase = workflowv1alpha1.WorkflowRunPhaseRunning
			Expect(k8sClient.Status().Update(ctx, wfrCopy)).To(Succeed())

			// Delete the Workflow.
			Expect(k8sClient.Delete(ctx, wf)).To(Succeed())

			// Reconcile on deleted object.
			_, err := r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			// Should be Deleting (blocked by active run).
			var fetched workflowv1alpha1.Workflow
			Expect(k8sClient.Get(ctx, key, &fetched)).To(Succeed())
			Expect(fetched.Status.Phase).To(Equal(workflowv1alpha1.WorkflowPhaseDeleting))

			// Argo delete should NOT have been called yet.
			Expect(len(argo.DeletedTemplates)).To(Equal(0))
		})
	})

	Describe("ReBAC tuple write on create", func() {
		It("calls WriteWorkflowOwner and records TupleCount in status", func() {
			argo, _, _, rebac, _ := newFakes()
			rebac.TupleCount = 3
			r := &WorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Argo: argo, Rebac: rebac}

			wf := minimalWorkflow("wf-rebac-write")
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, &workflowv1alpha1.Workflow{
					ObjectMeta: metav1.ObjectMeta{Name: wf.Name, Namespace: wf.Namespace},
				})
			}()

			key := types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace}
			_, _ = r.Reconcile(ctx, requestFor(key)) // finalizer
			_, err := r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			Expect(len(rebac.WrittenWorkflowTuples)).To(Equal(1))

			var fetched workflowv1alpha1.Workflow
			Expect(k8sClient.Get(ctx, key, &fetched)).To(Succeed())
			Expect(fetched.Status.TupleCount).To(Equal(int32(3)))
		})
	})

	Describe("Deletion cascades Argo WorkflowTemplate", func() {
		It("calls DeleteWorkflowTemplate when WorkflowRuns are all terminal", func() {
			argo, _, _, rebac, _ := newFakes()
			r := &WorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Argo: argo, Rebac: rebac}

			wf := minimalWorkflow("wf-delete-cascade")
			Expect(k8sClient.Create(ctx, wf)).To(Succeed())

			key := types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace}

			// Reconcile 1: finalizer; Reconcile 2: project.
			_, _ = r.Reconcile(ctx, requestFor(key))
			_, err := r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			// Delete (no active runs).
			Expect(k8sClient.Delete(ctx, wf)).To(Succeed())

			// Reconcile deletion.
			_, err = r.Reconcile(ctx, requestFor(key))
			Expect(err).NotTo(HaveOccurred())

			// Argo DeleteWorkflowTemplate should have been called.
			Expect(len(argo.DeletedTemplates)).To(Equal(1))

			// Workflow should be gone (finalizer removed).
			Eventually(func() bool {
				err := k8sClient.Get(ctx, key, &workflowv1alpha1.Workflow{})
				return err != nil
			}, pollTimeout, pollInterval).Should(BeTrue())
		})
	})
})
