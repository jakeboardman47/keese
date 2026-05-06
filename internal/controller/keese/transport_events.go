// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

// Event reason constants for the Transport controller.
// All recorder.Eventf calls MUST use one of these constants — no free-text reasons.
// See .claude/rules/04-kubernetes.md §11.
const (
	// ReasonTransportProvisioned is emitted when a Transport transitions to Ready.
	ReasonTransportProvisioned = "TransportProvisioned"

	// ReasonTransportUnreachable is emitted when a dependency (NATS, MCP, a2a peer)
	// becomes unreachable after provisioning.
	ReasonTransportUnreachable = "TransportUnreachable"

	// ReasonTransportTypeImmutable is emitted when a spec.type mutation is attempted.
	// Rejected at admission (VAP); also recorded on reconcile guard.
	ReasonTransportTypeImmutable = "TransportTypeImmutable"

	// ReasonNATSStreamOwned is emitted when the controller takes ownership of a
	// JetStream stream via the auto-create-stream annotation.
	ReasonNATSStreamOwned = "NATSStreamOwned"

	// ReasonNATSStreamNotFound is emitted when the default (non-opt-in) mode finds
	// that the JetStream stream referenced by spec.nats.streamName does not exist.
	ReasonNATSStreamNotFound = "NATSStreamNotFound"

	// ReasonNATSStreamDeleteFailed is emitted when the controller fails to delete
	// a controller-owned JetStream stream during finalizer cleanup.
	ReasonNATSStreamDeleteFailed = "NATSStreamDeleteFailed"

	// ReasonNATSStreamConfigIgnored is emitted when spec.nats.streamConfig is set
	// but the auto-create-stream annotation is absent.
	ReasonNATSStreamConfigIgnored = "NATSStreamConfigIgnored"

	// ReasonNATSStreamMigrationRequired is emitted when a live stream config change
	// would require dual-consumer + backfill migration (P8 runbook).
	ReasonNATSStreamMigrationRequired = "NATSStreamMigrationRequired"

	// ReasonMCPRouteNotFound is emitted when spec.mcp.mcpRouteRef cannot be resolved.
	ReasonMCPRouteNotFound = "MCPRouteNotFound"

	// ReasonReferenceGrantMissing is emitted when a cross-namespace reference requires
	// a ReferenceGrant that is absent.
	ReasonReferenceGrantMissing = "ReferenceGrantMissing"

	// ReasonCertificateNotFound is emitted when spec.nats.tls.certificateRef or
	// spec.a2a.mutualTLS.certificateRef cannot be resolved.
	ReasonCertificateNotFound = "CertificateNotFound"

	// ReasonA2APeerAuthzDenied is emitted when an OpenFGA cross-tenant authz check
	// denies the messaging attempt.
	ReasonA2APeerAuthzDenied = "A2APeerAuthzDenied"

	// ReasonCrossTenantAgreementMissing is emitted when spec.a2a.scope=cross-tenant
	// but no Approved CrossTenantAgreement covers the workspace pair.
	ReasonCrossTenantAgreementMissing = "CrossTenantAgreementMissing"

	// ReasonStreamLagged is emitted when the stdio outboundQueueDepth ceiling is
	// reached and the oldest frame is dropped.
	ReasonStreamLagged = "StreamLagged"
)
