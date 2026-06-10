// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package extauth

import (
	"context"
	"strings"
)

// Cross-tenant a2a discriminator headers.
//
// A request reaching ext_authz is one of two shapes:
//
//  1. LLM / MCP egress — matched against a ToolBinding/WorkspaceTool and
//     resolved to `tool:<name>#can_call@<subject>` (the EH4 path in check.go).
//  2. Cross-tenant a2a message — the a2a/NATS transport sidecar (design 09
//     `spec.a2a.scope: cross-tenant`) fronts the peer call and stamps these
//     `x-keese-a2a-*` headers. ext_authz then resolves the cross-tenant trust
//     tuple `workspace:<W_to>#messageable_from@workspace:<W_from>` instead.
//
// The presence of HeaderA2AScope with value A2AScopeCrossTenant is the sole
// discriminator. It follows the existing `x-keese-*` header convention
// (egress-authz-protocol spec §1; main.go emits `x-keese-tool` /
// `x-keese-workspace`). Intra-tenant a2a never reaches this path — per design
// 09 §spec.a2a, topic existence in `keese.tenant.<uid>.wf.<run-uid>.*` is
// authz, so the sidecar does not set the scope header for it.
const (
	// HeaderA2AScope marks the request's a2a scope. The cross-tenant
	// decision path triggers only when its value is A2AScopeCrossTenant.
	HeaderA2AScope = "x-keese-a2a-scope"
	// HeaderA2APeerWorkspace carries the destination ("to") peer workspace
	// — the FGA object of the messageable_from Check. Format: the bare
	// workspace identifier the CrossTenantAgreement controller writes its
	// tuples with (the Workspace UID; see crosstenanagreement_rebac.go
	// craMessageableFromTuples), optionally prefixed `<namespace>/`.
	HeaderA2APeerWorkspace = "x-keese-a2a-peer-workspace"

	// A2AScopeCrossTenant is the only HeaderA2AScope value that triggers a
	// messageable_from Check. Any other value (including intra-tenant) is a
	// non-cross-tenant request and never reaches the cross-tenant Check.
	A2AScopeCrossTenant = "cross-tenant"
)

// Cross-tenant reason codes (extend the DENY allowlist in check.go). Kept
// distinct so audit + metrics can separate cross-tenant denials from egress
// `can_call` denials.
const (
	// ReasonA2APeerMissing fires when the scope header marks a cross-tenant
	// request but no (or an empty) peer-workspace header is present — there
	// is nothing to Check, so fail-closed.
	ReasonA2APeerMissing = "a2a_peer_workspace_missing"
	// ReasonA2ADenied is the OpenFGA-denied outcome for messageable_from
	// (no Approved CrossTenantAgreement covers the pair, or it was revoked).
	ReasonA2ADenied = "messageable_from_denied"
)

// isCrossTenantA2A reports whether the request carries the cross-tenant a2a
// discriminator. Header keys are already lowercased by the gRPC server's
// envoyRequestToHTTPRequest, matching the const definitions.
func isCrossTenantA2A(req *HTTPRequest) bool {
	return req.Headers[HeaderA2AScope] == A2AScopeCrossTenant
}

// normalizeWorkspaceRef turns a peer-workspace header value into the FGA
// object id. The CrossTenantAgreement controller writes
// `workspace:<id>#messageable_from@workspace:<id>` where <id> is the bare
// workspace identifier (no `<namespace>/` prefix — see
// craMessageableFromTuples). The a2a sidecar may stamp either the bare id or
// a `<namespace>/<name>` form, so we take the last path segment. The caller
// adds the `workspace:` type prefix.
func normalizeWorkspaceRef(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.LastIndex(v, "/"); i >= 0 {
		return v[i+1:]
	}
	return v
}

// authorizeCrossTenant resolves a cross-tenant a2a message request.
//
// Direction is load-bearing (rule 05.9): the caller (the request's own
// workspace, derived from the SA token) is the FGA *user* (W_from); the peer
// workspace from HeaderA2APeerWorkspace is the FGA *object* (W_to). This is
// the EXACT shape the CrossTenantAgreement controller writes —
// `workspace:<W_to>#messageable_from@workspace:<W_from>` (model.fga line 93;
// crosstenanagreement_rebac.go craMessageableFromTuples). A grant W_from→W_to
// does NOT imply W_to→W_from, because Check is asymmetric in (user, object).
//
// Fail-closed: a missing subject, a missing peer header, an FGA error, or an
// FGA deny all return Allowed=false (deny 403), mirroring the can_call path.
func authorizeCrossTenant(ctx context.Context, req *HTTPRequest, fga FGAChecker) *Decision {
	// Caller (W_from): the request's own workspace, from the SA-token sub.
	// Use the default service-account-subject extraction — the a2a token is
	// a projected workspace SA token, same shape as the egress path.
	subj, err := ExtractSubject(req, "", "", "")
	if err != nil {
		return &Decision{Reason: ReasonSubjectError, CrossTenant: true}
	}

	peer := normalizeWorkspaceRef(req.Headers[HeaderA2APeerWorkspace])
	if peer == "" {
		return &Decision{
			Reason:          ReasonA2APeerMissing,
			CrossTenant:     true,
			User:            subj.User,
			Workspace:       subj.Workspace,
			CallerWorkspace: subj.Workspace.UID,
		}
	}

	fromObj := "workspace:" + subj.Workspace.UID // W_from = FGA user
	toObj := "workspace:" + peer                 // W_to   = FGA object
	d := &Decision{
		CrossTenant:     true,
		User:            subj.User,
		Workspace:       subj.Workspace,
		CallerWorkspace: subj.Workspace.UID,
		PeerWorkspace:   peer,
		// Tuple recorded for audit (rule 05.10): object#relation@user.
		Tuple: toObj + "#messageable_from@" + fromObj,
	}

	// Direction: Check(user=W_from, relation=messageable_from, object=W_to).
	allowed, err := fga.Check(ctx, fromObj, "messageable_from", toObj)
	if err != nil {
		d.Reason = ReasonFGAError
		return d
	}
	d.Allowed = allowed
	if allowed {
		d.Reason = ReasonAllowed
	} else {
		d.Reason = ReasonA2ADenied
	}
	return d
}
