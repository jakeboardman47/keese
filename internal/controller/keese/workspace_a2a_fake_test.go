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

// fakeA2ARebacReconciler is a stateful test A2ARebacReconciler. It models the
// LIVE OpenFGA a2a_callable_by tuple store so tests can assert pruning: it holds
// the current grants per workspace object, and ReconcileA2A converges them to the
// desired set (writing the missing, deleting the stale) via the same pure diff
// the production reconciler uses. It records every write and delete so a test can
// assert "tuple GONE" after disable / CTA-revoke, not merely "delete was called".
type fakeA2ARebacReconciler struct {
	// live maps workspaceObj → its current set of a2a_callable_by tuples.
	live map[string]map[WorkspaceRebacTuple]struct{}
	// Written / Deleted accumulate every tuple ever written / deleted, in order.
	Written []WorkspaceRebacTuple
	Deleted []WorkspaceRebacTuple
	// err, when non-nil, is returned by every ReconcileA2A call (fail-closed test).
	err error
}

func newFakeA2ARebacReconciler() *fakeA2ARebacReconciler {
	return &fakeA2ARebacReconciler{live: map[string]map[WorkspaceRebacTuple]struct{}{}}
}

func (f *fakeA2ARebacReconciler) ReconcileA2A(_ context.Context, workspaceObj string, desired []WorkspaceRebacTuple) (int, int, error) {
	if f.err != nil {
		return 0, 0, f.err
	}
	cur := f.live[workspaceObj]
	liveSlice := make([]WorkspaceRebacTuple, 0, len(cur))
	for t := range cur {
		liveSlice = append(liveSlice, t)
	}
	toWrite, toDelete := reconcileA2ATuples(workspaceObj, liveSlice, desired)
	if cur == nil {
		cur = map[WorkspaceRebacTuple]struct{}{}
		f.live[workspaceObj] = cur
	}
	for _, t := range toWrite {
		cur[t] = struct{}{}
		f.Written = append(f.Written, t)
	}
	for _, t := range toDelete {
		delete(cur, t)
		f.Deleted = append(f.Deleted, t)
	}
	return len(toWrite), len(toDelete), nil
}

// liveTuples returns the current live a2a_callable_by tuples for workspaceObj,
// for direct "tuple present / GONE" assertions.
func (f *fakeA2ARebacReconciler) liveTuples(workspaceObj string) []WorkspaceRebacTuple {
	out := []WorkspaceRebacTuple{}
	for t := range f.live[workspaceObj] {
		out = append(out, t)
	}
	return out
}

var _ A2ARebacReconciler = &fakeA2ARebacReconciler{}
