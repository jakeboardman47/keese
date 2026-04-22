// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package transport

import "context"

// RebacTuple represents a single OpenFGA relationship tuple.
// Format mirrors the OpenFGA tuple model: object#relation@user.
type RebacTuple struct {
	// Object is the FGA object string, e.g. "transport:my-transport".
	Object string
	// Relation is the FGA relation, e.g. "owner", "messageable_from".
	Relation string
	// User is the FGA user string, e.g. "workspace:ws-a".
	User string
}

// RebacWriter is satisfied by the internal/rebac package once the OpenFGA SDK
// is added to go.mod. Until then FakeRebacWriter is used in tests.
//
// TODO(spec-followup): replace with internal/rebac.Writer once openfga-sdk is in go.mod.
type RebacWriter interface {
	// Sync writes (or no-ops if already present) the given tuples to OpenFGA.
	// Idempotent — calling Sync with the same tuples twice is safe.
	Sync(ctx context.Context, tuples []RebacTuple) error

	// Delete removes the given tuples from OpenFGA.
	// Missing tuples are silently ignored (idempotent).
	Delete(ctx context.Context, tuples []RebacTuple) error
}

// FakeRebacWriter is a no-op RebacWriter used in tests and as the default
// when no real OpenFGA client is wired. It records calls for assertion.
type FakeRebacWriter struct {
	Synced  []RebacTuple
	Deleted []RebacTuple
	// FailNext causes the next Sync call to return an error.
	FailNext bool
}

func (f *FakeRebacWriter) Sync(_ context.Context, tuples []RebacTuple) error {
	if f.FailNext {
		f.FailNext = false
		return rebacError("fake rebac sync failure")
	}
	f.Synced = append(f.Synced, tuples...)
	return nil
}

func (f *FakeRebacWriter) Delete(_ context.Context, tuples []RebacTuple) error {
	f.Deleted = append(f.Deleted, tuples...)
	return nil
}

var _ RebacWriter = &FakeRebacWriter{}

type rebacError string

func (e rebacError) Error() string { return string(e) }

// transportOwnerTuple returns the transport.owner tuple for this transport.
// transport:<namespace>/<name>#owner@workspace:<namespace>
func transportOwnerTuple(namespace, name string) RebacTuple {
	return RebacTuple{
		Object:   "transport:" + namespace + "/" + name,
		Relation: "owner",
		User:     "workspace:" + namespace,
	}
}
