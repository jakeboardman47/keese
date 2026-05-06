// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"testing"

	argov1alpha1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// TestBuildArgoWorkflowTemplate_FieldShape asserts that buildArgoWorkflowTemplate
// produces an object with the correct TypeMeta, labels, owner-ref, and template
// structure — without hitting an API server.
func TestBuildArgoWorkflowTemplate_FieldShape(t *testing.T) {
	wf := &keesev1alpha1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-wf",
			Namespace: "default",
			UID:       types.UID("aaa-bbb-ccc"),
		},
		Spec: keesev1alpha1.WorkflowSpec{
			Entrypoint: "run",
			Templates: []keesev1alpha1.WorkflowTemplateStep{
				{Name: "step-a", Image: "alpine:3.18", RetryLimit: 2},
				{Name: "step-b", Image: "busybox:1.36"},
			},
		},
	}
	// Populate GVK so owner-ref is non-empty (normally set by the scheme on fetch).
	wf.APIVersion = "workflow.operator.keese.ai/v1alpha1"
	wf.Kind = "Workflow"

	name := argoTemplateName(wf)
	got := buildArgoWorkflowTemplate(wf, name)

	// TypeMeta must be set for SSA to route correctly.
	if got.APIVersion != "argoproj.io/v1alpha1" {
		t.Errorf("APIVersion = %q, want %q", got.APIVersion, "argoproj.io/v1alpha1")
	}
	if got.Kind != "WorkflowTemplate" {
		t.Errorf("Kind = %q, want %q", got.Kind, "WorkflowTemplate")
	}

	// Name follows convention.
	if got.Name != "argo-wft-my-wf" {
		t.Errorf("Name = %q, want argo-wft-my-wf", got.Name)
	}

	// Namespace preserved.
	if got.Namespace != "default" {
		t.Errorf("Namespace = %q, want default", got.Namespace)
	}

	// Label keese.ai/managed is set.
	if got.Labels["keese.ai/managed"] != "true" {
		t.Errorf("labels[keese.ai/managed] = %q, want true", got.Labels["keese.ai/managed"])
	}
	if got.Labels["keese.ai/workflow"] != "my-wf" {
		t.Errorf("labels[keese.ai/workflow] = %q, want my-wf", got.Labels["keese.ai/workflow"])
	}

	// Owner-ref is set to the keese Workflow.
	if len(got.OwnerReferences) != 1 {
		t.Fatalf("OwnerReferences len = %d, want 1", len(got.OwnerReferences))
	}
	or := got.OwnerReferences[0]
	if or.Kind != "Workflow" {
		t.Errorf("ownerRef.Kind = %q, want Workflow", or.Kind)
	}
	if or.UID != "aaa-bbb-ccc" {
		t.Errorf("ownerRef.UID = %q, want aaa-bbb-ccc", or.UID)
	}
	if or.Controller == nil || !*or.Controller {
		t.Error("ownerRef.Controller not set to true")
	}

	// Entrypoint propagated.
	if got.Spec.Entrypoint != "run" {
		t.Errorf("Entrypoint = %q, want run", got.Spec.Entrypoint)
	}

	// Should have 3 templates: entrypoint steps wrapper + 2 container templates.
	if len(got.Spec.Templates) != 3 {
		t.Fatalf("Templates len = %d, want 3 (1 steps + 2 containers)", len(got.Spec.Templates))
	}

	// First template is the steps entrypoint.
	entryTmpl := got.Spec.Templates[0]
	if entryTmpl.Name != "run" {
		t.Errorf("templates[0].Name = %q, want run", entryTmpl.Name)
	}
	if len(entryTmpl.Steps) != 2 {
		t.Errorf("entrypoint steps len = %d, want 2", len(entryTmpl.Steps))
	}

	// Container templates retain retry strategy where set.
	var stepA *argov1alpha1.Template
	for i := range got.Spec.Templates {
		if got.Spec.Templates[i].Name == "step-a" {
			stepA = &got.Spec.Templates[i]
		}
	}
	if stepA == nil {
		t.Fatal("step-a template not found")
	}
	if stepA.RetryStrategy == nil {
		t.Fatal("step-a RetryStrategy is nil, want set")
	}
	if stepA.RetryStrategy.Limit == nil || stepA.RetryStrategy.Limit.IntValue() != 2 {
		t.Errorf("step-a RetryStrategy.Limit = %v, want 2", stepA.RetryStrategy.Limit)
	}
}

// TestBuildArgoWorkflow_FieldShape asserts that buildArgoWorkflow produces an object
// with correct TypeMeta, WorkflowTemplateRef, Arguments, and optional fields.
func TestBuildArgoWorkflow_FieldShape(t *testing.T) {
	timeout := metav1.Duration{Duration: 30 * 60 * 1e9} // 30m in nanoseconds
	spec := ArgoWorkflowSpec{
		Name:                    "keese-wfr-run1",
		Namespace:               "tenant-ns",
		WorkflowTemplateRefName: "argo-wft-my-wf",
		Parameters: []keesev1alpha1.WorkflowRunParameter{
			{Name: "env", Value: "prod"},
			{Name: "count", Value: "5"},
		},
		Timeout:    &timeout,
		RetryLimit: 3,
		Labels:     map[string]string{"keese.ai/managed": "true"},
	}

	got := buildArgoWorkflow(spec)

	if got.APIVersion != "argoproj.io/v1alpha1" {
		t.Errorf("APIVersion = %q", got.APIVersion)
	}
	if got.Kind != "Workflow" {
		t.Errorf("Kind = %q", got.Kind)
	}
	if got.Name != "keese-wfr-run1" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Namespace != "tenant-ns" {
		t.Errorf("Namespace = %q", got.Namespace)
	}

	// WorkflowTemplateRef set correctly.
	if got.Spec.WorkflowTemplateRef == nil {
		t.Fatal("WorkflowTemplateRef is nil")
	}
	if got.Spec.WorkflowTemplateRef.Name != "argo-wft-my-wf" {
		t.Errorf("WorkflowTemplateRef.Name = %q", got.Spec.WorkflowTemplateRef.Name)
	}

	// Arguments carry both parameters.
	if len(got.Spec.Arguments.Parameters) != 2 {
		t.Fatalf("Arguments.Parameters len = %d, want 2", len(got.Spec.Arguments.Parameters))
	}
	if got.Spec.Arguments.Parameters[0].Name != "env" {
		t.Errorf("param[0].Name = %q", got.Spec.Arguments.Parameters[0].Name)
	}
	if got.Spec.Arguments.Parameters[0].Value == nil || got.Spec.Arguments.Parameters[0].Value.String() != "prod" {
		t.Errorf("param[0].Value = %v", got.Spec.Arguments.Parameters[0].Value)
	}

	// Timeout → ActiveDeadlineSeconds.
	if got.Spec.ActiveDeadlineSeconds == nil {
		t.Fatal("ActiveDeadlineSeconds is nil")
	}
	if *got.Spec.ActiveDeadlineSeconds != 1800 {
		t.Errorf("ActiveDeadlineSeconds = %d, want 1800", *got.Spec.ActiveDeadlineSeconds)
	}

	// RetryStrategy set.
	if got.Spec.RetryStrategy == nil {
		t.Fatal("RetryStrategy is nil")
	}
	if got.Spec.RetryStrategy.Limit == nil || got.Spec.RetryStrategy.Limit.IntValue() != 3 {
		t.Errorf("RetryStrategy.Limit = %v", got.Spec.RetryStrategy.Limit)
	}
}

// TestArgoStatusToKeese_PhaseAndNodes exercises argoStatusToKeese with a realistic
// Argo Workflow status, asserting all keese mirror fields are populated correctly.
func TestArgoStatusToKeese_PhaseAndNodes(t *testing.T) {
	started := metav1.Now()
	finished := metav1.Now()

	argoWf := &argov1alpha1.Workflow{
		Status: argov1alpha1.WorkflowStatus{
			Phase:      argov1alpha1.WorkflowSucceeded,
			StartedAt:  started,
			FinishedAt: finished,
			Nodes: argov1alpha1.Nodes{
				"node-1": argov1alpha1.NodeStatus{
					ID:          "node-1",
					Phase:       argov1alpha1.NodeSucceeded,
					DisplayName: "step-a",
					Message:     "ok",
					StartedAt:   started,
					FinishedAt:  finished,
				},
			},
			Outputs: &argov1alpha1.Outputs{
				Artifacts: argov1alpha1.Artifacts{
					{Name: "result", Path: "/tmp/out"},
				},
			},
		},
	}

	got := argoStatusToKeese(argoWf)

	if got.Phase != "Succeeded" {
		t.Errorf("Phase = %q, want Succeeded", got.Phase)
	}
	if got.StartedAt == nil {
		t.Error("StartedAt is nil")
	}
	if got.FinishedAt == nil {
		t.Error("FinishedAt is nil")
	}

	if len(got.Nodes) != 1 {
		t.Fatalf("Nodes len = %d, want 1", len(got.Nodes))
	}
	n := got.Nodes[0]
	if n.ID != "node-1" {
		t.Errorf("node.ID = %q", n.ID)
	}
	if n.Phase != "Succeeded" {
		t.Errorf("node.Phase = %q", n.Phase)
	}
	if n.DisplayName != "step-a" {
		t.Errorf("node.DisplayName = %q", n.DisplayName)
	}

	if len(got.Artifacts) != 1 {
		t.Fatalf("Artifacts len = %d, want 1", len(got.Artifacts))
	}
	a := got.Artifacts[0]
	if a.Name != "result" {
		t.Errorf("artifact.Name = %q", a.Name)
	}
	if a.Path != "/tmp/out" {
		t.Errorf("artifact.Path = %q", a.Path)
	}
}
