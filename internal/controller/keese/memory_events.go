// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

// Event reason constants for the Memory and SharedMemory controllers.
// All recorder.Eventf calls must use one of these constants (rule 04.11).
// No free-text reasons and no PII or credential content in event messages.
const (
	// ReasonProvisioningStarted is emitted when the controller begins backend provisioning.
	ReasonProvisioningStarted = "ProvisioningStarted"

	// ReasonProvisioningFailed is emitted when backend provisioning returns an error.
	ReasonProvisioningFailed = "ProvisioningFailed"

	// ReasonProvisioningSucceeded is emitted when the backend has been confirmed ready.
	ReasonProvisioningSucceeded = "ProvisioningSucceeded"

	// ReasonDeprovisioningStarted is emitted when the finalizer begins cleanup.
	ReasonDeprovisioningStarted = "DeprovisioningStarted"

	// ReasonDeprovisioningFailed is emitted when backend cleanup returns an error.
	ReasonDeprovisioningFailed = "DeprovisioningFailed"

	// ReasonDeprovisioningSucceeded is emitted when cleanup completes before finalizer removal.
	ReasonDeprovisioningSucceeded = "DeprovisioningSucceeded"

	// ReasonRebacSyncFailed is emitted when OpenFGA tuple writes fail.
	ReasonRebacSyncFailed = "RebacSyncFailed"

	// ReasonRebacSyncSucceeded is emitted when OpenFGA tuples are confirmed written.
	ReasonRebacSyncSucceeded = "RebacSyncSucceeded"

	// ReasonRebacPurgeFailed is emitted when OpenFGA tuple deletion fails during cleanup.
	ReasonRebacPurgeFailed = "RebacPurgeFailed"

	// ReasonHAViolation is emitted when a Redis or Qdrant provider lacks HA replicas
	// outside a dev namespace.
	ReasonHAViolation = "HAViolation"

	// ReasonAuthzDenied is emitted when the SharedMemoryMutationAuthz VAP-equivalent
	// check rejects a sharedWith[] mutation due to missing tenant-admin role.
	ReasonAuthzDenied = "AuthzDenied"

	// ReasonDegraded is emitted when the backend reports an unhealthy state.
	ReasonDegraded = "Degraded"

	// ReasonReady is emitted when the resource transitions to the Ready phase.
	ReasonReady = "Ready"
)
