// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"

	"github.com/keese-ai/keese/internal/rebac"
)

// WorkspaceA2AOpenFGAReconciler is the production A2ARebacReconciler. It reads
// the live a2a_callable_by tuples for a workspace from OpenFGA, then writes the
// missing ones and deletes the stale ones so the store converges to the desired
// grant set on every reconcile.
type WorkspaceA2AOpenFGAReconciler struct {
	Client *rebac.Client
}

// ReconcileA2A reads workspace:W#a2a_callable_by@* via the OpenFGA Read API
// (filter: object=workspaceObj, relation=a2a_callable_by, user wildcard), diffs
// against desired, and applies the difference (writes then deletes). Read is the
// only API that can discover a grant the controller no longer wants — Sync/Delete
// are blind. Write/Delete are individually idempotent (already-exists / not-found
// are swallowed by rebac.Client), so a partial failure leaves a converging store.
func (w *WorkspaceA2AOpenFGAReconciler) ReconcileA2A(ctx context.Context, workspaceObj string, desired []WorkspaceRebacTuple) (int, int, error) {
	// Read every live a2a_callable_by tuple on this workspace object. The user
	// field is left empty (wildcard) so the self grant and all per-peer grants
	// come back; object+relation are pinned so we never see another workspace's
	// or another relation's tuples.
	liveTuples, err := w.Client.Read(ctx, workspaceObj, a2aCallableByRelation, "")
	if err != nil {
		return 0, 0, err
	}
	live := make([]WorkspaceRebacTuple, 0, len(liveTuples))
	for _, t := range liveTuples {
		live = append(live, WorkspaceRebacTuple{Object: t.Object, Relation: t.Relation, User: t.User})
	}

	toWrite, toDelete := reconcileA2ATuples(workspaceObj, live, desired)

	// Write missing grants first so a callable peer is never momentarily denied
	// during an add+remove churn; then prune stale ones.
	for _, t := range toWrite {
		if err := w.Client.Write(ctx, t.Object, t.Relation, t.User); err != nil {
			return 0, 0, err
		}
	}
	for _, t := range toDelete {
		if err := w.Client.Delete(ctx, t.Object, t.Relation, t.User); err != nil {
			return len(toWrite), 0, err
		}
	}
	return len(toWrite), len(toDelete), nil
}

var _ A2ARebacReconciler = (*WorkspaceA2AOpenFGAReconciler)(nil)
