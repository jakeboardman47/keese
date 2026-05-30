// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CrossTenantAgreementPhase represents the lifecycle phase of a CrossTenantAgreement.
// +kubebuilder:validation:Enum=Pending;Approved;Rejected;Expired
type CrossTenantAgreementPhase string

const (
	CRAPhaseP CrossTenantAgreementPhase = "Pending"
	CRAPhaseA CrossTenantAgreementPhase = "Approved"
	CRAPhaseR CrossTenantAgreementPhase = "Rejected"
	CRAPhaseE CrossTenantAgreementPhase = "Expired"
)

// A2ARole enumerates allowed agent-to-agent communication roles.
// +kubebuilder:validation:Enum=reader;writer;bidirectional
type A2ARole string

const (
	A2ARoleReader        A2ARole = "reader"
	A2ARoleWriter        A2ARole = "writer"
	A2ARoleBidirectional A2ARole = "bidirectional"
)

// SignatureType identifies how the approval signature was computed.
// +kubebuilder:validation:Enum=oidc-keyless;sa-token
type SignatureType string

const (
	SignatureTypeOIDCKeyless SignatureType = "oidc-keyless"
	SignatureTypeSAToken     SignatureType = "sa-token"
)

// TenantEndpoint references a tenant and an optional workspace selector.
type TenantEndpoint struct {
	// TenantRef references the Tenant CR participating in this agreement.
	// Immutable after creation.
	//
	// +keese:rebac-tuple=tenant.allows_messaging
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="tenantRef is immutable after creation"
	TenantRef LocalObjectRef `json:"tenantRef"`

	// WorkspaceSelector restricts which workspaces in this tenant are covered.
	// +optional
	WorkspaceSelector *metav1.LabelSelector `json:"workspaceSelector,omitempty"`
}

// LocalObjectRef references a cluster-scoped object by name only.
type LocalObjectRef struct {
	// Name of the referenced object.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// CRAScope defines the communication channels covered by the agreement.
//
// +kubebuilder:validation:XValidation:rule="self.natsSubjects.all(s, s.startsWith('keese.cta.'))",message="every natsSubject must start with 'keese.cta.'"
// +kubebuilder:validation:XValidation:rule="size(self.natsSubjects) <= 50",message="at most 50 natsSubjects allowed"
type CRAScope struct {
	// NATSSubjects lists the NATS subject patterns covered by this agreement.
	// Each must start with "keese.cta." and the list is capped at 50.
	//
	// +keese:rebac-tuple=workspace.messageable_from
	// +kubebuilder:validation:MaxItems=50
	// +listType=set
	NATSSubjects []string `json:"natsSubjects"`

	// A2ARoles lists the agent-to-agent communication roles enabled by this agreement.
	// Allowed values: reader, writer, bidirectional.
	// +kubebuilder:validation:MinItems=1
	// +listType=set
	A2ARoles []A2ARole `json:"a2aRoles"`
}

// CRAApproval records a single-tenant approval of the agreement.
type CRAApproval struct {
	// Tenant is the name of the approving tenant.
	Tenant string `json:"tenant"`

	// ApprovedBy identifies the OIDC email or ServiceAccount that approved.
	// +kubebuilder:validation:MinLength=1
	ApprovedBy string `json:"approvedBy"`

	// ApprovedAt is the UTC timestamp of the approval.
	ApprovedAt metav1.Time `json:"approvedAt"`

	// Signature is the base64-encoded approval signature.
	// Computed over (cra-uid || tenant-uid || expiresAt).
	//
	// +keese:rebac-tuple=tenant.can_approve_cra
	// +kubebuilder:validation:MinLength=1
	Signature string `json:"signature"`

	// SignatureType identifies the signature scheme used.
	SignatureType SignatureType `json:"signatureType"`
}

// WorkspaceSnapshotEntry records a from/to workspace pair frozen at Approved transition (TOFU).
type WorkspaceSnapshotEntry struct {
	// FromWorkspace is the name of the originating workspace.
	FromWorkspace string `json:"fromWorkspace"`

	// ToWorkspace is the name of the destination workspace.
	ToWorkspace string `json:"toWorkspace"`

	// SnapshotAt is the UTC timestamp when this pair was frozen.
	SnapshotAt metav1.Time `json:"snapshotAt"`
}

// CrossTenantAgreementSpec defines the desired state of CrossTenantAgreement.
//
// Finalizer:
//   - finalizers.crosstenanagreement.keese.ai/nats — triggers NATS stream deletion
//
// +kubebuilder:validation:XValidation:rule="self.from.tenantRef.name != self.to.tenantRef.name",message="from and to tenants must differ"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.expiresAt) || self.expiresAt == oldSelf.expiresAt",message="expiresAt is immutable after creation"
type CrossTenantAgreementSpec struct {
	// From identifies the originating tenant and workspace selector.
	From TenantEndpoint `json:"from"`

	// To identifies the destination tenant and workspace selector.
	To TenantEndpoint `json:"to"`

	// Scope defines the NATS subjects and A2A roles covered by this agreement.
	Scope CRAScope `json:"scope"`

	// ExpiresAt is the RFC3339 expiry timestamp. Must be in the future on create.
	// Immutable after creation (VAP-enforced).
	// +kubebuilder:validation:Pattern=`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`
	// +optional
	ExpiresAt string `json:"expiresAt,omitempty"`
}

// CrossTenantAgreementStatus defines the observed state of CrossTenantAgreement.
type CrossTenantAgreementStatus struct {
	// ObservedGeneration is the last generation successfully reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is the lifecycle phase of this agreement.
	// +optional
	Phase CrossTenantAgreementPhase `json:"phase,omitempty"`

	// Conditions reports readiness and approval state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastReconcileTime is the timestamp of the most recent successful reconcile.
	// +optional
	LastReconcileTime metav1.Time `json:"lastReconcileTime,omitempty"`

	// Approvals is an append-only list of tenant approvals (max 2; one per tenant).
	// +optional
	// +kubebuilder:validation:MaxItems=2
	Approvals []CRAApproval `json:"approvals,omitempty"`

	// WorkspaceSnapshot is frozen at the Approved transition (TOFU).
	// New workspaces matching the selector do NOT inherit the agreement automatically;
	// a WorkspaceSnapshotDrift event is emitted and a new CRA is required.
	// +optional
	WorkspaceSnapshot []WorkspaceSnapshotEntry `json:"workspaceSnapshot,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=cra,categories=keese
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Approved')].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="From",type="string",JSONPath=".spec.from.tenantRef.name"
// +kubebuilder:printcolumn:name="To",type="string",JSONPath=".spec.to.tenantRef.name"

// CrossTenantAgreement is the Schema for the crosstenanagreements API.
// CrossTenantAgreement is cluster-scoped; it governs cross-tenant NATS messaging and A2A roles.
// Design: docs/designs/25-cross-tenant-agreement.md + 25-ii-spec-schema.md + 25-iii-approval-flow.md
// Spec: docs/specs/keese.ai-v1alpha1-ii-cra.md
type CrossTenantAgreement struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CrossTenantAgreementSpec   `json:"spec,omitempty"`
	Status CrossTenantAgreementStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CrossTenantAgreementList contains a list of CrossTenantAgreement.
type CrossTenantAgreementList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CrossTenantAgreement `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CrossTenantAgreement{}, &CrossTenantAgreementList{})
}
