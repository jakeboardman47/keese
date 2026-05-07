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

// TransportRebacWriter is satisfied by TransportOpenFGARebacWriter (real, production)
// and by a test-only fake (see transport_rebac_fake_test.go). The real implementation is wired at startup
// via cmd/main.go when OPENFGA_API_URL is set; otherwise TransportNoopRebacWriter
// is used so the operator boots without a live OpenFGA instance.
type TransportRebacWriter interface {
	// Sync writes (or no-ops if already present) the given tuples to OpenFGA.
	// Idempotent — calling Sync with the same tuples twice is safe.
	Sync(ctx context.Context, tuples []TransportRebacTuple) error

	// Delete removes the given tuples from OpenFGA.
	// Missing tuples are silently ignored (idempotent).
	Delete(ctx context.Context, tuples []TransportRebacTuple) error
}

// TransportNoopRebacWriter is a silent no-op TransportRebacWriter used when OpenFGA
// is not configured (dev/local run without OPENFGA_API_URL). It does not record calls.
type TransportNoopRebacWriter struct{}

func (TransportNoopRebacWriter) Sync(_ context.Context, _ []TransportRebacTuple) error  { return nil }
func (TransportNoopRebacWriter) Delete(_ context.Context, _ []TransportRebacTuple) error { return nil }

var _ TransportRebacWriter = TransportNoopRebacWriter{}

// transportOwnerTuple returns the transport.owner tuple for this transport.
// transport:<namespace>/<name>#owner@workspace:<namespace>
func transportOwnerTuple(namespace, name string) TransportRebacTuple {
	return TransportRebacTuple{
		Object:   "transport:" + namespace + "/" + name,
		Relation: "owner",
		User:     "workspace:" + namespace,
	}
}
