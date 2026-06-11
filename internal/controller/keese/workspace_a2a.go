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

// a2aTuplesFor computes the desired a2a_callable_by tuples for the workspace.
//
//   - A2A disabled (or spec.a2a nil) → no tuples.
//   - intra-tenant → the self tuple only (pure; no API lookup).
//   - cross-tenant → one tuple per Approved-CTA peer. The resolver enforces the
//     CTA gate (rule 05: no tuple without an Approved, non-expired agreement). A
//     resolver error is returned so the caller fails the reconcile rather than
//     writing a partial/empty grant set over a transient API error.
//
// The returned tuples are merged with rebacTuplesFor's set and synced together,
// so a2a tuples participate in the same idempotent Sync + cleanup Delete.
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
