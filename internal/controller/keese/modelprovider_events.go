// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

// Event reason constants for the ModelProvider controller.
// Every recorder.Eventf call in modelprovider_controller.go must use one of
// these constants (rule 04.11). No free-text reasons; no credential material,
// tokens, or response bodies in event messages (rule 02 Logging & events).
//
// The package already defines generic ReasonReady / ReasonDegraded for the
// Memory controller, so ModelProvider reasons are prefixed to avoid a
// redeclaration in this flat package.
const (
	// ReasonModelProviderValidated is emitted when the spec passes validation.
	ReasonModelProviderValidated = "ModelProviderValidated"
	// ReasonModelProviderReady is emitted when the provider reaches the Ready phase.
	ReasonModelProviderReady = "ModelProviderReady"
	// ReasonModelProviderDegraded is emitted when the provider becomes unhealthy.
	ReasonModelProviderDegraded = "ModelProviderDegraded"
	// ReasonModelProviderDiscoveryStarted is emitted when a discovery poll begins.
	ReasonModelProviderDiscoveryStarted = "DiscoveryStarted"
	// ReasonModelProviderDiscoverySucceeded is emitted when a discovery poll
	// returns a model list.
	ReasonModelProviderDiscoverySucceeded = "DiscoverySucceeded"
	// ReasonModelProviderDiscoveryFailed is emitted when a discovery poll errors
	// or is rate-limited (429); the controller backs off and requeues.
	ReasonModelProviderDiscoveryFailed = "DiscoveryFailed"
	// ReasonModelProviderRebacSyncFailed is emitted when the credential-binding
	// ReBAC tuple write fails.
	ReasonModelProviderRebacSyncFailed = "ModelProviderRebacSyncFailed"
	// ReasonModelProviderRebacSyncSucceeded is emitted when the credential-binding
	// ReBAC tuple is confirmed written.
	ReasonModelProviderRebacSyncSucceeded = "ModelProviderRebacSyncSucceeded"
	// ReasonModelProviderRebacPurgeFailed is emitted when tuple deletion fails
	// during finalizer cleanup.
	ReasonModelProviderRebacPurgeFailed = "ModelProviderRebacPurgeFailed"
)
