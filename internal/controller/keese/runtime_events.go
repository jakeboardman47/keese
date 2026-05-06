// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Package runtime provides event reason constants for the runtime controller group.
// All recorder.Eventf calls MUST use a constant from this file (rule 04.11).
package keese

// AgentRuntime event reasons.
const (
	// ReasonRuntimeStarted is emitted when the runtime reaches Ready phase.
	ReasonRuntimeStarted = "RuntimeStarted"

	// ReasonRuntimeStopped is emitted when the runtime is cleanly stopped.
	ReasonRuntimeStopped = "RuntimeStopped"

	// ReasonProviderUnknown is emitted when spec.implementation names an unregistered provider.
	ReasonProviderUnknown = "ProviderUnknown"

	// ReasonImageVersionUnsupported is emitted when the goose imageTag is outside SupportedImageVersions.
	ReasonImageVersionUnsupported = "ImageVersionUnsupported"

	// ReasonSubAgentCleanupTimeout is emitted when sub-agent cleanup exceeds the drain budget.
	ReasonSubAgentCleanupTimeout = "SubAgentCleanupTimeout"

	// ReasonCredentialExpired is emitted when a SA token or upstream credential expires.
	ReasonCredentialExpired = "CredentialExpired"
)

// RuntimeExtension event reasons.
const (
	// ReasonExtensionTupleWritten is emitted when an OpenFGA tuple is written successfully.
	ReasonExtensionTupleWritten = "ExtensionTupleWritten"

	// ReasonExtensionTupleDeleted is emitted when an OpenFGA tuple is deleted successfully.
	ReasonExtensionTupleDeleted = "ExtensionTupleDeleted"

	// ReasonExtensionRuntimeRefInvalid is emitted when spec.runtimeRef names a missing AgentRuntime.
	ReasonExtensionRuntimeRefInvalid = "ExtensionRuntimeRefInvalid"

	// ReasonExtensionOpenFGAUnavailable is emitted when OpenFGA is unreachable during a tuple op.
	ReasonExtensionOpenFGAUnavailable = "ExtensionOpenFGAUnavailable"
)
