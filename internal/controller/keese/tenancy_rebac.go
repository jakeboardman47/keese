// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// TenantRebacTuple represents a single OpenFGA relationship tuple.
// Format mirrors the OpenFGA tuple model: object#relation@user.
type TenantRebacTuple struct {
	// Object is the FGA object string, e.g. "tenant:my-tenant".
	Object string
	// Relation is the FGA relation, e.g. "admin", "member", "allows_messaging".
	Relation string
	// User is the FGA user string, e.g. "user:alice@example.com" or "tenant:other".
	User string
}

// TenantRebacWriter is satisfied by TenantOpenFGARebacWriter (real, production)
// and by a test-only fake (see tenancy_rebac_fake_test.go). The real implementation is wired at startup
// via cmd/main.go when OPENFGA_API_URL is set; otherwise TenantNoopRebacWriter
// is used so the operator boots without a live OpenFGA instance.
type TenantRebacWriter interface {
	// Sync writes (or no-ops if already present) the given tuples to OpenFGA.
	// It is idempotent — calling Sync with the same tuples twice is safe.
	Sync(ctx context.Context, tuples []TenantRebacTuple) error

	// Delete removes the given tuples from OpenFGA.
	// Missing tuples are silently ignored (idempotent).
	Delete(ctx context.Context, tuples []TenantRebacTuple) error
}

// TenantNoopRebacWriter is a silent no-op TenantRebacWriter used when OpenFGA
// is not configured (dev/local run without OPENFGA_API_URL). It does not record calls.
type TenantNoopRebacWriter struct{}

func (TenantNoopRebacWriter) Sync(_ context.Context, _ []TenantRebacTuple) error  { return nil }
func (TenantNoopRebacWriter) Delete(_ context.Context, _ []TenantRebacTuple) error { return nil }

var _ TenantRebacWriter = TenantNoopRebacWriter{}

// tenantAdminTuples computes the admin tuples for a Tenant given its name and
// admin subjects. One tuple per subject: tenant:<name>#admin@user:<subject>.
func tenantAdminTuples(tenantName string, adminSubjects []adminSubject) []TenantRebacTuple {
	tuples := make([]TenantRebacTuple, 0, len(adminSubjects))
	for _, s := range adminSubjects {
		tuples = append(tuples, TenantRebacTuple{
			Object:   "tenant:" + tenantName,
			Relation: "admin",
			User:     "user:" + s.Name,
		})
	}
	return tuples
}

// tenantOIDCProviderTuples computes uses_oidc_provider tuples for each allowed provider.
// tenant:<name>#uses_oidc_provider@oidc_provider:<provider>
func tenantOIDCProviderTuples(tenantName string, allowedProviders []string) []TenantRebacTuple {
	tuples := make([]TenantRebacTuple, 0, len(allowedProviders))
	for _, p := range allowedProviders {
		tuples = append(tuples, TenantRebacTuple{
			Object:   "tenant:" + tenantName,
			Relation: "uses_oidc_provider",
			User:     "oidc_provider:" + p,
		})
	}
	return tuples
}

// adminSubject is a minimal local struct mirroring TenantSubject for rebac helpers
// to avoid circular imports between this file and types.
type adminSubject struct {
	Name string
}
