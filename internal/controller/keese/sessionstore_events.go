// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

// Event reason constants for the SessionStore controller.
// Every recorder.Eventf call in sessionstore_controller.go must use one of these
// constants (rule 04.11). No free-text reasons; no DSN/credential material,
// tokens, or response bodies in event messages (rule 02 Logging & events).
//
// Reasons are SessionStore-prefixed to avoid redeclaration in this flat package.
const (
	// ReasonSessionStoreValidated is emitted when the spec passes validation.
	ReasonSessionStoreValidated = "SessionStoreValidated"
	// ReasonSessionStoreReady is emitted when the store reaches the Ready phase.
	ReasonSessionStoreReady = "SessionStoreReady"
	// ReasonSessionStoreDegraded is emitted when the store becomes unhealthy
	// (e.g. backend ref invalid, or a SQLite store asked for >1 replica).
	ReasonSessionStoreDegraded = "SessionStoreDegraded"
	// ReasonSessionStoreMigrating is emitted when a postgres schema migration begins.
	ReasonSessionStoreMigrating = "SessionStoreMigrating"
	// ReasonSessionStoreMigrated is emitted when the postgres schema + RLS policy
	// are confirmed applied.
	ReasonSessionStoreMigrated = "SessionStoreMigrated"
	// ReasonSessionStoreMigrationFailed is emitted when the postgres migration errors.
	ReasonSessionStoreMigrationFailed = "SessionStoreMigrationFailed"
	// ReasonSessionStoreRebacSyncFailed is emitted when the workspace-binding ReBAC
	// tuple write fails.
	ReasonSessionStoreRebacSyncFailed = "SessionStoreRebacSyncFailed"
	// ReasonSessionStoreRebacSyncSucceeded is emitted when the workspace-binding
	// ReBAC tuple is confirmed written.
	ReasonSessionStoreRebacSyncSucceeded = "SessionStoreRebacSyncSucceeded"
	// ReasonSessionStoreRebacPurgeFailed is emitted when tuple deletion fails
	// during finalizer cleanup.
	ReasonSessionStoreRebacPurgeFailed = "SessionStoreRebacPurgeFailed"
)
