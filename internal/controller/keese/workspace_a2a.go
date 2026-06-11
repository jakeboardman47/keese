// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// a2aCallableByRelation is the OpenFGA relation gating inbound A2A HTTP/SSE
// calls into a workspace (model.fga workspace.a2a_callable_by, 04a iter-7).
const a2aCallableByRelation = "a2a_callable_by"

// a2aSelfTuple returns the intra-tenant self grant
// `workspace:W#a2a_callable_by@workspace:W`. Writing this opens the workspace's
// A2A endpoint to same-tenant peers (the ext_authz Check resolves the caller's
// own workspace as the FGA user; the self grant admits it). The object and user
// are the bare Workspace name — the same identifier rebacTuplesFor uses for
// owner/editor/viewer tuples, keeping the workspace's tuple namespace internally
// consistent.
func a2aSelfTuple(ws *keesev1alpha1.Workspace) WorkspaceRebacTuple {
	wsObj := "workspace:" + ws.Name
	return WorkspaceRebacTuple{
		Object:   wsObj,
		Relation: a2aCallableByRelation,
		User:     wsObj,
	}
}

// a2aCrossTenantPeerTuple returns the per-peer grant
// `workspace:W_to#a2a_callable_by@workspace:W_from` for a single approved peer.
// W_to is this (callee) workspace; W_from is the cross-tenant caller. Written
// ONLY for peers covered by an Approved CrossTenantAgreement (see
// A2ACrossTenantResolver). peerWorkspace is the bare workspace identifier from
// the CTA's frozen WorkspaceSnapshot.
func a2aCrossTenantPeerTuple(ws *keesev1alpha1.Workspace, peerWorkspace string) WorkspaceRebacTuple {
	return WorkspaceRebacTuple{
		Object:   "workspace:" + ws.Name,
		Relation: a2aCallableByRelation,
		User:     "workspace:" + peerWorkspace,
	}
}

// noopA2ARebacReconciler is the A2ARebacReconciler used when OpenFGA is not
// configured (dev/local run without OPENFGA_API_URL). With no store to read, it
// cannot discover stale tuples, so it falls back to the write-only behavior:
// the desired grants are pushed through the WorkspaceRebacWriter and nothing is
// pruned. Pruning only matters against a live authz store, which this path lacks.
type noopA2ARebacReconciler struct {
	writer WorkspaceRebacWriter
}

func (n noopA2ARebacReconciler) ReconcileA2A(ctx context.Context, _ string, desired []WorkspaceRebacTuple) (int, int, error) {
	if len(desired) == 0 {
		return 0, 0, nil
	}
	if err := n.writer.Sync(ctx, desired); err != nil {
		return 0, 0, err
	}
	return len(desired), 0, nil
}

var _ A2ARebacReconciler = noopA2ARebacReconciler{}

// A2ACrossTenantResolver answers: which cross-tenant peer workspaces may call
// this workspace's A2A endpoint? A peer is eligible only when an Approved,
// non-expired CrossTenantAgreement references both tenants and its frozen
// WorkspaceSnapshot pairs the peer (W_from) with this workspace (W_to).
//
// Read-only against the Kubernetes API. Injected so the Workspace controller is
// testable without a live CrossTenantAgreement CRD (the fake lives in
// workspace_a2a_fake_test.go). Fail-closed: a resolver error aborts the
// reconcile (no tuple is written), and an absent/expired CTA yields no peers.
type A2ACrossTenantResolver interface {
	// ApprovedPeers returns the bare peer-workspace identifiers (W_from) that an
	// Approved CrossTenantAgreement authorizes to call this workspace's A2A
	// endpoint. tenant is the callee workspace's tenant; calleeWorkspace is its
	// bare name. Returns an empty slice (never nil-with-error) when no Approved
	// CTA covers the workspace.
	ApprovedPeers(ctx context.Context, tenant, calleeWorkspace string) ([]string, error)
}

// k8sA2ACrossTenantResolver is the production A2ACrossTenantResolver. It lists
// cluster-scoped CrossTenantAgreements and selects peers from the frozen
// WorkspaceSnapshot of every Approved, non-expired agreement whose `to` tenant
// is the callee's tenant.
type k8sA2ACrossTenantResolver struct {
	client client.Client
	// now is injectable for deterministic expiry tests; defaults to time.Now.
	now func() time.Time
}

// NewK8sA2ACrossTenantResolver builds the production resolver over the given
// client.
func NewK8sA2ACrossTenantResolver(c client.Client) A2ACrossTenantResolver {
	return &k8sA2ACrossTenantResolver{client: c, now: time.Now}
}

// ApprovedPeers lists Approved CTAs and collects the W_from peers paired with
// calleeWorkspace in each agreement's frozen snapshot.
//
// Direction: this workspace is the callee (W_to). We only honor agreements where
// spec.to.tenantRef.name == tenant, so a grant is never inferred from the
// caller's side alone (rule 05.9 asymmetry).
func (r *k8sA2ACrossTenantResolver) ApprovedPeers(ctx context.Context, tenant, calleeWorkspace string) ([]string, error) {
	var list authzv1alpha1.CrossTenantAgreementList
	if err := r.client.List(ctx, &list); err != nil {
		return nil, err
	}
	now := time.Now
	if r.now != nil {
		now = r.now
	}
	seen := map[string]struct{}{}
	var peers []string
	for i := range list.Items {
		cta := &list.Items[i]
		if cta.Status.Phase != authzv1alpha1.CRAPhaseA {
			continue // not Approved → fail-closed (no tuple)
		}
		if cta.Spec.To.TenantRef.Name != tenant {
			continue // this workspace is not the callee in this agreement
		}
		if isCTAExpired(cta, now()) {
			continue // expired → revoked, fail-closed
		}
		for _, pair := range cta.Status.WorkspaceSnapshot {
			if pair.ToWorkspace != calleeWorkspace {
				continue
			}
			if pair.FromWorkspace == "" {
				continue
			}
			if _, ok := seen[pair.FromWorkspace]; ok {
				continue
			}
			seen[pair.FromWorkspace] = struct{}{}
			peers = append(peers, pair.FromWorkspace)
		}
	}
	return peers, nil
}

// isCTAExpired reports whether the agreement's spec.expiresAt is in the past.
// An unset/unparseable expiry is treated as non-expiring (the CTA controller is
// the authority on expiry transitions; this is a defense-in-depth check so a
// stale Approved phase cannot keep a tuple alive past its window).
func isCTAExpired(cta *authzv1alpha1.CrossTenantAgreement, now time.Time) bool {
	if cta.Spec.ExpiresAt == "" {
		return false
	}
	exp, err := time.Parse(time.RFC3339, cta.Spec.ExpiresAt)
	if err != nil {
		return false
	}
	return now.After(exp)
}

var _ A2ACrossTenantResolver = &k8sA2ACrossTenantResolver{}

// A2ARebacReconciler reconciles the LIVE a2a_callable_by tuples on a single
// callee workspace object to a DESIRED set, pruning stale grants. It exists
// separately from the write-only WorkspaceRebacWriter because pruning requires
// READING the live tuple set from OpenFGA (Sync/Delete are blind writes that
// cannot discover a grant the controller no longer wants).
//
// This closes the revocation hole (rule 04.4 derived-state, rule 05.9 revocation
// SLO): on disable or CTA-revoke the desired set shrinks (to empty, or minus a
// peer), and ReconcileA2A DELETEs the now-stale tuples on the SAME reconcile —
// no waiting for Workspace teardown.
type A2ARebacReconciler interface {
	// ReconcileA2A reads every existing tuple with relation a2a_callable_by on
	// object workspaceObj, then converges the live set to desired: it writes the
	// tuples in desired that are absent, and deletes the live a2a_callable_by
	// tuples on workspaceObj that are not in desired. desired MUST contain only
	// a2a_callable_by tuples whose Object == workspaceObj. Returns the number of
	// tuples written and deleted (for status/debuggability). Idempotent: a second
	// call with the same desired set writes 0 and deletes 0.
	ReconcileA2A(ctx context.Context, workspaceObj string, desired []WorkspaceRebacTuple) (written, deleted int, err error)
}

// reconcileA2ATuples is the shared pruning algorithm used by both the OpenFGA
// implementation and the test fake. live is the current a2a_callable_by tuple
// set on workspaceObj (as read from the store); desired is the target set. It
// returns the tuples to write (desired minus live) and to delete (live minus
// desired). Pure — does no I/O, so it is unit-testable and identical across
// implementations.
func reconcileA2ATuples(workspaceObj string, live, desired []WorkspaceRebacTuple) (toWrite, toDelete []WorkspaceRebacTuple) {
	desiredSet := make(map[WorkspaceRebacTuple]struct{}, len(desired))
	for _, d := range desired {
		desiredSet[d] = struct{}{}
	}
	liveSet := make(map[WorkspaceRebacTuple]struct{}, len(live))
	for _, l := range live {
		// Defense-in-depth: only ever prune a2a_callable_by tuples on THIS
		// workspace object. Read filters by object+relation, but guard anyway so
		// a mis-scoped read can never delete an unrelated grant.
		if l.Object != workspaceObj || l.Relation != a2aCallableByRelation {
			continue
		}
		liveSet[l] = struct{}{}
		if _, ok := desiredSet[l]; !ok {
			toDelete = append(toDelete, l)
		}
	}
	for _, d := range desired {
		if _, ok := liveSet[d]; !ok {
			toWrite = append(toWrite, d)
		}
	}
	return toWrite, toDelete
}

// a2aTuplesFor computes the desired a2a_callable_by tuples for the workspace.
//
//   - A2A disabled (or spec.a2a nil) → no tuples.
//   - intra-tenant → the self tuple only (pure; no API lookup).
//   - cross-tenant → one tuple per Approved-CTA peer. The resolver enforces the
//     CTA gate (rule 05: no tuple without an Approved, non-expired agreement). A
//     resolver error is returned so the caller fails the reconcile rather than
//     writing a partial/empty grant set over a transient API error.
//
// The returned set is the DESIRED state handed to A2ARebacReconciler.ReconcileA2A,
// which reads the live set and prunes the difference. This is what makes disable
// and CTA-revoke take effect immediately (the desired set shrinks; the stale
// tuples are deleted on the same reconcile) rather than only at teardown.
func a2aTuplesFor(ctx context.Context, ws *keesev1alpha1.Workspace, resolver A2ACrossTenantResolver) ([]WorkspaceRebacTuple, error) {
	if ws.Spec.A2A == nil || !ws.Spec.A2A.Enabled {
		return nil, nil
	}
	switch ws.Spec.A2A.Scope {
	case keesev1alpha1.WorkspaceA2AScopeCrossTenant:
		tenant := ws.Spec.TenantRef.Name
		if tenant == "" {
			// No tenant → cannot establish a cross-tenant grant. Fail-closed:
			// write nothing.
			return nil, nil
		}
		peers, err := resolver.ApprovedPeers(ctx, tenant, ws.Name)
		if err != nil {
			return nil, err
		}
		tuples := make([]WorkspaceRebacTuple, 0, len(peers))
		for _, p := range peers {
			tuples = append(tuples, a2aCrossTenantPeerTuple(ws, p))
		}
		return tuples, nil
	default:
		// intra-tenant (the default). Self grant only.
		return []WorkspaceRebacTuple{a2aSelfTuple(ws)}, nil
	}
}
