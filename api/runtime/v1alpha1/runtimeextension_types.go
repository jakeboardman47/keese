// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ExtensionToolRef names a tool that this extension exposes.
type ExtensionToolRef struct {
	// Name must match a tool name permitted by the GuardrailBinding effective policy (design 16).
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// RuntimeExtensionSpec defines the desired state of RuntimeExtension.
//
// +keese:rebac-tuple=extension.owner
type RuntimeExtensionSpec struct {
	// RuntimeRef points to the AgentRuntime this extension is bound to.
	// The controller writes extension:E#enabled_in@workspace:W tuples for
	// each Workspace that has this extension admitted.
	//
	// +keese:rebac-tuple=extension.enabled_in
	RuntimeRef RuntimeRef `json:"runtimeRef"`

	// Tools is the allow-list of tools this extension exposes.
	// Each name must exist in the GuardrailBinding effective policy.
	// +optional
	Tools []ExtensionToolRef `json:"tools,omitempty"`

	// Description is a human-readable summary of this extension.
	// +optional
	Description string `json:"description,omitempty"`
}

// RuntimeRef is a namespaced reference to an AgentRuntime (cluster-scoped, so Namespace is ignored).
type RuntimeRef struct {
	// Name of the AgentRuntime.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// RuntimeExtensionPhase is the lifecycle phase of a RuntimeExtension.
// +kubebuilder:validation:Enum=Pending;Ready;Degraded
type RuntimeExtensionPhase string

const (
	// RuntimeExtensionPhasePending means the extension has not yet been validated.
	RuntimeExtensionPhasePending RuntimeExtensionPhase = "Pending"
	// RuntimeExtensionPhaseReady means the extension is bound and tuples are written.
	RuntimeExtensionPhaseReady RuntimeExtensionPhase = "Ready"
	// RuntimeExtensionPhaseDegraded means the extension has errors (e.g. runtimeRef invalid).
	RuntimeExtensionPhaseDegraded RuntimeExtensionPhase = "Degraded"
)

// RuntimeExtensionStatus defines the observed state of RuntimeExtension.
type RuntimeExtensionStatus struct {
	// ObservedGeneration is the .metadata.generation the controller last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is the high-level lifecycle phase.
	// +optional
	Phase RuntimeExtensionPhase `json:"phase,omitempty"`

	// BoundWorkspaces is the count of active enabled_in tuples (one per admitted Workspace).
	// +optional
	BoundWorkspaces int32 `json:"boundWorkspaces,omitempty"`

	// Conditions holds standard meta/v1 condition entries.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Runtime",type=string,JSONPath=`.spec.runtimeRef.name`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// RuntimeExtension is the Schema for the runtimeextensions API.
// Namespaced; bundles N tools for a runtime and manages ReBAC tuples.
type RuntimeExtension struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RuntimeExtensionSpec   `json:"spec,omitempty"`
	Status RuntimeExtensionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RuntimeExtensionList contains a list of RuntimeExtension.
type RuntimeExtensionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RuntimeExtension `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RuntimeExtension{}, &RuntimeExtensionList{})
}
