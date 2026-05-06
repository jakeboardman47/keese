// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TokenBudgetPhase represents the lifecycle phase of a TokenBudget.
// +kubebuilder:validation:Enum=Ready;Exhausted;SoftExhausted;ResetFailed
type TokenBudgetPhase string

const (
	TokenBudgetPhaseReady         TokenBudgetPhase = "Ready"
	TokenBudgetPhaseExhausted     TokenBudgetPhase = "Exhausted"
	TokenBudgetPhaseSoftExhausted TokenBudgetPhase = "SoftExhausted"
	TokenBudgetPhaseResetFailed   TokenBudgetPhase = "ResetFailed"
)

// ExhaustionMode controls how the controller enforces the budget on exhaustion.
// +kubebuilder:validation:Enum=hard;soft;disabled
type ExhaustionMode string

const (
	ExhaustionModeHard     ExhaustionMode = "hard"
	ExhaustionModeSoft     ExhaustionMode = "soft"
	ExhaustionModeDisabled ExhaustionMode = "disabled"
)

// TokenBudgetScopeType discriminates tenant vs workspace scope.
// +kubebuilder:validation:Enum=tenant;workspace
type TokenBudgetScopeType string

const (
	TokenBudgetScopeTenant    TokenBudgetScopeType = "tenant"
	TokenBudgetScopeWorkspace TokenBudgetScopeType = "workspace"
)

// TokenBudgetScopeRef holds the name of the scoped resource (tenant or workspace).
type TokenBudgetScopeRef struct {
	// Name of the tenant or workspace this budget applies to.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// TokenBudgetScope is a discriminated one-of: exactly one of tenant or workspace must be set.
//
// +kubebuilder:validation:XValidation:rule="has(self.tenant) != has(self.workspace)",message="exactly one of tenant or workspace must be set"
type TokenBudgetScope struct {
	// Tenant scopes this budget to a named Tenant resource (cluster-level).
	// +optional
	Tenant *TokenBudgetScopeRef `json:"tenant,omitempty"`

	// Workspace scopes this budget to a named Workspace resource (namespace-scoped).
	// +optional
	Workspace *TokenBudgetScopeRef `json:"workspace,omitempty"`
}

// TokenLimit defines the token cap per model (or aggregate via model="*").
type TokenLimit struct {
	// Model is the upstream model identifier; use "*" for an aggregate cap across all models.
	// +kubebuilder:validation:MinLength=1
	Model string `json:"model"`

	// InputTokens is the maximum input (prompt) tokens allowed in the window.
	// +kubebuilder:validation:Minimum=1
	// +optional
	InputTokens *int64 `json:"inputTokens,omitempty"`

	// OutputTokens is the maximum output (completion) tokens allowed in the window.
	// +kubebuilder:validation:Minimum=1
	// +optional
	OutputTokens *int64 `json:"outputTokens,omitempty"`

	// TotalTokens is the maximum combined token count allowed in the window.
	// +kubebuilder:validation:Minimum=1
	// +optional
	TotalTokens *int64 `json:"totalTokens,omitempty"`
}

// PricebookRef references an optional pricebook for USD billing export (design 21).
type PricebookRef struct {
	// Name of the pricebook resource.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// TokenBudgetSpec defines the desired state of TokenBudget.
//
// +kubebuilder:validation:XValidation:rule="self.limits.size() > 0",message="limits must not be empty"
type TokenBudgetSpec struct {
	// Scope discriminates tenant vs workspace budget. Exactly one of tenant/workspace must be set.
	Scope TokenBudgetScope `json:"scope"`

	// Limits defines per-model (or aggregate) token caps for this budget.
	// Each entry must set at least one of inputTokens, outputTokens, or totalTokens.
	//
	// +keese:rebac-tuple=budget:can_enforce
	// +kubebuilder:validation:MinItems=1
	Limits []TokenLimit `json:"limits"`

	// WindowDuration is the accounting window as a Go duration string.
	// Valid units: h (hours) or d (days). Minimum 1h, maximum 720h.
	// Default: 720h (30 days).
	//
	// +kubebuilder:default="720h"
	// +kubebuilder:validation:Pattern=`^[0-9]+(h|d|m)$`
	WindowDuration string `json:"windowDuration,omitempty"`

	// WindowAnchor is an optional RFC3339 timestamp anchoring the window start.
	// Controller advances by WindowDuration per elapsed period.
	// +optional
	WindowAnchor string `json:"windowAnchor,omitempty"`

	// ExhaustionMode controls enforcement on budget crossover: hard (429), soft (warn), disabled.
	// Default: hard.
	//
	// +kubebuilder:default="hard"
	ExhaustionMode ExhaustionMode `json:"exhaustionMode,omitempty"`

	// PricebookRef optionally references a pricebook for USD billing export (design 21).
	// +optional
	PricebookRef *PricebookRef `json:"pricebookRef,omitempty"`
}

// TokenUsageEntry records consumed tokens per model (matches TokenLimit shape).
type TokenUsageEntry struct {
	// Model is the model identifier.
	Model string `json:"model"`

	// InputTokens consumed in the current window.
	// +optional
	InputTokens int64 `json:"inputTokens,omitempty"`

	// OutputTokens consumed in the current window.
	// +optional
	OutputTokens int64 `json:"outputTokens,omitempty"`

	// TotalTokens consumed in the current window.
	// +optional
	TotalTokens int64 `json:"totalTokens,omitempty"`
}

// TokenBudgetStatus defines the observed state of TokenBudget.
type TokenBudgetStatus struct {
	// ObservedGeneration is the last processed generation of the spec.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is the lifecycle phase of the budget.
	Phase TokenBudgetPhase `json:"phase,omitempty"`

	// WindowStart is the RFC3339 start time of the current accounting window.
	WindowStart string `json:"windowStart,omitempty"`

	// WindowEnd is the RFC3339 end time of the current accounting window.
	WindowEnd string `json:"windowEnd,omitempty"`

	// ConsumedCurrent holds per-model consumption in the current window.
	// +optional
	ConsumedCurrent []TokenUsageEntry `json:"consumedCurrent,omitempty"`

	// ConsumedPrevious holds per-model consumption from the previous window,
	// populated on window reset and zeroed when ConsumedCurrent is cleared.
	// +optional
	ConsumedPrevious []TokenUsageEntry `json:"consumedPrevious,omitempty"`

	// LastReconcileTime records the most recent successful reconcile timestamp.
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`

	// Conditions holds the standard Kubernetes condition list.
	// Types: Ready, BudgetExceeded, ResetFailed.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=tb
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="WindowEnd",type="string",JSONPath=".status.windowEnd"
// +kubebuilder:printcolumn:name="Exhaustion",type="string",JSONPath=".spec.exhaustionMode"

// TokenBudget is the Schema for the tokenbudgets API.
type TokenBudget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TokenBudgetSpec   `json:"spec,omitempty"`
	Status TokenBudgetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TokenBudgetList contains a list of TokenBudget.
type TokenBudgetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TokenBudget `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TokenBudget{}, &TokenBudgetList{})
}
