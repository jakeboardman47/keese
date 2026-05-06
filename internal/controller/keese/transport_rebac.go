// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// TransportRebacTuple represents a single OpenFGA relationship tuple.
// Format mirrors the OpenFGA tuple model: object#relation@user.
type TransportRebacTuple struct {
	// Object is the FGA object string, e.g. "transport:my-transport".
	Object string
	// Relation is the FGA relation, e.g. "owner", "messageable_from".
	Relation string
	// User is the FGA user string, e.g. "workspace:ws-a".
	User string
}

// TransportRebacWriter is satisfied by the internal/rebac package once the OpenFGA SDK
// is added to go.mod. Until then TransportFakeRebacWriter is used in tests.
//
// TODO(spec-followup): replace with internal/rebac.Writer once openfga-sdk is in go.mod.
type TransportRebacWriter interface {
	// Sync writes (or no-ops if already present) the given tuples to OpenFGA.
	// Idempotent — calling Sync with the same tuples twice is safe.
	Sync(ctx context.Context, tuples []TransportRebacTuple) error

	// Delete removes the given tuples from OpenFGA.
	// Missing tuples are silently ignored (idempotent).
	Delete(ctx context.Context, tuples []TransportRebacTuple) error
}

// TransportFakeRebacWriter is a no-op TransportRebacWriter used in tests and as the default
// when no real OpenFGA client is wired. It records calls for assertion.
type TransportFakeRebacWriter struct {
	Synced  []TransportRebacTuple
	Deleted []TransportRebacTuple
	// FailNext causes the next Sync call to return an error.
	FailNext bool
}

func (f *TransportFakeRebacWriter) Sync(_ context.Context, tuples []TransportRebacTuple) error {
	if f.FailNext {
		f.FailNext = false
		return transportRebacError("fake rebac sync failure")
	}
	f.Synced = append(f.Synced, tuples...)
	return nil
}

func (f *TransportFakeRebacWriter) Delete(_ context.Context, tuples []TransportRebacTuple) error {
	f.Deleted = append(f.Deleted, tuples...)
	return nil
}

var _ TransportRebacWriter = &TransportFakeRebacWriter{}

type transportRebacError string

func (e transportRebacError) Error() string { return string(e) }

// transportOwnerTuple returns the transport.owner tuple for this transport.
// transport:<namespace>/<name>#owner@workspace:<namespace>
func transportOwnerTuple(namespace, name string) TransportRebacTuple {
	return TransportRebacTuple{
		Object:   "transport:" + namespace + "/" + name,
		Relation: "owner",
		User:     "workspace:" + namespace,
	}
}
