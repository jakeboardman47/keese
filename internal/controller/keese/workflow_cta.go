// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// CrossTenantPeer describes a cross-tenant participant derived from a
// step's transportRef.
type CrossTenantPeer struct {
	// TransportRefName is the name of the Transport CR triggering the CTA check.
	TransportRefName string
	// PeerWorkspaceRef is the peer workspace namespace.
	PeerWorkspaceRef string
}

// CrossTenantAgreementResolver checks whether a Workflow + WorkflowRun pair
// has the required CrossTenantAgreements in place.
//
// TODO(spec-followup): The real CrossTenantAgreement CRD is not yet scaffolded.
// The Fake covers test assertions. When the CRD exists, the resolver lists
// CrossTenantAgreement objects and matches status.phase == "Approved".
type CrossTenantAgreementResolver interface {
	// ResolvePeers extracts the cross-tenant peers implied by the Workflow's
	// transportRefs that carry spec.a2a.scope: cross-tenant. Returns nil if
	// no cross-tenant steps exist.
	ResolvePeers(ctx context.Context, wf *keesev1alpha1.Workflow) ([]CrossTenantPeer, error)

	// CheckApproved verifies that an Approved CrossTenantAgreement exists
	// for each (workspaceRef, peerWorkspaceRef) pair. Returns the first
	// missing peer if any agreement is absent or not Approved, or nil if all
	// agreements are satisfied.
	CheckApproved(ctx context.Context, wfr *keesev1alpha1.WorkflowRun, peers []CrossTenantPeer) (*CrossTenantPeer, error)
}

// FakeWorkflowCTAResolver is a test-only CrossTenantAgreementResolver.
type FakeWorkflowCTAResolver struct {
	// ReturnPeers is the list of peers returned by ResolvePeers.
	ReturnPeers []CrossTenantPeer
	// MissingPeer is returned by CheckApproved to simulate a missing CTA.
	// nil means all agreements are approved.
	MissingPeer *CrossTenantPeer
	// Err is returned by all calls when non-nil.
	Err error
}

// ResolvePeers returns FakeWorkflowCTAResolver.ReturnPeers.
func (f *FakeWorkflowCTAResolver) ResolvePeers(_ context.Context, _ *keesev1alpha1.Workflow) ([]CrossTenantPeer, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.ReturnPeers, nil
}

// CheckApproved returns FakeWorkflowCTAResolver.MissingPeer.
func (f *FakeWorkflowCTAResolver) CheckApproved(_ context.Context, _ *keesev1alpha1.WorkflowRun, _ []CrossTenantPeer) (*CrossTenantPeer, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.MissingPeer, nil
}

// Verify FakeWorkflowCTAResolver satisfies the interface at compile time.
var _ CrossTenantAgreementResolver = (*FakeWorkflowCTAResolver)(nil)
