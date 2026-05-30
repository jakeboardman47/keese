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
}

// Reason codes for DENY.
const (
	ReasonAllowed       = "allowed"
	ReasonNoMatch       = "no_binding_matched"
	ReasonSubjectError  = "subject_extraction_failed"
	ReasonFGADenied     = "openfga_denied"
	ReasonFGAError      = "openfga_check_error"
)

// Authorize is the orchestration function: resolve binding → extract
// subject → call FGA → return decision. Pure function over
// dependencies passed in; the gRPC server wires the deps once and
// calls this per request.
func Authorize(ctx context.Context, req *HTTPRequest, resolver *Resolver, fga FGAChecker) *Decision {
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
