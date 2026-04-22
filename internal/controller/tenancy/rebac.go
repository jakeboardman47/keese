// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package tenancy

import "context"

// RebacTuple represents a single OpenFGA relationship tuple.
// Format mirrors the OpenFGA tuple model: object#relation@user.
type RebacTuple struct {
	// Object is the FGA object string, e.g. "tenant:my-tenant".
	Object string
	// Relation is the FGA relation, e.g. "admin", "member", "allows_messaging".
	Relation string
	// User is the FGA user string, e.g. "user:alice@example.com" or "tenant:other".
	User string
}

// RebacWriter is satisfied by the internal/rebac package once the OpenFGA SDK
// is added to go.mod. Until then, FakeRebacWriter is used in tests and the
// real wire-up is deferred.
//
// TODO(spec-followup): replace with internal/rebac.Writer once openfga-sdk is in go.mod.
type RebacWriter interface {
	// Sync writes (or no-ops if already present) the given tuples to OpenFGA.
	// It is idempotent — calling Sync with the same tuples twice is safe.
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
}

func (f *FakeRebacWriter) Sync(_ context.Context, tuples []RebacTuple) error {
	f.Synced = append(f.Synced, tuples...)
	return nil
}

func (f *FakeRebacWriter) Delete(_ context.Context, tuples []RebacTuple) error {
	f.Deleted = append(f.Deleted, tuples...)
	return nil
}

var _ RebacWriter = &FakeRebacWriter{}

// tenantAdminTuples computes the admin tuples for a Tenant given its name and
// admin subjects. One tuple per subject: tenant:<name>#admin@user:<subject>.
func tenantAdminTuples(tenantName string, adminSubjects []adminSubject) []RebacTuple {
	tuples := make([]RebacTuple, 0, len(adminSubjects))
	for _, s := range adminSubjects {
		tuples = append(tuples, RebacTuple{
			Object:   "tenant:" + tenantName,
			Relation: "admin",
			User:     "user:" + s.Name,
		})
	}
	return tuples
}

// tenantOIDCProviderTuples computes uses_oidc_provider tuples for each allowed provider.
// tenant:<name>#uses_oidc_provider@oidc_provider:<provider>
func tenantOIDCProviderTuples(tenantName string, allowedProviders []string) []RebacTuple {
	tuples := make([]RebacTuple, 0, len(allowedProviders))
	for _, p := range allowedProviders {
		tuples = append(tuples, RebacTuple{
			Object:   "tenant:" + tenantName,
			Relation: "uses_oidc_provider",
			User:     "oidc_provider:" + p,
		})
	}
	return tuples
}

// craAllowsMessagingTuple returns the top-level cross-tenant messaging tuple.
// tenant:<to>#allows_messaging@tenant:<from>
func craAllowsMessagingTuple(fromTenant, toTenant string) RebacTuple {
	return RebacTuple{
		Object:   "tenant:" + toTenant,
		Relation: "allows_messaging",
		User:     "tenant:" + fromTenant,
	}
}

// craMessageableFromTuples returns the cartesian workspace-level tuples.
// workspace:<toWS>#messageable_from@workspace:<fromWS>
func craMessageableFromTuples(fromWorkspaces, toWorkspaces []string) []RebacTuple {
	tuples := make([]RebacTuple, 0, len(fromWorkspaces)*len(toWorkspaces))
	for _, from := range fromWorkspaces {
		for _, to := range toWorkspaces {
			tuples = append(tuples, RebacTuple{
				Object:   "workspace:" + to,
				Relation: "messageable_from",
				User:     "workspace:" + from,
			})
		}
	}
	return tuples
}

// adminSubject is a minimal local struct mirroring TenantSubject for rebac helpers
// to avoid circular imports between this file and types.
type adminSubject struct {
	Name string
}
