// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkspaceTool is the namespaced, tenant-owned counterpart to
// `ToolBinding`. Tenants register HTTP-request → tool mappings for
// internal APIs or MCP-tools the platform catalogue does not know
// about.
//
// Tool-name resolution: the final OpenFGA `tool:<n>` for a
// WorkspaceTool match is `tool:<namespace>.<spec.toolName>` — the
// namespace prefix scopes the name into the tenant's
// `tool:<namespace>.*` slice and prevents collision with the
// cluster-scoped `ToolBinding` namespace.
//
// keese-authz tries cluster-scoped ToolBindings first; namespaced
// WorkspaceTools are only consulted for requests whose subject
// resolves to a workspace inside the WorkspaceTool's namespace.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=wt,categories=keese
// +kubebuilder:printcolumn:name=Path,type=string,JSONPath=`.spec.match.paths[0].value`
// +kubebuilder:printcolumn:name=Tool,type=string,JSONPath=`.spec.toolName`
// +kubebuilder:printcolumn:name=Workspace,type=string,JSONPath=`.spec.workspaceRef.name`
// +kubebuilder:printcolumn:name=Ready,type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
type WorkspaceTool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkspaceToolSpec   `json:"spec,omitempty"`
	Status WorkspaceToolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type WorkspaceToolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkspaceTool `json:"items"`
}

// WorkspaceToolSpec mirrors ToolBindingSpec but adds a workspace
// scope. The match shape is identical; the toolName is namespaced
// internally to prevent cross-tenant collision.
type WorkspaceToolSpec struct {
	Match HTTPRouteMatch `json:"match"`

	// ToolName is the per-namespace tool name; the final OpenFGA
	// object is `tool:<namespace>.<toolName>`.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9.-]*$`
	// +keese:rebac-tuple=tool:<n>#allowed_in@workspace:<w>
	ToolName string `json:"toolName"`

	// WorkspaceRef restricts which workspace this binding applies
	// to. When set, only requests originating from a session pod
	// of this workspace match. Empty `workspaceRef` makes the
	// binding apply to every workspace in the namespace.
	//
	// +optional
	WorkspaceRef *NamespaceLocalRef `json:"workspaceRef,omitempty"`

	// +optional
	BodyDiscriminator *BodyDiscriminator `json:"bodyDiscriminator,omitempty"`

	// +kubebuilder:default=ServiceAccountSubject
	SubjectFrom SubjectFromSource `json:"subjectFrom,omitempty"`

	// +optional
	JWTClaimName string `json:"jwtClaimName,omitempty"`

	// +kubebuilder:default=ServiceAccountName
	WorkspaceFrom WorkspaceFromSource `json:"workspaceFrom,omitempty"`
}

// NamespaceLocalRef points at another resource in the same namespace.
type NamespaceLocalRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// WorkspaceToolStatus mirrors ToolBindingStatus.
type WorkspaceToolStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	MatchedRequests int64 `json:"matchedRequests,omitempty"`
}

func init() {
	SchemeBuilder.Register(&WorkspaceTool{}, &WorkspaceToolList{})
}
