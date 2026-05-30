// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OIDCProviderPhase represents the operational phase of an OIDCProvider.
// +kubebuilder:validation:Enum=Active;Degraded
type OIDCProviderPhase string

const (
	OIDCProviderPhaseActive   OIDCProviderPhase = "Active"
	OIDCProviderPhaseDegraded OIDCProviderPhase = "Degraded"
)

// NormalizationConfig controls subject normalization applied after template rendering.
type NormalizationConfig struct {
	// Lowercase converts the rendered subject to lowercase before OpenFGA Check.
	// +optional
	Lowercase bool `json:"lowercase,omitempty"`

	// Trim strips leading/trailing whitespace from the rendered subject.
	// +optional
	Trim bool `json:"trim,omitempty"`
}

// AudienceTemplate defines a named token-projection audience with its own template and TTL.
//
// +kubebuilder:validation:XValidation:rule="self.expirationSeconds >= 60 && self.expirationSeconds <= 600",message="expirationSeconds must be in [60,600] (rule 05.3 TTL cap)"
type AudienceTemplate struct {
	// Name is the unique identifier for this audience template.
	// At least one entry named "egress" is required in the OIDCProvider (VAP-enforced).
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Template is a Go template over {.Claims, .Issuer, .Audience} using the restricted
	// Sprig allow-list: trimPrefix, trimSuffix, lower, upper, split, replace.
	// Admission rejects any template referencing a function outside this set.
	// +kubebuilder:validation:MinLength=1
	Template string `json:"template"`

	// ExpirationSeconds is the projected ServiceAccount token lifetime. Range [60,600]
	// per rule 05.3. Kubelet rotates the token at 80% of this value.
	// +kubebuilder:validation:Minimum=60
	// +kubebuilder:validation:Maximum=600
	ExpirationSeconds int32 `json:"expirationSeconds"`
}

// OIDCProviderSpec defines the desired state of OIDCProvider.
//
// Finalizer:
//   - finalizers.oidcprovider.keese.ai/cache-flush
//     Sends cache-flush signal to all gateway pods before allowing CR deletion.
//
// SSA fieldOwner: keese-oidcprovider-controller
//
// VAP (ValidatingAdmissionPolicy) enforces:
//  1. subjectTemplate parses without error
//  2. Every audienceTemplates[].template parses without error
//  3. Sprig allow-list: only trimPrefix, trimSuffix, lower, upper, split, replace
//  4. audienceTemplates contains at least one entry named "egress"
//  5. Every audienceTemplates[].expirationSeconds in [60,600]
//
// +kubebuilder:validation:XValidation:rule="self.audienceTemplates.exists(t, t.name == 'egress')",message="audienceTemplates must contain at least one entry named 'egress'"
type OIDCProviderSpec struct {
	// Issuer is the OIDC issuer URL. JWKS is auto-derived via
	// <issuer>/.well-known/openid-configuration unless jwksUri is set.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +kubebuilder:validation:MinLength=1
	Issuer string `json:"issuer"`

	// JWKSUri overrides the auto-derived JWKS endpoint. Use for air-gapped clusters,
	// Dex, or Pinniped deployments.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +optional
	JWKSUri string `json:"jwksUri,omitempty"`

	// Audiences lists glob patterns accepted for the aud claim. E.g. "keese-egress-*".
	// +kubebuilder:validation:MinItems=1
	// +listType=set
	Audiences []string `json:"audiences"`

	// SubjectTemplate is a Go template over {.Claims, .Issuer, .Audience}.
	// Only functions in the Sprig allow-list may be used: trimPrefix, trimSuffix,
	// lower, upper, split, replace. Admission rejects on parse failure.
	// +kubebuilder:validation:MinLength=1
	SubjectTemplate string `json:"subjectTemplate"`

	// AudienceTemplates defines the named token-projection audiences for agent pods.
	// Must contain at least one entry named "egress" (VAP-enforced, rule 05.3).
	// Three-token projection paths:
	//   egress       → /var/run/keese/tokens/egress      (Envoy AI Gateway ext_authz)
	//   supervisor   → /var/run/keese/tokens/supervisor  (ACP bridge, 08b)
	//   workflowRun  → /var/run/keese/tokens/workflowRun (NATS bridge, workflow pods only)
	// +kubebuilder:validation:MinItems=1
	AudienceTemplates []AudienceTemplate `json:"audienceTemplates"`

	// Normalization controls subject normalization after template rendering.
	// +optional
	Normalization *NormalizationConfig `json:"normalization,omitempty"`
}

// OIDCProviderStatus defines the observed state of OIDCProvider.
type OIDCProviderStatus struct {
	// ObservedGeneration is the last generation successfully reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is Active when JWKS is reachable and templates are valid; Degraded otherwise.
	// +optional
	Phase OIDCProviderPhase `json:"phase,omitempty"`

	// LastTemplateValidationTime is the UTC timestamp of the most recent successful
	// template parse.
	// +optional
	LastTemplateValidationTime metav1.Time `json:"lastTemplateValidationTime,omitempty"`

	// LastReconcileTime is the timestamp of the most recent successful reconcile.
	// +optional
	LastReconcileTime metav1.Time `json:"lastReconcileTime,omitempty"`

	// ResolvedJWKSUri is the JWKS endpoint the controller most recently used,
	// either copied from spec.jwksUri or derived via OIDC discovery against
	// spec.issuer. Caching it avoids a discovery round-trip on every reconcile.
	// Cleared when the spec changes in a way that invalidates the cache
	// (issuer or jwksUri mutation).
	// +optional
	ResolvedJWKSUri string `json:"resolvedJwksUri,omitempty"`

	// Conditions reports Ready and JWKSReachable conditions.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=oidcp,categories=keese
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Issuer",type="string",JSONPath=".spec.issuer"
// +kubebuilder:printcolumn:name="AudienceTemplates",type="string",JSONPath=".spec.audienceTemplates[*].name"
// +keese:rebac-tuple=tenant.uses_oidc_provider

// OIDCProvider is the Schema for the oidcproviders API.
// OIDCProvider is cluster-scoped and configures OIDC issuer trust for the Envoy AI Gateway
// ext_authz pipeline. Bootstrap CRs (kubernetes-default, google, github-actions, etc.) are
// created by the keese-oidcprovider-bootstrap Job at install time.
//
// Design: docs/designs/04b-projected-sa-identity.md + docs/designs/04b-ii-oidc-trust.md
// Spec: docs/specs/authz.keese.ai-v1alpha1.md
//
// tenant.uses_oidc_provider OpenFGA relation: the Tenant controller writes tuples
// (tenant:<name>#uses_oidc_provider@oidc_provider:<spec.oidc.allowedProviders[]>) per
// 04a iter-6; this controller owns OIDCProvider CRs only — tuples are written by Tenant.
type OIDCProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OIDCProviderSpec   `json:"spec,omitempty"`
	Status OIDCProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OIDCProviderList contains a list of OIDCProvider.
type OIDCProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OIDCProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OIDCProvider{}, &OIDCProviderList{})
}
