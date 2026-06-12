// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RecipePhase is the lifecycle phase of a Recipe.
// +kubebuilder:validation:Enum=Pending;Pulling;Verified;Ready;Failed;Terminating
type RecipePhase string

const (
	RecipePhasePending     RecipePhase = "Pending"
	RecipePhasePulling     RecipePhase = "Pulling"
	RecipePhaseVerified    RecipePhase = "Verified"
	RecipePhaseReady       RecipePhase = "Ready"
	RecipePhaseFailed      RecipePhase = "Failed"
	RecipePhaseTerminating RecipePhase = "Terminating"
)

// RecipeTool defines a single tool allowed in the recipe.
type RecipeTool struct {
	// Name is the tool identifier.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// RecipeModel selects the model for a recipe in exactly one of two forms:
//
//   - Literal:  set provider + modelID (the original, backwards-compatible form).
//   - Reference: set modelProviderRef to point at a ModelProvider CR in the same
//     namespace, which owns the endpoint + credential source (E5).
//
// The RecipeModelEitherForm CEL rule enforces the one-of: literal requires both
// provider and modelID; the reference form requires modelProviderRef and forbids
// the literal fields. Existing Recipes that set provider+modelID keep working.
//
// +kubebuilder:validation:XValidation:rule="has(self.modelProviderRef) ? (!has(self.provider) && !has(self.modelID)) : (has(self.provider) && has(self.modelID))",message="set exactly one model form: literal (provider+modelID) or modelProviderRef (RecipeModelEitherForm)"
type RecipeModel struct {
	// Provider is the model provider name, e.g. "anthropic". Literal form;
	// mutually exclusive with modelProviderRef.
	// +kubebuilder:validation:MinLength=1
	// +optional
	Provider string `json:"provider,omitempty"`
	// ModelID is the provider-specific model identifier. Literal form;
	// mutually exclusive with modelProviderRef.
	// +kubebuilder:validation:MinLength=1
	// +optional
	ModelID string `json:"modelID,omitempty"`
	// ModelProviderRef references a ModelProvider CR in the Recipe's namespace
	// that owns the model endpoint and credential source. Reference form;
	// mutually exclusive with the literal provider+modelID fields.
	// +optional
	ModelProviderRef *corev1.LocalObjectReference `json:"modelProviderRef,omitempty"`
}

// RecipeHook is a pre/post-flight hook. Exactly one of cel or shellRef must be set.
// +kubebuilder:validation:XValidation:rule="has(self.cel) != has(self.shellRef)",message="exactly one of cel or shellRef must be set"
type RecipeHook struct {
	// Cel is a CEL expression evaluated before/after the recipe.
	// +optional
	Cel string `json:"cel,omitempty"`
	// ShellRef names a registered shell hook; no inline shell is permitted.
	// +optional
	ShellRef string `json:"shellRef,omitempty"`
}

// RecipeExtension references a RuntimeExtension that must be enabled for this recipe.
type RecipeExtension struct {
	// Name is the RuntimeExtension name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Namespace is the RuntimeExtension namespace.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
}

// RecipeParameter is a typed, injectable parameter.
// +kubebuilder:validation:Enum=string;int;bool
type RecipeParameterType string

const (
	RecipeParameterTypeString RecipeParameterType = "string"
	RecipeParameterTypeInt    RecipeParameterType = "int"
	RecipeParameterTypeBool   RecipeParameterType = "bool"
)

// RecipeParameter defines a typed recipe argument injected as an env var.
type RecipeParameter struct {
	// Name is the parameter name (also used as the env var key).
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Type is the parameter type.
	Type RecipeParameterType `json:"type"`
	// Required indicates the parameter must be supplied at invocation time.
	// +optional
	Required bool `json:"required,omitempty"`
	// Default is the default value when the parameter is not required and not supplied.
	// +optional
	Default string `json:"default,omitempty"`
}

// RecipeSourceRef is a reference to a RecipeSource object.
type RecipeSourceRef struct {
	// Name is the RecipeSource name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Namespace is the RecipeSource namespace; defaults to the Recipe's namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// RecipeSpec defines the desired state of Recipe.
//
// +kubebuilder:validation:XValidation:rule="!has(self.tools) || self.tools.size() == 0 || self.tools.all(t, t.name != '')",message="tool names must be non-empty"
type RecipeSpec struct {
	// Instructions is the OCI layer path to the instructions.md file within the artifact.
	// +kubebuilder:validation:MinLength=1
	Instructions string `json:"instructions"`

	// Tools is the allowlist of tools this recipe may use. Checked at admit against
	// GuardrailBinding.status.effectivePolicy.tools.allow.
	// +keese:rebac-tuple=recipe:R#readable_by@workspace:W
	// +optional
	Tools []RecipeTool `json:"tools,omitempty"`

	// Model specifies the provider and model ID.
	Model RecipeModel `json:"model"`

	// PreFlight is an optional hook that runs before the recipe executes.
	// +optional
	PreFlight *RecipeHook `json:"preFlight,omitempty"`

	// PostFlight is an optional hook that runs after the recipe exits or session ends.
	// +optional
	PostFlight *RecipeHook `json:"postFlight,omitempty"`

	// Extensions lists RuntimeExtensions required by this recipe.
	// Each is checked via OpenFGA at admit: extension:E#enabled_in@workspace:W.
	// +keese:rebac-tuple=recipe:R#uses_extension@extension:E
	// +optional
	Extensions []RecipeExtension `json:"extensions,omitempty"`

	// Parameters defines typed arguments injected as env vars into the workspace.
	// +optional
	Parameters []RecipeParameter `json:"parameters,omitempty"`

	// SourceRef is the reference to the RecipeSource that provides the artifact.
	SourceRef RecipeSourceRef `json:"sourceRef"`
}

// RecipeStatus defines the observed state of Recipe.
type RecipeStatus struct {
	// ObservedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is the current lifecycle phase.
	// +optional
	Phase RecipePhase `json:"phase,omitempty"`

	// ResolvedDigest is the OCI digest of the cached artifact, populated after
	// the RecipeSource is Synced and cosign-verified.
	// +optional
	ResolvedDigest string `json:"resolvedDigest,omitempty"`

	// RebacTupleCount is the number of OpenFGA tuples last synced for debuggability.
	// +optional
	RebacTupleCount int32 `json:"rebacTupleCount,omitempty"`

	// Conditions contains detailed status conditions.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

const (
	// RecipeConditionReady is true when the Recipe is verified and cached.
	RecipeConditionReady = "Ready"
	// RecipeConditionVerified is true when cosign verification succeeded.
	RecipeConditionVerified = "Verified"
	// RecipeConditionProgressing is true while the controller is pulling or verifying.
	RecipeConditionProgressing = "Progressing"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=".spec.model.modelID"
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=".spec.sourceRef.name"

// Recipe is the Schema for the recipes API.
type Recipe struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RecipeSpec   `json:"spec,omitempty"`
	Status RecipeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RecipeList contains a list of Recipe.
type RecipeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Recipe `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Recipe{}, &RecipeList{})
}
