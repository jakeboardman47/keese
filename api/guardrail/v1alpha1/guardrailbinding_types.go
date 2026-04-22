// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BindingScopeType enumerates the three scope tiers.
// +kubebuilder:validation:Enum=Cluster;Tenant;Workspace
type BindingScopeType string

const (
	BindingScopeCluster   BindingScopeType = "Cluster"
	BindingScopeTenant    BindingScopeType = "Tenant"
	BindingScopeWorkspace BindingScopeType = "Workspace"
)

// BindingPhase is the high-level status phase.
// +kubebuilder:validation:Enum=Ready;Degraded;Pending
type BindingPhase string

const (
	BindingPhaseReady   BindingPhase = "Ready"
	BindingPhaseDegraded BindingPhase = "Degraded"
	BindingPhasePending BindingPhase = "Pending"
)

// RecipeHookEvent enumerates hook trigger points.
// +kubebuilder:validation:Enum=beforeToolCall;afterToolCall;onError
type RecipeHookEvent string

// RateLimitScope enumerates the scoping of a rate-limit window.
// +kubebuilder:validation:Enum=tenant;workspace;sa
type RateLimitScope string

// NamespacedRef is a name+namespace reference.
type NamespacedRef struct {
	// Name is the referenced object's name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Namespace is the referenced object's namespace.
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`
}

// BindingScope defines the scope tier and its associated reference.
type BindingScope struct {
	// Type is the scope tier. Immutable after creation (enforced by VAP).
	// +kubebuilder:validation:Required
	Type BindingScopeType `json:"type"`

	// TenantRef is required when Type == Tenant.
	// +optional
	TenantRef *NamespacedRef `json:"tenantRef,omitempty"`

	// WorkspaceRef is required when Type == Workspace.
	// +optional
	WorkspaceRef *NamespacedRef `json:"workspaceRef,omitempty"`
}

// RateLimit describes a per-tool request-rate ceiling.
// Fields are locked by 06-ii-spec-schema.md §05c cross-dependency.
type RateLimit struct {
	// Requests is the request count threshold (0 = no limit).
	// +kubebuilder:default=0
	Requests int64 `json:"requests"`

	// Window is the duration string for the rate window, e.g. "1m", "5s".
	// +kubebuilder:default="1m"
	Window string `json:"window"`

	// Scope controls per-tenant, per-workspace, or per-service-account limiting.
	// +kubebuilder:default="sa"
	Scope RateLimitScope `json:"scope"`
}

// ToolsSpec defines the allowed/denied MCP tools and their rate limits.
type ToolsSpec struct {
	// Allow is the allowlist of MCP tool names. Empty means allow all.
	// Merge rule: intersection across all bindings in the scope chain.
	// +keese:rebac-tuple=tool#allowed_in@workspace
	// +optional
	Allow []string `json:"allow,omitempty"`

	// Deny is the denylist of MCP tool names.
	// Merge rule: union across all bindings in the scope chain.
	// +keese:rebac-tuple=tool#denied_in@workspace
	// +optional
	Deny []string `json:"deny,omitempty"`

	// RateLimit applies a request-rate ceiling to all tools in this binding.
	// Merge rule: min(requests) per matching (window, scope) tuple.
	// +optional
	RateLimit *RateLimit `json:"rateLimit,omitempty"`
}

// KyvernoPolicyRef is a reference to a Kyverno ClusterPolicy by name.
type KyvernoPolicyRef struct {
	// PolicyRef is the ClusterPolicy .metadata.name.
	// +kubebuilder:validation:Required
	PolicyRef string `json:"policyRef"`
}

// OpenFGASpec configures the OpenFGA tuple source for this binding.
type OpenFGASpec struct {
	// ConfigMapRef points to the ConfigMap holding the OpenFGA tuple definitions.
	// +optional
	ConfigMapRef *NamespacedRef `json:"configMapRef,omitempty"`
}

// EnvoySpec holds the Envoy SecurityPolicy reference for this binding.
// The controller SSA-applies the referenced SecurityPolicy.
type EnvoySpec struct {
	// SecurityPolicyRef references an Envoy SecurityPolicy in the gateway namespace.
	// +optional
	SecurityPolicyRef *NamespacedRef `json:"securityPolicyRef,omitempty"`
}

// ServiceRef is a reference to an in-cluster Service used by recipe hooks.
// URL form is rejected by VAP (rule 05.4, zero-trust egress).
type ServiceRef struct {
	// Name is the Service .metadata.name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Namespace is the Service namespace.
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`
	// Port is the Service port number.
	// +kubebuilder:validation:Required
	Port int32 `json:"port"`
	// Path is the HTTP path to call on the Service, e.g. "/before-tool-call".
	// +kubebuilder:validation:Required
	Path string `json:"path"`
}

// RecipeHook registers a pre- or post-flight hook for recipes.
type RecipeHook struct {
	// Event names the trigger point.
	// +kubebuilder:validation:Required
	Event RecipeHookEvent `json:"event"`

	// ServiceRef is required; URL field is rejected by VAP.
	// +kubebuilder:validation:Required
	ServiceRef ServiceRef `json:"serviceRef"`
}

// TokenBudget defines per-resource token ceilings.
// Merge rule: min() across all bindings in the scope chain.
type TokenBudget struct {
	// Input is the maximum number of input tokens (0 = no limit).
	// +keese:rebac-tuple=workspace#has_budget
	// +kubebuilder:default=0
	Input int64 `json:"input"`
	// Output is the maximum number of output tokens (0 = no limit).
	// +kubebuilder:default=0
	Output int64 `json:"output"`
	// Total is the combined input+output ceiling (0 = no limit).
	// +kubebuilder:default=0
	Total int64 `json:"total"`
}

// InheritRef is a reference to another GuardrailBinding resolved at merge time.
type InheritRef struct {
	// Name is the GuardrailBinding .metadata.name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Namespace is the GuardrailBinding namespace.
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`
}

// GuardrailBindingSpec defines the desired state of GuardrailBinding.
type GuardrailBindingSpec struct {
	// Scope defines the tier and associated reference.
	// +kubebuilder:validation:Required
	Scope BindingScope `json:"scope"`

	// Tools configures MCP tool allow/deny lists and rate limits.
	// +optional
	Tools *ToolsSpec `json:"tools,omitempty"`

	// Kyverno lists Kyverno ClusterPolicy references to compose.
	// Merge rule: union — all named policies apply.
	// +optional
	Kyverno []KyvernoPolicyRef `json:"kyverno,omitempty"`

	// OpenFGA configures the OpenFGA tuple source.
	// +optional
	OpenFGA *OpenFGASpec `json:"openfga,omitempty"`

	// Envoy references the Envoy SecurityPolicy for this binding.
	// +optional
	Envoy *EnvoySpec `json:"envoy,omitempty"`

	// RecipeHooks registers pre- and post-flight hooks.
	// Merge rule: union — hooks from all bindings run.
	// +optional
	RecipeHooks []RecipeHook `json:"recipeHooks,omitempty"`

	// TokenBudget defines per-resource token ceilings.
	// +optional
	TokenBudget *TokenBudget `json:"tokenBudget,omitempty"`

	// Inherit lists parent GuardrailBindings resolved at merge time.
	// +optional
	Inherit []InheritRef `json:"inherit,omitempty"`
}

// EffectiveRateLimit is the merged rate limit placed in status.
type EffectiveRateLimit struct {
	Requests int64          `json:"requests"`
	Window   string         `json:"window"`
	Scope    RateLimitScope `json:"scope"`
}

// EffectiveTools is the merged tool policy placed in status.
type EffectiveTools struct {
	// Allow is the intersection of all binding allow lists.
	Allow []string `json:"allow,omitempty"`
	// Deny is the union of all binding deny lists.
	Deny []string `json:"deny,omitempty"`
	// RateLimit is the strictest merged rate limit.
	RateLimit *EffectiveRateLimit `json:"rateLimit,omitempty"`
}

// EffectiveTokenBudget is the merged token budget placed in status.
type EffectiveTokenBudget struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
	Total  int64 `json:"total"`
}

// EffectivePolicy is the computed merged policy consumed by Recipe and
// Workspace admission. The ext_authz sidecar reads ONLY this field.
type EffectivePolicy struct {
	// Tools is the merged tool policy.
	Tools EffectiveTools `json:"tools,omitempty"`
	// TokenBudget is the merged token budget (min across all bindings).
	TokenBudget EffectiveTokenBudget `json:"tokenBudget,omitempty"`
	// ObservedGeneration is the generation of this binding at compute time.
	// Used by TOCTOU VAP to reject stale reads.
	ObservedGeneration int64 `json:"observedGeneration"`
}

const (
	// ConditionReady is true when the effective policy has been computed.
	ConditionReady = "Ready"
	// ConditionParentReadable is true when all inherited parent bindings are readable.
	ConditionParentReadable = "ParentReadable"
)

// GuardrailBindingStatus defines the observed state of GuardrailBinding.
type GuardrailBindingStatus struct {
	// Phase is the high-level status: Ready, Degraded, or Pending.
	// +optional
	Phase BindingPhase `json:"phase,omitempty"`

	// ObservedGeneration is the generation of the binding last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// EffectivePolicy is the merged output consumed by Recipe + Workspace admission.
	// +optional
	EffectivePolicy *EffectivePolicy `json:"effectivePolicy,omitempty"`

	// LastMergeTime is the wall-clock time of the last successful merge.
	// +optional
	LastMergeTime *metav1.Time `json:"lastMergeTime,omitempty"`

	// RebacTupleCount records the number of OpenFGA tuples synced on the last reconcile.
	// +optional
	RebacTupleCount int32 `json:"rebacTupleCount,omitempty"`

	// Conditions holds standard Kubernetes condition objects.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Scope",type="string",JSONPath=".metadata.labels.keese\\.ai/binding-scope"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="ObservedGen",type="integer",JSONPath=".status.observedGeneration"

// GuardrailBinding is the Schema for the guardrailbindings API.
// It composes Kyverno policies, OpenFGA tuples, Envoy SecurityPolicy refs,
// recipe hooks, and token budgets into a single CRD with a strictest-wins
// merge lattice spanning Cluster / Tenant / Workspace scopes.
type GuardrailBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GuardrailBindingSpec   `json:"spec,omitempty"`
	Status GuardrailBindingStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GuardrailBindingList contains a list of GuardrailBinding.
type GuardrailBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GuardrailBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GuardrailBinding{}, &GuardrailBindingList{})
}
