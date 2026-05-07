// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import "context"

// CTARebacTuple represents a single OpenFGA relationship tuple for
// CrossTenantAgreement relations (allows_messaging, messageable_from).
type CTARebacTuple struct {
	// Object is the FGA object string, e.g. "tenant:to-tenant".
	Object string
	// Relation is the FGA relation, e.g. "allows_messaging".
	Relation string
	// User is the FGA user string, e.g. "tenant:from-tenant".
	User string
}

// CTARebacWriter manages OpenFGA tuples for CrossTenantAgreement resources.
// The real implementation (CTAOpenFGARebacWriter) is wired at startup via
// cmd/main.go when OPENFGA_API_URL is set. Tests inject a fake writer (see
// crosstenanagreement_rebac_fake_test.go). When OpenFGA is unconfigured,
// CTANoopRebacWriter is used as the fallback.
type CTARebacWriter interface {
	// Sync writes (or no-ops if already present) the given tuples to OpenFGA.
	// It is idempotent — calling Sync with the same tuples twice is safe.
	Sync(ctx context.Context, tuples []CTARebacTuple) error

	// Delete removes the given tuples from OpenFGA.
	// Missing tuples are silently ignored (idempotent).
	Delete(ctx context.Context, tuples []CTARebacTuple) error
}

// CTANoopRebacWriter is a silent no-op CTARebacWriter used when OpenFGA
// is not configured (dev/local run without OPENFGA_API_URL). It does not record calls.
type CTANoopRebacWriter struct{}

func (CTANoopRebacWriter) Sync(_ context.Context, _ []CTARebacTuple) error  { return nil }
func (CTANoopRebacWriter) Delete(_ context.Context, _ []CTARebacTuple) error { return nil }

var _ CTARebacWriter = CTANoopRebacWriter{}

// craAllowsMessagingTuple returns the top-level cross-tenant messaging tuple.
// tenant:<to>#allows_messaging@tenant:<from>
func craAllowsMessagingTuple(fromTenant, toTenant string) CTARebacTuple {
	return CTARebacTuple{
		Object:   "tenant:" + toTenant,
		Relation: "allows_messaging",
		User:     "tenant:" + fromTenant,
	}
}

// craMessageableFromTuples returns the cartesian workspace-level tuples.
// workspace:<toWS>#messageable_from@workspace:<fromWS>
func craMessageableFromTuples(fromWorkspaces, toWorkspaces []string) []CTARebacTuple {
	tuples := make([]CTARebacTuple, 0, len(fromWorkspaces)*len(toWorkspaces))
	for _, from := range fromWorkspaces {
		for _, to := range toWorkspaces {
			tuples = append(tuples, CTARebacTuple{
				Object:   "workspace:" + to,
				Relation: "messageable_from",
				User:     "workspace:" + from,
			})
		}
	}
	return tuples
}
