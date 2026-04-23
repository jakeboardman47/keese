// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TenantPhase represents the lifecycle phase of a Tenant.
// +kubebuilder:validation:Enum=Pending;Provisioning;Active;Suspended;Terminating
type TenantPhase string

const (
	TenantPhasePending      TenantPhase = "Pending"
	TenantPhaseProvisioning TenantPhase = "Provisioning"
	TenantPhaseActive       TenantPhase = "Active"
	TenantPhaseSuspended    TenantPhase = "Suspended"
	TenantPhaseTerminating  TenantPhase = "Terminating"
)

// TenantSubject references a Kubernetes or OIDC user/group/service-account.
type TenantSubject struct {
	// Kind of the subject — User, Group, or ServiceAccount.
	// +kubebuilder:validation:Enum=User;Group;ServiceAccount
	Kind string `json:"kind"`

	// Name is the OIDC email, group name, or service-account name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// CapsuleTenantRef refers to an existing capsule.clastix.io/v1beta2/Tenant.
type CapsuleTenantRef struct {
	// Name of the Capsule Tenant. Immutable while any namespace is live.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="capsuleTenantRef.name is immutable"
	Name string `json:"name"`
}

// CrossNamespaceObjectRef is a namespace-qualified object reference.
type CrossNamespaceObjectRef struct {
	// Name of the referenced object.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace where the referenced object lives.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
}

// TenantOIDCConfig controls which OIDCProvider CRs are accepted for this tenant.
type TenantOIDCConfig struct {
	// AllowedProviders lists OIDCProvider names (in authz.operator.keese.ai) accepted for token
	// validation in this tenant. Empty means all configured providers are accepted.
	// Tokens from issuers not in the allow-list are rejected 403 OIDCProviderNotFound.
	//
	// +keese:rebac-tuple=tenant.uses_oidc_provider
	// +optional
	// +listType=set
	AllowedProviders []string `json:"allowedProviders,omitempty"`
}

// TenantSecurityConfig holds tenant-level security overrides.
type TenantSecurityConfig struct {
	// AllowUnsafeTransports enables non-TLS transport (break-glass; design 09 cross-cut).
	// Requires namespace label keese.ai/break-glass=true.
	// +optional
	AllowUnsafeTransports bool `json:"allowUnsafeTransports,omitempty"`
}

// RetryBudget constrains per-call retry behaviour.
// +kubebuilder:validation:XValidation:rule="self.perCallTimeout == '' || duration(self.perCallTimeout) >= duration('1s')",message="perCallTimeout must be >= 1s when set"
type RetryBudget struct {
	// MaxRetries is the maximum number of retries per call. 0 = no retries.
	// +kubebuilder:validation:Minimum=0
	MaxRetries int32 `json:"maxRetries"`

	// PerCallTimeout is the per-attempt timeout as a duration string (e.g. "30s").
	// +kubebuilder:validation:Pattern=`^([0-9]+(s|m|h))+$`
	// +optional
	PerCallTimeout string `json:"perCallTimeout,omitempty"`
}

// TenantSpec defines the desired state of Tenant.
//
// Finalizers:
//   - finalizers.tenant.operator.keese.ai/workspaces  — prevent deletion while Workspaces live
//   - finalizers.tenant.operator.keese.ai/namespaces  — prevent label removal while Workspaces live
//   - finalizers.tenant.operator.keese.ai/agreements  — drain CrossTenantAgreements before delete
//
// +kubebuilder:validation:XValidation:rule="size(self.adminSubjects) > 0",message="adminSubjects must be non-empty"
// +kubebuilder:validation:XValidation:rule="!has(self.jwksCacheFailOpenSeconds) || self.jwksCacheFailOpenSeconds == 0 || (self.jwksCacheFailOpenSeconds >= 30 && self.jwksCacheFailOpenSeconds <= 600)",message="jwksCacheFailOpenSeconds must be in [30,600] when set"
type TenantSpec struct {
	// CapsuleTenantRef delegates namespace aggregation to a Capsule Tenant (Mode B).
	// Immutable while any namespace is live. When set, namespaceSelector is silently
	// ignored with a warning event NamespaceSelectorIgnoredInModeB.
	// +optional
	CapsuleTenantRef *CapsuleTenantRef `json:"capsuleTenantRef,omitempty"`

	// NamespaceSelector aggregates namespaces by label (Mode A). Ignored in Mode B.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// AdminSubjects are the users/groups with admin rights on this tenant.
	// At least one entry is required (VAP-enforced).
	//
	// +keese:rebac-tuple=tenant:T#admin@user:U
	// +kubebuilder:validation:MinItems=1
	AdminSubjects []TenantSubject `json:"adminSubjects"`

	// DefaultGuardrailBindings lists names of GuardrailBinding CRs inherited by workspaces.
	// +optional
	// +listType=set
	DefaultGuardrailBindings []string `json:"defaultGuardrailBindings,omitempty"`

	// TokenBudgetRef references the TokenBudget governing this tenant's aggregate spend.
	// Must resolve when set (webhook-enforced).
	// +optional
	TokenBudgetRef *CrossNamespaceObjectRef `json:"tokenBudgetRef,omitempty"`

	// CredentialPoolRef references the credential pool for this tenant.
	// Must resolve when set (webhook-enforced).
	// +optional
	CredentialPoolRef *CrossNamespaceObjectRef `json:"credentialPoolRef,omitempty"`

	// DefaultWorkspaceQuota sets the ResourceList applied as default quota to each workspace.
	// +optional
	DefaultWorkspaceQuota corev1.ResourceList `json:"defaultWorkspaceQuota,omitempty"`

	// DedicatedGateway provisions a per-tenant Envoy AI Gateway instance.
	// Cannot be toggled while status.namespaces[] is non-empty (VAP-enforced).
	// +optional
	DedicatedGateway bool `json:"dedicatedGateway,omitempty"`

	// OIDC controls which OIDCProviders are accepted for this tenant.
	// +optional
	OIDC *TenantOIDCConfig `json:"oidc,omitempty"`

	// Security holds break-glass and transport security overrides.
	// +optional
	Security *TenantSecurityConfig `json:"security,omitempty"`

	// DefaultRetryBudget constrains per-call retry policy for workspaces in this tenant.
	// +optional
	DefaultRetryBudget *RetryBudget `json:"defaultRetryBudget,omitempty"`

	// ArtifactStoreRef references the artifact store for this tenant.
	// Must resolve when set (webhook-enforced).
	// +optional
	ArtifactStoreRef *CrossNamespaceObjectRef `json:"artifactStoreRef,omitempty"`

	// JWKSCacheFailOpenSeconds controls how long the gateway serves cached JWKS on fetch
	// failure. Range [30,600]; 0 = let webhook apply default (300 for shared gateway, 60
	// for dedicated). VAP-enforced range.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=600
	// +optional
	JWKSCacheFailOpenSeconds int32 `json:"jwksCacheFailOpenSeconds,omitempty"`

	// AuditArgumentsRedacted opts in to redacting agent call arguments in audit logs
	// (PII-safe default is false).
	// +optional
	AuditArgumentsRedacted bool `json:"auditArgumentsRedacted,omitempty"`
}

// TenantStatus defines the observed state of Tenant.
type TenantStatus struct {
	// ObservedGeneration is the last generation successfully reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is the lifecycle phase of the tenant.
	// +optional
	Phase TenantPhase `json:"phase,omitempty"`

	// Conditions reports readiness and provisioning state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastReconcileTime is the timestamp of the most recent successful reconcile.
	// +optional
	LastReconcileTime metav1.Time `json:"lastReconcileTime,omitempty"`

	// Namespaces is the observed namespace list.
	// Mode A: label-derived. Mode B: mirrored from Capsule Tenant.
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`

	// CapsuleTenantResolved indicates whether the Capsule Tenant named in spec.capsuleTenantRef
	// has been found and is being tracked (Mode B only).
	// +optional
	CapsuleTenantResolved bool `json:"capsuleTenantResolved,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=tenant,categories=keese
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Namespaces",type="integer",JSONPath=".status.namespaces"

// Tenant is the Schema for the tenants API.
// Tenant is cluster-scoped; its name IS the OpenFGA identity key.
// Design: docs/designs/24-tenant-crd.md + docs/designs/24b-tenant-crd.md
// Spec: docs/specs/tenancy.operator.keese.ai-v1alpha1-ii-tenant.md
type Tenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantSpec   `json:"spec,omitempty"`
	Status TenantStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TenantList contains a list of Tenant.
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Tenant `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Tenant{}, &TenantList{})
}
