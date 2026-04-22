// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package tenancy

// Event reason constants for the Tenant and CrossTenantAgreement controllers.
// All recorder.Eventf calls MUST use one of these constants — no free-text reasons.
// See .claude/rules/04-kubernetes.md §11.
const (
	// --- Tenant lifecycle events ---

	// ReasonTenantProvisioned is emitted when the Tenant transitions to Active.
	ReasonTenantProvisioned = "TenantProvisioned"

	// ReasonNamespaceAdded is emitted when a namespace is added to status.namespaces[].
	ReasonNamespaceAdded = "NamespaceAdded"

	// ReasonNamespaceRemoved is emitted when a namespace is removed from status.namespaces[].
	ReasonNamespaceRemoved = "NamespaceRemoved"

	// ReasonTenantLabelLocked is emitted when a namespace label mutation is denied
	// because the Tenant finalizer is still active.
	ReasonTenantLabelLocked = "TenantLabelLocked"

	// ReasonCapsuleTenantNotFound is emitted when spec.capsuleTenantRef cannot be resolved (Mode B).
	ReasonCapsuleTenantNotFound = "CapsuleTenantNotFound"

	// ReasonRefNotResolved is emitted when any cross-namespace ref (tokenBudgetRef,
	// credentialPoolRef, artifactStoreRef) cannot be resolved.
	ReasonRefNotResolved = "RefNotResolved"

	// ReasonTenantDeletionBlocked is emitted when deletion is prevented by a finalizer
	// (workspaces, namespaces, or agreements outstanding).
	ReasonTenantDeletionBlocked = "TenantDeletionBlocked"

	// ReasonSelectorOverlapDenied is emitted when the namespace selector would overlap
	// with another Tenant's selector.
	ReasonSelectorOverlapDenied = "SelectorOverlapDenied"

	// ReasonNamespaceSelectorIgnoredInModeB is emitted when both capsuleTenantRef and
	// namespaceSelector are specified; the selector is ignored in Mode B.
	ReasonNamespaceSelectorIgnoredInModeB = "NamespaceSelectorIgnoredInModeB"

	// ReasonJWKSCacheExhausted is emitted when the JWKS fail-open window expires
	// and the gateway begins rejecting tokens.
	ReasonJWKSCacheExhausted = "JWKSCacheExhausted"

	// ReasonAuditRedactionUnavailable is emitted when auditArgumentsRedacted is true
	// but the redaction sidecar is not reachable.
	ReasonAuditRedactionUnavailable = "AuditRedactionUnavailable"

	// ReasonRebacTupleWritten is emitted when OpenFGA tuples are synced successfully.
	ReasonRebacTupleWritten = "RebacTupleWritten"

	// ReasonRebacTupleDeleteFailed is emitted when OpenFGA tuple deletion fails during cleanup.
	ReasonRebacTupleDeleteFailed = "RebacTupleDeleteFailed"

	// --- CrossTenantAgreement lifecycle events ---

	// ReasonCRAApproved is emitted when a CrossTenantAgreement transitions to Approved.
	ReasonCRAApproved = "CRAApproved"

	// ReasonCRAExpired is emitted when a CrossTenantAgreement transitions to Expired.
	ReasonCRAExpired = "CRAExpired"

	// ReasonCRARejected is emitted when a CrossTenantAgreement transitions to Rejected.
	ReasonCRARejected = "CRARejected"

	// ReasonOutOfBandTupleObserved is emitted when a pre-existing OpenFGA tuple is
	// detected for a workspace pair; the sync is a no-op and the event is audited.
	ReasonOutOfBandTupleObserved = "OutOfBandTupleObserved"

	// ReasonTupleSyncFailed is emitted when an OpenFGA Sync call fails for a CRA.
	ReasonTupleSyncFailed = "TupleSyncFailed"

	// ReasonWorkspaceSnapshotDrift is emitted when the selector-resolved workspaces
	// diverge from the frozen workspaceSnapshot (TOFU — no auto-extension).
	ReasonWorkspaceSnapshotDrift = "WorkspaceSnapshotDrift"

	// ReasonCRAApprovalInvalid is emitted when an approval annotation is present but
	// the signature verification fails or the approver lacks can_approve_cra permission.
	ReasonCRAApprovalInvalid = "CRAApprovalInvalid"

	// ReasonSignatureVerificationFailed is emitted when cosign or SA-token HMAC
	// verification returns an error.
	ReasonSignatureVerificationFailed = "SignatureVerificationFailed"

	// ReasonCRAConflict is emitted when an existing Approved CRA already covers the
	// same (from-tenant, to-tenant, workspace-pair) triplet.
	ReasonCRAConflict = "CRAConflict"

	// ReasonNATSStreamDeleted is emitted when the NATS JetStream stream for a CRA is
	// successfully deleted on finalizer cleanup.
	ReasonNATSStreamDeleted = "NATSStreamDeleted"

	// ReasonNATSStreamDeleteFailed is emitted when the NATS JetStream stream deletion fails.
	ReasonNATSStreamDeleteFailed = "NATSStreamDeleteFailed"
)
