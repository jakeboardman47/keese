// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	argov1alpha1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// workflowTemplateFieldOwner is the SSA field-owner for WorkflowTemplate writes.
const workflowTemplateFieldOwner = workflowFieldOwner // "keese-workflow-controller"

// workflowRunSSAFieldOwner is the SSA field-owner for Argo Workflow writes.
const workflowRunSSAFieldOwner = workflowRunFieldOwner // "keese-workflowrun-controller"

// ClientArgoProjector is the production ArgoProjector backed by a controller-runtime client.
// It projects keese Workflow / WorkflowRun CRs into Argo WorkflowTemplate / Workflow objects
// using Server-Side Apply (SSA) with explicit field ownership.
type ClientArgoProjector struct {
	client client.Client
}

// NewClientArgoProjector constructs a ClientArgoProjector.
func NewClientArgoProjector(c client.Client) *ClientArgoProjector {
	return &ClientArgoProjector{client: c}
}

// Verify ClientArgoProjector satisfies the interface at compile time.
var _ ArgoProjector = (*ClientArgoProjector)(nil)

// ProjectWorkflowTemplate creates or updates an Argo WorkflowTemplate that mirrors the
// keese Workflow's step templates. The projected object is owner-ref'd to the keese
// Workflow so deletion cascades when the keese CR is removed.
// Returns the projected WorkflowTemplate name.
func (p *ClientArgoProjector) ProjectWorkflowTemplate(ctx context.Context, wf *keesev1alpha1.Workflow) (string, error) {
	name := argoTemplateName(wf)

	desired := buildArgoWorkflowTemplate(wf, name)

	// SSA — apply with WorkflowReconciler's field owner.
	if err := p.client.Patch(ctx, desired, client.Apply,
		client.FieldOwner(workflowTemplateFieldOwner),
		client.ForceOwnership,
	); err != nil {
		return "", fmt.Errorf("SSA WorkflowTemplate %s/%s: %w", wf.Namespace, name, err)
	}

	return name, nil
}

// DeleteWorkflowTemplate removes the Argo WorkflowTemplate for a Workflow.
// Returns nil if already absent (idempotent).
func (p *ClientArgoProjector) DeleteWorkflowTemplate(ctx context.Context, wf *keesev1alpha1.Workflow) error {
	obj := &argov1alpha1.WorkflowTemplate{}
	obj.Name = argoTemplateName(wf)
	obj.Namespace = wf.Namespace

	if err := p.client.Delete(ctx, obj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete WorkflowTemplate %s/%s: %w", wf.Namespace, obj.Name, err)
	}

	return nil
}

// ProjectWorkflow creates or updates an Argo Workflow from the given spec.
// The projected object is owner-ref'd via labels (owner-ref on run-scoped objects
// is sufficient because the keese WorkflowRun owns the lifecycle).
// Returns the projected Argo Workflow name.
func (p *ClientArgoProjector) ProjectWorkflow(ctx context.Context, spec ArgoWorkflowSpec) (string, error) {
	desired := buildArgoWorkflow(spec)

	if err := p.client.Patch(ctx, desired, client.Apply,
		client.FieldOwner(workflowRunSSAFieldOwner),
		client.ForceOwnership,
	); err != nil {
		return "", fmt.Errorf("SSA Workflow %s/%s: %w", spec.Namespace, spec.Name, err)
	}

	return spec.Name, nil
}

// GetWorkflowStatus retrieves the current Argo Workflow status by name.
// Returns nil, nil when the Argo Workflow does not yet exist.
func (p *ClientArgoProjector) GetWorkflowStatus(ctx context.Context, namespace, name string) (*ArgoWorkflowStatus, error) {
	var obj argov1alpha1.Workflow
	if err := p.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &obj); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get Argo Workflow %s/%s: %w", namespace, name, err)
	}

	return argoStatusToKeese(&obj), nil
}

// DeleteWorkflow removes the Argo Workflow by name. Returns nil if absent.
func (p *ClientArgoProjector) DeleteWorkflow(ctx context.Context, namespace, name string) error {
	obj := &argov1alpha1.Workflow{}
	obj.Name = name
	obj.Namespace = namespace

	if err := p.client.Delete(ctx, obj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete Argo Workflow %s/%s: %w", namespace, name, err)
	}

	return nil
}

// ──────────────────────────────────────────────────
// helpers
// ──────────────────────────────────────────────────

// argoTemplateName derives the deterministic Argo WorkflowTemplate name for a keese Workflow.
func argoTemplateName(wf *keesev1alpha1.Workflow) string {
	return "argo-wft-" + wf.Name
}

// buildArgoWorkflowTemplate constructs the Argo WorkflowTemplate object ready for SSA.
// TypeMeta is required for SSA (the server uses it to route the apply).
func buildArgoWorkflowTemplate(wf *keesev1alpha1.Workflow, name string) *argov1alpha1.WorkflowTemplate {
	templates := keeseStepsToArgoTemplates(wf)

	wft := &argov1alpha1.WorkflowTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "WorkflowTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: wf.Namespace,
			Labels: map[string]string{
				"keese.ai/managed":  "true",
				"keese.ai/workflow": wf.Name,
			},
			// Owner-ref to the keese Workflow so deletion cascades automatically.
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         wf.GroupVersionKind().GroupVersion().String(),
					Kind:               wf.Kind,
					Name:               wf.Name,
					UID:                wf.UID,
					BlockOwnerDeletion: boolPtr(true),
					Controller:         boolPtr(true),
				},
			},
		},
		Spec: argov1alpha1.WorkflowSpec{
			Entrypoint: wf.Spec.Entrypoint,
			Templates:  templates,
		},
	}

	return wft
}

// buildArgoWorkflow constructs the Argo Workflow object ready for SSA from an ArgoWorkflowSpec.
func buildArgoWorkflow(spec ArgoWorkflowSpec) *argov1alpha1.Workflow {
	// Convert keese WorkflowRunParameters → Argo Arguments.
	args := parametersToArguments(spec.Parameters)

	wf := &argov1alpha1.Workflow{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "Workflow",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        spec.Name,
			Namespace:   spec.Namespace,
			Labels:      spec.Labels,
			Annotations: spec.Annotations,
		},
		Spec: argov1alpha1.WorkflowSpec{
			WorkflowTemplateRef: &argov1alpha1.WorkflowTemplateRef{
				Name: spec.WorkflowTemplateRefName,
			},
			Arguments: args,
		},
	}

	// Propagate optional timeout as ActiveDeadlineSeconds.
	if spec.Timeout != nil {
		secs := int64(spec.Timeout.Duration.Seconds())
		wf.Spec.ActiveDeadlineSeconds = &secs
	}

	// Inject per-run retry strategy cap.
	if spec.RetryLimit > 0 {
		wf.Spec.RetryStrategy = &argov1alpha1.RetryStrategy{
			Limit: intstrPtr(int(spec.RetryLimit)),
		}
	}

	return wf
}

// keeseStepsToArgoTemplates converts keese WorkflowTemplateSteps into Argo Templates.
// Each step becomes a container Template; they are wrapped in a steps Template that
// chains them sequentially.
func keeseStepsToArgoTemplates(wf *keesev1alpha1.Workflow) []argov1alpha1.Template {
	templates := make([]argov1alpha1.Template, 0, len(wf.Spec.Templates)+1)

	// Build a container template per step.
	for _, step := range wf.Spec.Templates {
		retryLimit := step.RetryLimit
		tmpl := argov1alpha1.Template{
			Name: step.Name,
			Container: &corev1.Container{
				Name:    step.Name,
				Image:   step.Image,
				Command: step.Command,
				Args:    step.Args,
			},
		}
		if retryLimit > 0 {
			tmpl.RetryStrategy = &argov1alpha1.RetryStrategy{
				Limit: intstrPtr(int(retryLimit)),
			}
		}
		templates = append(templates, tmpl)
	}

	// Build the steps template that chains them.
	steps := make([]argov1alpha1.ParallelSteps, 0, len(wf.Spec.Templates))
	for _, step := range wf.Spec.Templates {
		steps = append(steps, argov1alpha1.ParallelSteps{
			Steps: []argov1alpha1.WorkflowStep{
				{Name: step.Name, Template: step.Name},
			},
		})
	}

	entrypointTmpl := argov1alpha1.Template{
		Name:  wf.Spec.Entrypoint,
		Steps: steps,
	}
	// Avoid duplicating if a step has the same name as the entrypoint.
	entrypointExists := false
	for _, t := range templates {
		if t.Name == wf.Spec.Entrypoint {
			entrypointExists = true
			break
		}
	}
	if !entrypointExists {
		templates = append([]argov1alpha1.Template{entrypointTmpl}, templates...)
	}

	return templates
}

// parametersToArguments converts keese WorkflowRunParameters to Argo Arguments.
func parametersToArguments(params []keesev1alpha1.WorkflowRunParameter) argov1alpha1.Arguments {
	if len(params) == 0 {
		return argov1alpha1.Arguments{}
	}
	argoParams := make([]argov1alpha1.Parameter, 0, len(params))
	for _, p := range params {
		val := argov1alpha1.AnyString(p.Value)
		argoParams = append(argoParams, argov1alpha1.Parameter{
			Name:  p.Name,
			Value: &val,
		})
	}
	return argov1alpha1.Arguments{Parameters: argoParams}
}

// argoStatusToKeese converts an Argo Workflow's status to the keese mirror struct.
func argoStatusToKeese(obj *argov1alpha1.Workflow) *ArgoWorkflowStatus {
	st := &ArgoWorkflowStatus{
		Phase: string(obj.Status.Phase),
	}

	if !obj.Status.StartedAt.IsZero() {
		t := obj.Status.StartedAt
		st.StartedAt = &t
	}
	if !obj.Status.FinishedAt.IsZero() {
		t := obj.Status.FinishedAt
		st.FinishedAt = &t
	}

	for _, n := range obj.Status.Nodes {
		node := ArgoNodeStatus{
			ID:          n.ID,
			Phase:       string(n.Phase),
			DisplayName: n.DisplayName,
			Message:     n.Message,
		}
		if !n.StartedAt.IsZero() {
			t := n.StartedAt
			node.StartedAt = &t
		}
		if !n.FinishedAt.IsZero() {
			t := n.FinishedAt
			node.FinishedAt = &t
		}
		st.Nodes = append(st.Nodes, node)
	}

	if obj.Status.Outputs != nil {
		for _, a := range obj.Status.Outputs.Artifacts {
			st.Artifacts = append(st.Artifacts, ArgoArtifact{
				Name: a.Name,
				Path: a.Path,
			})
		}
	}

	return st
}

// boolPtr is a small helper to get a *bool from a literal.
func boolPtr(b bool) *bool { return &b }

// intstrPtr converts an int to the *intstr.IntOrString that Argo's RetryStrategy.Limit needs.
func intstrPtr(i int) *intstr.IntOrString {
	v := intstr.FromInt32(int32(i))
	return &v
}
