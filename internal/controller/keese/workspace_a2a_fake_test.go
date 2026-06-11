// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package keese

import "context"

// fakeA2ACTAResolver is a test-only A2ACrossTenantResolver. It returns a fixed
// set of approved peer-workspace ids keyed by (tenant, calleeWorkspace), or an
// error to exercise the controller's fail-closed reconcile path.
type fakeA2ACTAResolver struct {
	// peers maps "<tenant>/<calleeWorkspace>" → approved peer workspace ids.
	peers map[string][]string
	// err, when non-nil, is returned by every ApprovedPeers call.
	err error
}

func newFakeA2ACTAResolver() *fakeA2ACTAResolver {
	return &fakeA2ACTAResolver{peers: map[string][]string{}}
}

// approve records that an Approved CTA pairs peerWorkspace (W_from) with the
// given callee workspace (W_to) in the callee's tenant.
func (f *fakeA2ACTAResolver) approve(tenant, calleeWorkspace, peerWorkspace string) {
	k := tenant + "/" + calleeWorkspace
	f.peers[k] = append(f.peers[k], peerWorkspace)
}

func (f *fakeA2ACTAResolver) ApprovedPeers(_ context.Context, tenant, calleeWorkspace string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.peers[tenant+"/"+calleeWorkspace], nil
}

var _ A2ACrossTenantResolver = &fakeA2ACTAResolver{}
