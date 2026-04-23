// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package workflow

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	workflowv1alpha1 "github.com/keese-ai/keese/api/workflow/v1alpha1"
)

// ArgoWorkflowStatus mirrors the fields keese cares about from an Argo Workflow.
// TODO(spec-followup): Argo Workflow types are not directly imported here — real
// implementation will depend on argoproj.io/argo-workflows/pkg/apis/workflow/v1alpha1
// being added to go.mod. The Fake implementation uses these structs as stand-ins.
type ArgoWorkflowStatus struct {
	// Phase is the Argo workflow phase string (e.g. "Running", "Succeeded").
	Phase string

	// StartedAt is when the Argo Workflow started.
	StartedAt *metav1.Time

	// FinishedAt is when the Argo Workflow finished.
	FinishedAt *metav1.Time

	// Nodes mirrors Argo node statuses keyed by node ID.
	Nodes []ArgoNodeStatus

	// Artifacts are output artifacts produced by the workflow.
	Artifacts []ArgoArtifact
}

// ArgoNodeStatus mirrors a single Argo node.
type ArgoNodeStatus struct {
	ID          string
	Phase       string
	DisplayName string
	Message     string
	StartedAt   *metav1.Time
	FinishedAt  *metav1.Time
}

// ArgoArtifact mirrors a single Argo output artifact.
type ArgoArtifact struct {
	Name   string
	Path   string
	NodeID string
}

// ArgoWorkflowSpec describes the desired Argo Workflow to project.
type ArgoWorkflowSpec struct {
	// Name is the Argo Workflow object name.
	Name string

	// Namespace is the target namespace.
	Namespace string

	// WorkflowTemplateRefName is the Argo WorkflowTemplate to instantiate.
	WorkflowTemplateRefName string

	// Parameters are name/value pairs forwarded to the Argo Workflow.
	Parameters []workflowv1alpha1.WorkflowRunParameter

	// Timeout is the maximum duration.
	Timeout *metav1.Duration

	// Labels to apply to the projected Argo Workflow.
	Labels map[string]string

	// Annotations to apply to the projected Argo Workflow.
	Annotations map[string]string

	// ServiceAccountAudience is injected into projected SA tokens.
	// Convention: "keese-wf-<workflow-run-uid>"
	ServiceAccountAudience string

	// RetryLimit is the composed retry cap (min of step limit and run budget).
	RetryLimit int32
}

// ArgoProjector creates and watches Argo Workflow / WorkflowTemplate resources.
// Real Argo client wire-up is deferred to post-gate; tests use FakeArgoProjector.
// TODO(spec-followup): Real projection requires the Argo Workflows CRD installed in
// envtest and the argoproj.io client registered in the scheme. The Fake covers test
// assertions without network dependencies.
type ArgoProjector interface {
	// ProjectWorkflowTemplate creates or updates an Argo WorkflowTemplate
	// that mirrors the keese Workflow templates. Returns the projected name.
	ProjectWorkflowTemplate(ctx context.Context, wf *workflowv1alpha1.Workflow) (string, error)

	// DeleteWorkflowTemplate removes the Argo WorkflowTemplate for a Workflow.
	// Returns nil if already absent (idempotent).
	DeleteWorkflowTemplate(ctx context.Context, wf *workflowv1alpha1.Workflow) error

	// ProjectWorkflow creates or updates an Argo Workflow from the spec.
	// Returns the projected Argo Workflow name.
	ProjectWorkflow(ctx context.Context, spec ArgoWorkflowSpec) (string, error)

	// GetWorkflowStatus retrieves the current Argo Workflow status by name.
	GetWorkflowStatus(ctx context.Context, namespace, name string) (*ArgoWorkflowStatus, error)

	// DeleteWorkflow removes the Argo Workflow by name. Returns nil if absent.
	DeleteWorkflow(ctx context.Context, namespace, name string) error
}

// FakeArgoProjector is a test-only ArgoProjector.
// Kept for tests; see argo_client.go for the production SSA impl.
type FakeArgoProjector struct {
	// ProjectedTemplates accumulates ProjectWorkflowTemplate calls.
	ProjectedTemplates []*workflowv1alpha1.Workflow
	// DeletedTemplates accumulates DeleteWorkflowTemplate calls.
	DeletedTemplates []*workflowv1alpha1.Workflow
	// ProjectedWorkflows accumulates ProjectWorkflow calls.
	ProjectedWorkflows []ArgoWorkflowSpec
	// DeletedWorkflows accumulates DeleteWorkflow calls by (namespace, name).
	DeletedWorkflows []string
	// ReturnTemplateName overrides the name returned by ProjectWorkflowTemplate.
	ReturnTemplateName string
	// ReturnWorkflowName overrides the name returned by ProjectWorkflow.
	ReturnWorkflowName string
	// StatusByName is returned by GetWorkflowStatus keyed by name.
	StatusByName map[string]*ArgoWorkflowStatus
	// Err is returned by all calls when non-nil.
	Err error
}

// ProjectWorkflowTemplate records the call and returns a projected name.
func (f *FakeArgoProjector) ProjectWorkflowTemplate(_ context.Context, wf *workflowv1alpha1.Workflow) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	f.ProjectedTemplates = append(f.ProjectedTemplates, wf)
	if f.ReturnTemplateName != "" {
		return f.ReturnTemplateName, nil
	}
	return "argo-wft-" + wf.Name, nil
}

// DeleteWorkflowTemplate records the call.
func (f *FakeArgoProjector) DeleteWorkflowTemplate(_ context.Context, wf *workflowv1alpha1.Workflow) error {
	if f.Err != nil {
		return f.Err
	}
	f.DeletedTemplates = append(f.DeletedTemplates, wf)
	return nil
}

// ProjectWorkflow records the call and returns a projected name.
func (f *FakeArgoProjector) ProjectWorkflow(_ context.Context, spec ArgoWorkflowSpec) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	f.ProjectedWorkflows = append(f.ProjectedWorkflows, spec)
	if f.ReturnWorkflowName != "" {
		return f.ReturnWorkflowName, nil
	}
	return spec.Name, nil
}

// GetWorkflowStatus returns the status registered by name, or nil if not found.
func (f *FakeArgoProjector) GetWorkflowStatus(_ context.Context, _, name string) (*ArgoWorkflowStatus, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	if f.StatusByName != nil {
		return f.StatusByName[name], nil
	}
	return nil, nil
}

// DeleteWorkflow records the deleted workflow name.
func (f *FakeArgoProjector) DeleteWorkflow(_ context.Context, _, name string) error {
	if f.Err != nil {
		return f.Err
	}
	f.DeletedWorkflows = append(f.DeletedWorkflows, name)
	return nil
}

// Verify FakeArgoProjector satisfies the interface at compile time.
var _ ArgoProjector = (*FakeArgoProjector)(nil)
