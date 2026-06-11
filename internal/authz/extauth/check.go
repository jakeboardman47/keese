// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package extauth

import (
	"context"
)

// FGAChecker is the narrow interface keese-authz needs from
// rebac.Client. Decoupling lets tests inject a fake without pulling
// in the whole OpenFGA SDK.
type FGAChecker interface {
	Check(ctx context.Context, user, relation, object string) (bool, error)
}

// Decision is the binary outcome of a Check call plus the metadata
// needed by audit + the gRPC response.
type Decision struct {
	Allowed       bool
	BindingName   string
	BindingNS     string
	FinalToolName string
	User          string
	Workspace     WorkspaceID
	// Reason is a short token identifying why DENY fired (no match,
	// FGA denied, FGA error, subject error). Used in audit + the
	// gRPC `permission_denied` body. Never includes user-controlled
	// strings.
	Reason string

	// Cross-tenant a2a fields (set only on the messageable_from path;
	// see crosstenant.go). CrossTenant flags the decision shape so
	// audit can record (tuple, SA, from, to, decision) per rule 05.10.
	CrossTenant bool
	// CallerWorkspace is the "from" workspace (the request's own
	// workspace UID = FGA user W_from).
	CallerWorkspace string
	// PeerWorkspace is the "to" workspace (the destination peer = FGA
	// object W_to).
	PeerWorkspace string
	// Tuple is the canonical OpenFGA tuple string the cross-tenant
	// decision evaluated, `object#relation@user`. Audited verbatim;
	// it is built only from validated workspace ids, never tokens.
	Tuple string
}

// Reason codes for DENY.
const (
	ReasonAllowed      = "allowed"
	ReasonNoMatch      = "no_binding_matched"
	ReasonSubjectError = "subject_extraction_failed"
	ReasonFGADenied    = "openfga_denied"
	ReasonFGAError     = "openfga_check_error"
)

// Authorize is the orchestration function: resolve binding → extract
// subject → call FGA → return decision. Pure function over
// dependencies passed in; the gRPC server wires the deps once and
// calls this per request.
func Authorize(ctx context.Context, req *HTTPRequest, resolver *Resolver, fga FGAChecker) *Decision {
	// Step 0a: synchronous A2A HTTP/SSE endpoint calls (E2) carry the
	// x-keese-a2a-call discriminator. They are NOT tool calls — they resolve
	// against `workspace:<W_to>#a2a_callable_by@workspace:<W_from>` instead of
	// `tool:<name>#can_call`. Checked before the NATS messageable_from path so a
	// request carrying both headers is treated as an endpoint call (the bridge
	// only stamps x-keese-a2a-call on the HTTP/SSE path). Fail-closed inside.
	if isA2AEndpointCall(req) {
		return authorizeA2AEndpoint(ctx, req, fga)
	}

	// Step 0b: cross-tenant a2a message requests carry the x-keese-a2a-scope
	// discriminator (see crosstenant.go). They are NOT tool calls — they
	// never match a ToolBinding — so they are resolved against the
	// cross-tenant trust tuple `workspace:<W_to>#messageable_from@workspace:
	// <W_from>` instead of `tool:<name>#can_call`. Fail-closed inside.
	if isCrossTenantA2A(req) {
		return authorizeCrossTenant(ctx, req, fga)
	}

	// Step 1: try resolving without a workspace filter (covers
	// cluster ToolBindings, which don't need namespace scope).
	res := resolver.Resolve(&ResolveRequest{HTTP: *req})
	if !res.Matched {
		// Step 2: extract subject + workspace, then re-resolve with
		// namespace scope so WorkspaceTools can match.
		// Use placeholder subjectFrom + workspaceFrom on the FIRST
		// extraction so we have something to scope the namespace
		// resolve. The actual binding's subjectFrom is honored on
		// the final extraction below.
		subj, err := ExtractSubject(req, "", "", "")
		if err != nil {
			return &Decision{Reason: ReasonSubjectError}
		}
		res = resolver.Resolve(&ResolveRequest{HTTP: *req, Workspace: subj.Workspace})
		if !res.Matched {
			return &Decision{
				Reason:    ReasonNoMatch,
				User:      subj.User,
				Workspace: subj.Workspace,
			}
		}
	}

	// Step 3: extract the subject using the binding's specific
	// subjectFrom / workspaceFrom config (might be JWTClaim).
	subj, err := ExtractSubject(req, res.SubjectFrom, res.WorkspaceFrom, res.JWTClaim)
	if err != nil {
		return &Decision{
			Reason:        ReasonSubjectError,
			BindingName:   res.BindingName,
			BindingNS:     res.BindingNS,
			FinalToolName: res.FinalToolName,
		}
	}

	// Step 4: call OpenFGA Check.
	allowed, err := fga.Check(ctx, subj.User, "can_call", "tool:"+res.FinalToolName)
	if err != nil {
		return &Decision{
			Reason:        ReasonFGAError,
			BindingName:   res.BindingName,
			BindingNS:     res.BindingNS,
			FinalToolName: res.FinalToolName,
			User:          subj.User,
			Workspace:     subj.Workspace,
		}
	}
	d := &Decision{
		Allowed:       allowed,
		BindingName:   res.BindingName,
		BindingNS:     res.BindingNS,
		FinalToolName: res.FinalToolName,
		User:          subj.User,
		Workspace:     subj.Workspace,
	}
	if allowed {
		d.Reason = ReasonAllowed
	} else {
		d.Reason = ReasonFGADenied
	}
	return d
}
