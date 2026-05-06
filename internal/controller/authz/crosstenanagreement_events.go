// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

// Event reason constants for the CrossTenantAgreement controller.
// All recorder.Eventf calls MUST use one of these constants — no free-text reasons.
// See .claude/rules/04-kubernetes.md §11.
const (
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
