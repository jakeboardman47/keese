// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkspaceShareSpec defines the desired state of WorkspaceShare.
// A WorkspaceShare grants cross-namespace access to a Workspace by projecting
// a ReferenceGrant (Gateway API) and OpenFGA tuples for cross-tenant authorization.
type WorkspaceShareSpec struct {
	// WorkspaceRef references the Workspace to share.
	// Must be in the same namespace as this WorkspaceShare.
	// +kubebuilder:validation:Required
	// +keese:rebac-tuple=workspace.shared_with
	WorkspaceRef LocalObjectReference `json:"workspaceRef"`

	// TargetNamespace is the namespace that will receive access to this workspace.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	TargetNamespace string `json:"targetNamespace"`

	// Grantees lists user or service-account identifiers granted access via this share.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +keese:rebac-tuple=workspace.cross_ns_viewer
	Grantees []string `json:"grantees,omitempty"`

	// ReadOnly controls whether grantees receive viewer (true) or editor (false) access.
	// +optional
	// +kubebuilder:default=true
	ReadOnly bool `json:"readOnly,omitempty"`
}

// WorkspaceShareStatus defines the observed state of WorkspaceShare.
type WorkspaceShareStatus struct {
	// ObservedGeneration is the .metadata.generation that the controller last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions contains detailed status conditions.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ReferenceGrantName is the name of the Gateway API ReferenceGrant projected by this share.
	// +optional
	ReferenceGrantName string `json:"referenceGrantName,omitempty"`

	// RebacTupleCount is the number of OpenFGA tuples owned by this share.
	// +optional
	RebacTupleCount int32 `json:"rebacTupleCount,omitempty"`
}

// WorkspaceShare condition types.
const (
	// WorkspaceShareConditionReady indicates the share is fully applied.
	WorkspaceShareConditionReady = "Ready"
	// WorkspaceShareConditionProgressing indicates the share is being set up.
	WorkspaceShareConditionProgressing = "Progressing"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=wss
// +kubebuilder:printcolumn:name="Workspace",type=string,JSONPath=".spec.workspaceRef.name"
// +kubebuilder:printcolumn:name="TargetNamespace",type=string,JSONPath=".spec.targetNamespace"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ReadOnly",type=boolean,JSONPath=".spec.readOnly"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// WorkspaceShare is the Schema for the workspaceshares API.
type WorkspaceShare struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkspaceShareSpec   `json:"spec,omitempty"`
	Status WorkspaceShareStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkspaceShareList contains a list of WorkspaceShare.
type WorkspaceShareList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkspaceShare `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkspaceShare{}, &WorkspaceShareList{})
}
