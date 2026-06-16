// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// SessionStoreTuple represents a single OpenFGA relationship tuple written for a
// SessionStore. Object/Relation/User follow the OpenFGA tuple format
// ("<type>:<id>" / "<relation>" / "<type>:<id>").
type SessionStoreTuple struct {
	Object   string
	Relation string
	User     string
}

// SessionStoreRebacWriter abstracts OpenFGA tuple operations for SessionStore.
// The real implementation is wired at startup; tests inject a fake.
//
// DEFERRED: the OpenFGA authorization model has no `sessionstore` type yet (see
// dev/bootstrap/openfga/model.fga). Until rebac-modeler lands the
// `sessionstore` type with a `workspace` relation, SetupWithManager defaults to
// SessionStoreNoopRebacWriter — the +keese:rebac-tuple=sessionstore.workspace
// marker on SessionStoreSpec.WorkspaceRef records the intended write so the
// pre-commit marker check passes and the controller is ready to swap in the real
// writer with no spec change. The model change is out of this agent's scope
// (do NOT edit model.fga).
type SessionStoreRebacWriter interface {
	// Write writes (upserts) the given tuples.
	Write(ctx context.Context, tuples []SessionStoreTuple) error
	// Delete removes the given tuples. Missing tuples are silently ignored.
	Delete(ctx context.Context, tuples []SessionStoreTuple) error
}

// SessionStoreWorkspaceTuple returns the tuple recording that a SessionStore is
// bound to (owned by) a Workspace. ext_authz consults this when gating session
// reads/writes (+keese:rebac-tuple=sessionstore.workspace).
//
// The tuple shape is provisional until rebac-modeler defines the `sessionstore`
// type in the OpenFGA model; the no-op writer makes the call a safe no-op today.
func SessionStoreWorkspaceTuple(storeID, workspaceRef string) SessionStoreTuple {
	return SessionStoreTuple{
		Object:   "sessionstore:" + storeID,
		Relation: "workspace",
		User:     "workspace:" + workspaceRef,
	}
}

// SessionStoreNoopRebacWriter is a silent no-op writer used until the OpenFGA
// model gains a `sessionstore` type (and for dev/local runs without OPENFGA_API_URL).
type SessionStoreNoopRebacWriter struct{}

// Write implements SessionStoreRebacWriter.
func (SessionStoreNoopRebacWriter) Write(_ context.Context, _ []SessionStoreTuple) error {
	return nil
}

// Delete implements SessionStoreRebacWriter.
func (SessionStoreNoopRebacWriter) Delete(_ context.Context, _ []SessionStoreTuple) error {
	return nil
}
