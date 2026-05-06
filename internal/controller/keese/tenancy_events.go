// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

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
)
