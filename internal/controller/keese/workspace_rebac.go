// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// WorkspaceRebacTuple represents a single OpenFGA relationship tuple.
// Format mirrors the OpenFGA tuple model: object#relation@user.
type WorkspaceRebacTuple struct {
	// Object is the FGA object string, e.g. "workspace:my-ws".
	Object string
	// Relation is the FGA relation, e.g. "owner", "editor", "viewer".
	Relation string
	// User is the FGA user string, e.g. "user:alice" or "service_account:ksa-uid".
	User string
}

// WorkspaceRebacWriter is satisfied by the internal/rebac package once the OpenFGA SDK
// is added to go.mod. Until then, the fake implementation (WorkspaceFakeRebacWriter) is
// used in tests and the real wire-up is deferred.
//
// TODO(spec-followup): replace with internal/rebac.Writer once openfga-sdk is in go.mod.
type WorkspaceRebacWriter interface {
	// Sync writes (or no-ops if already present) the given tuples to OpenFGA.
	// It is idempotent — calling Sync with the same tuples twice is safe.
	Sync(ctx context.Context, tuples []WorkspaceRebacTuple) error

	// Delete removes the given tuples from OpenFGA.
	// Missing tuples are silently ignored (idempotent).
	Delete(ctx context.Context, tuples []WorkspaceRebacTuple) error
}

// WorkspaceFakeRebacWriter is a no-op WorkspaceRebacWriter used in tests and as the default
// when no real OpenFGA client is wired. It records calls for assertion.
type WorkspaceFakeRebacWriter struct {
	Synced  []WorkspaceRebacTuple
	Deleted []WorkspaceRebacTuple
}

func (f *WorkspaceFakeRebacWriter) Sync(_ context.Context, tuples []WorkspaceRebacTuple) error {
	f.Synced = append(f.Synced, tuples...)
	return nil
}

func (f *WorkspaceFakeRebacWriter) Delete(_ context.Context, tuples []WorkspaceRebacTuple) error {
	f.Deleted = append(f.Deleted, tuples...)
	return nil
}

var _ WorkspaceRebacWriter = &WorkspaceFakeRebacWriter{}

// sessionAttachedByTuple returns the OpenFGA tuple that records the attaching
// identity for a WorkspaceSession:
//
//	session:<sessionUID>#attached_by@<attachSubject>
//
// It is written on the Active transition and deleted on Terminating.
// The attachSubject already carries the FGA user-type prefix (e.g. "user:alice@example.com").
func sessionAttachedByTuple(sessionUID, attachSubject string) WorkspaceRebacTuple {
	return WorkspaceRebacTuple{
		Object:   "session:" + sessionUID,
		Relation: "attached_by",
		User:     attachSubject,
	}
}

// WriteSessionAttachedBy is a convenience wrapper that syncs the attached_by
// tuple for the given session UID + subject.
func WriteSessionAttachedBy(ctx context.Context, w WorkspaceRebacWriter, sessionUID, attachSubject string) error {
	return w.Sync(ctx, []WorkspaceRebacTuple{sessionAttachedByTuple(sessionUID, attachSubject)})
}

// DeleteSessionAttachedBy is a convenience wrapper that removes the attached_by
// tuple for the given session UID + subject.
func DeleteSessionAttachedBy(ctx context.Context, w WorkspaceRebacWriter, sessionUID, attachSubject string) error {
	return w.Delete(ctx, []WorkspaceRebacTuple{sessionAttachedByTuple(sessionUID, attachSubject)})
}
