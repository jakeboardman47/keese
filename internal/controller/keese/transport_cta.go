// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// CTAInfo holds the minimal information about a CrossTenantAgreement needed
// for Transport admission checks.
type CTAInfo struct {
	// Name is the CRA object name.
	Name string
	// Phase is the CRA phase (e.g. "Approved").
	Phase string
	// FromTenant is spec.from.tenantRef.name.
	FromTenant string
	// ToTenant is spec.to.tenantRef.name.
	ToTenant string
}

// CTAResolver resolves CrossTenantAgreements covering a given (from-namespace, to-endpoint) pair.
// Read-only against the Kubernetes API. Injected as a dependency so the controller can
// be tested without a live API server CRD for CrossTenantAgreement.
//
// TODO(spec-followup): real implementation lists keese.ai/v1alpha1
// CrossTenantAgreements via the K8s client and checks phase=Approved + expiry.
type CTAResolver interface {
	// HasApprovedCTA returns true when an Approved CrossTenantAgreement exists that
	// covers the (fromNamespace → endpoint) pair.
	//
	// fromNamespace is the namespace of the Transport (maps to a workspace → tenant).
	// endpoint is the spec.a2a.endpoint value (maps to the peer workspace → tenant).
	HasApprovedCTA(ctx context.Context, fromNamespace, endpoint string) (bool, error)
}

// FakeCTAResolver is a CTAResolver used in tests.
type FakeCTAResolver struct {
	// Approved holds the (fromNamespace+"/"+endpoint) keys for which an Approved CTA exists.
	Approved map[string]bool
	// FailNext causes the next call to return an error.
	FailNext bool
}

func NewFakeCTAResolver() *FakeCTAResolver {
	return &FakeCTAResolver{Approved: make(map[string]bool)}
}

func (f *FakeCTAResolver) HasApprovedCTA(_ context.Context, fromNamespace, endpoint string) (bool, error) {
	if f.FailNext {
		f.FailNext = false
		return false, ctaResolveError("fake cta resolve failure")
	}
	return f.Approved[fromNamespace+"/"+endpoint], nil
}

var _ CTAResolver = &FakeCTAResolver{}

type ctaResolveError string

func (e ctaResolveError) Error() string { return string(e) }
