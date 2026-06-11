// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "testing"

// TestReconcileA2ATuples exercises the pure desired-vs-live diff that powers
// a2a_callable_by pruning. Disable shrinks desired to empty (prune all);
// CTA-revoke drops one peer (prune that peer only); a stable set churns nothing.
func TestReconcileA2ATuples(t *testing.T) {
	const wsObj = "workspace:w-to"
	self := WorkspaceRebacTuple{Object: wsObj, Relation: a2aCallableByRelation, User: wsObj}
	peerA := WorkspaceRebacTuple{Object: wsObj, Relation: a2aCallableByRelation, User: "workspace:peer-a"}
	peerB := WorkspaceRebacTuple{Object: wsObj, Relation: a2aCallableByRelation, User: "workspace:peer-b"}
	// A tuple on a DIFFERENT object — must never be pruned by this workspace.
	other := WorkspaceRebacTuple{Object: "workspace:other", Relation: a2aCallableByRelation, User: "workspace:peer-a"}

	tests := []struct {
		name          string
		live, desired []WorkspaceRebacTuple
		wantWrite     []WorkspaceRebacTuple
		wantDelete    []WorkspaceRebacTuple
	}{
		{
			name:       "enable from empty writes the self grant",
			live:       nil,
			desired:    []WorkspaceRebacTuple{self},
			wantWrite:  []WorkspaceRebacTuple{self},
			wantDelete: nil,
		},
		{
			name:       "disable prunes the self grant",
			live:       []WorkspaceRebacTuple{self},
			desired:    nil,
			wantWrite:  nil,
			wantDelete: []WorkspaceRebacTuple{self},
		},
		{
			name:       "cta revoke prunes only the revoked peer",
			live:       []WorkspaceRebacTuple{peerA, peerB},
			desired:    []WorkspaceRebacTuple{peerA},
			wantWrite:  nil,
			wantDelete: []WorkspaceRebacTuple{peerB},
		},
		{
			name:       "stable set churns nothing (idempotent)",
			live:       []WorkspaceRebacTuple{peerA},
			desired:    []WorkspaceRebacTuple{peerA},
			wantWrite:  nil,
			wantDelete: nil,
		},
		{
			name:       "never prunes a tuple on another workspace object",
			live:       []WorkspaceRebacTuple{other},
			desired:    nil,
			wantWrite:  nil,
			wantDelete: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotWrite, gotDelete := reconcileA2ATuples(wsObj, tc.live, tc.desired)
			if !sameTupleSet(gotWrite, tc.wantWrite) {
				t.Errorf("write set: got %v, want %v", gotWrite, tc.wantWrite)
			}
			if !sameTupleSet(gotDelete, tc.wantDelete) {
				t.Errorf("delete set: got %v, want %v", gotDelete, tc.wantDelete)
			}
		})
	}
}

// sameTupleSet reports set equality ignoring order.
func sameTupleSet(a, b []WorkspaceRebacTuple) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[WorkspaceRebacTuple]int, len(a))
	for _, t := range a {
		seen[t]++
	}
	for _, t := range b {
		seen[t]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}
