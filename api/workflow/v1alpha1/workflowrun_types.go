// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkflowRunPhase represents the lifecycle phase of a WorkflowRun.
// +kubebuilder:validation:Enum=Pending;Provisioning;Running;Succeeded;Failed;Error
type WorkflowRunPhase string

const (
	WorkflowRunPhasePending      WorkflowRunPhase = "Pending"
	WorkflowRunPhaseProvisioning WorkflowRunPhase = "Provisioning"
	WorkflowRunPhaseRunning      WorkflowRunPhase = "Running"
	WorkflowRunPhaseSucceeded    WorkflowRunPhase = "Succeeded"
	WorkflowRunPhaseFailed       WorkflowRunPhase = "Failed"
	WorkflowRunPhaseError        WorkflowRunPhase = "Error"
)

// WorkflowRunParameter is a name/value pair passed to the Argo Workflow.
type WorkflowRunParameter struct {
	// Name is the parameter name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Value is the parameter value.
	Value string `json:"value"`
}

// ArtifactInput declares an input artifact for the WorkflowRun.
type ArtifactInput struct {
	// Name identifies the artifact.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Path is the artifact location (URL or object-store key).
	// +kubebuilder:validation:MinLength=1
	Path string `json:"path"`
}

// SupervisionContext carries metadata for human-in-the-loop supervision.
type SupervisionContext struct {
	// RequireApproval stops the run at each step boundary until an approval
	// annotation is applied.
	// +optional
	RequireApproval bool `json:"requireApproval,omitempty"`

	// ReviewerRef names a ServiceAccount or Group to notify for approvals.
	// +optional
	ReviewerRef string `json:"reviewerRef,omitempty"`

	// MaxWaitSeconds is how long to wait for approval before failing the step.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=3600
	// +optional
	MaxWaitSeconds int32 `json:"maxWaitSeconds,omitempty"`
}

// WorkflowRunSpec defines the desired state of WorkflowRun.
// Immutability of workspaceRef and workflowRef after run start is enforced
// by the admission webhook (VAP rule cannot reference .status from .spec CEL context).
// +kubebuilder:validation:XValidation:rule="self.retryBudget >= 1",message="spec.retryBudget must be >= 1"
type WorkflowRunSpec struct {
	// WorkspaceRef is the owning Workspace CR (same namespace).
	// Immutable after phase != Pending.
	// +kubebuilder:validation:Required
	// +keese:rebac-tuple=workflowrun.workspace
	WorkspaceRef LocalObjectReference `json:"workspaceRef"`

	// WorkflowRef names the Workflow CR this run targets.
	// Immutable after phase != Pending.
	// +kubebuilder:validation:Required
	// +keese:rebac-tuple=workflowrun.workflow
	WorkflowRef LocalObjectReference `json:"workflowRef"`

	// Parameters are name/value pairs forwarded to the Argo Workflow.
	// +optional
	Parameters []WorkflowRunParameter `json:"parameters,omitempty"`

	// Artifacts declares input artifacts for this run.
	// +optional
	Artifacts []ArtifactInput `json:"artifacts,omitempty"`

	// RetryBudget caps the total number of step retries for this run.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=50
	// +kubebuilder:default=10
	// +optional
	RetryBudget int32 `json:"retryBudget,omitempty"`

	// Timeout is the maximum duration the run may take (e.g. "1h30m").
	// Immutable after phase != Pending.
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// Suspended pauses the run without cancelling it.
	// +optional
	Suspended bool `json:"suspended,omitempty"`

	// SupervisionContext configures human-in-the-loop approval gates.
	// +optional
	SupervisionContext *SupervisionContext `json:"supervisionContext,omitempty"`
}

// NodeStatus mirrors a single Argo workflow node result.
type NodeStatus struct {
	// ID is the Argo node identifier.
	ID string `json:"id"`

	// Phase is the Argo node phase string.
	Phase string `json:"phase"`

	// DisplayName is the human-readable node name.
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// Message carries an optional Argo node message.
	// +optional
	Message string `json:"message,omitempty"`

	// StartedAt is when this node started.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// FinishedAt is when this node completed.
	// +optional
	FinishedAt *metav1.Time `json:"finishedAt,omitempty"`
}

// ArtifactOutput records a single output artifact produced by the run.
type ArtifactOutput struct {
	// Name identifies the artifact.
	Name string `json:"name"`

	// Path is where the artifact was stored.
	Path string `json:"path"`

	// NodeID is the Argo node that produced this artifact.
	// +optional
	NodeID string `json:"nodeID,omitempty"`
}

// WorkflowRunStatus defines the observed state of WorkflowRun.
type WorkflowRunStatus struct {
	// ObservedGeneration is the .metadata.generation last fully reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is the keese-level lifecycle phase.
	// +optional
	Phase WorkflowRunPhase `json:"phase,omitempty"`

	// ArgoPhase mirrors the Argo Workflow phase string.
	// +optional
	ArgoPhase string `json:"argoPhase,omitempty"`

	// ArgoWorkflowName is the name of the projected Argo Workflow object.
	// +optional
	ArgoWorkflowName string `json:"argoWorkflowName,omitempty"`

	// StartedAt records when the Argo Workflow started.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// FinishedAt records when the Argo Workflow finished.
	// +optional
	FinishedAt *metav1.Time `json:"finishedAt,omitempty"`

	// Nodes mirrors Argo node statuses for observability.
	// +optional
	Nodes []NodeStatus `json:"nodes,omitempty"`

	// Artifacts records output artifacts produced by the run.
	// +optional
	Artifacts []ArtifactOutput `json:"artifacts,omitempty"`

	// Conditions holds standard status conditions.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// TupleCount records the number of OpenFGA tuples last written.
	// +optional
	TupleCount int32 `json:"tupleCount,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=wfr
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="ArgoPhase",type="string",JSONPath=".status.argoPhase"

// WorkflowRun is the Schema for the workflowruns API.
// It projects an Argo Workflow and manages NATS streams, SA tokens, and CTA checks.
type WorkflowRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec   WorkflowRunSpec   `json:"spec,omitempty"`
	Status WorkflowRunStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkflowRunList contains a list of WorkflowRun.
type WorkflowRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkflowRun `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkflowRun{}, &WorkflowRunList{})
}
