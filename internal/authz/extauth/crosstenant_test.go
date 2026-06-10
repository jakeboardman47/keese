// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package extauth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/keese-ai/keese/internal/authz/extauth"
)

// fakeFGA records the (user, relation, object) it was Checked with and
// returns a scripted result. It models the directional messageable_from
// grant: only the exact (user, relation, object) triple in `grant` allows.
type fakeFGA struct {
	// grant is the single allowed tuple, keyed object#relation@user.
	grant string
	// err, when non-nil, is returned from Check (simulates OpenFGA down).
	err error

	// lastUser/lastRelation/lastObject capture what Authorize passed.
	lastUser     string
	lastRelation string
	lastObject   string
	calls        int
}

func (f *fakeFGA) Check(_ context.Context, user, relation, object string) (bool, error) {
	f.calls++
	f.lastUser, f.lastRelation, f.lastObject = user, relation, object
	if f.err != nil {
		return false, f.err
	}
	return object+"#"+relation+"@"+user == f.grant, nil
}

// a2aReq builds an HTTPRequest carrying the cross-tenant a2a discriminator:
// a workspace SA token for `fromUID` (the caller) + the peer header for
// `toRef` (the destination). scope="" omits the scope header (not a
// cross-tenant request); otherwise it sets x-keese-a2a-scope.
func a2aReq(fromUID, toRef, scope string) *extauth.HTTPRequest {
	jwt := makeJWT(map[string]any{
		"sub": "system:serviceaccount:alpha:ksa-" + fromUID,
	})
	h := map[string]string{"authorization": "Bearer " + jwt}
	if scope != "" {
		h[extauth.HeaderA2AScope] = scope
	}
	if toRef != "" {
		h[extauth.HeaderA2APeerWorkspace] = toRef
	}
	return &extauth.HTTPRequest{Headers: h}
}

func TestAuthorize_CrossTenant_AllowOnGrant(t *testing.T) {
	t.Parallel()
	// Grant: workspace:WTO#messageable_from@workspace:WFROM (W_from→W_to).
	fga := &fakeFGA{grant: "workspace:wto#messageable_from@workspace:wfrom"}
	req := a2aReq("wfrom", "wto", extauth.A2AScopeCrossTenant)

	d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), fga)

	if !d.Allowed {
		t.Fatalf("expected allow; got deny reason=%q", d.Reason)
	}
	if d.Reason != extauth.ReasonAllowed {
		t.Fatalf("reason: got %q want %q", d.Reason, extauth.ReasonAllowed)
	}
	if !d.CrossTenant {
		t.Fatalf("expected CrossTenant=true")
	}
	// Direction: caller is the FGA user, peer is the FGA object.
	if fga.lastUser != "workspace:wfrom" {
		t.Fatalf("Check user (W_from): got %q want workspace:wfrom", fga.lastUser)
	}
	if fga.lastObject != "workspace:wto" {
		t.Fatalf("Check object (W_to): got %q want workspace:wto", fga.lastObject)
	}
	if fga.lastRelation != "messageable_from" {
		t.Fatalf("Check relation: got %q want messageable_from", fga.lastRelation)
	}
	if d.Tuple != "workspace:wto#messageable_from@workspace:wfrom" {
		t.Fatalf("audit tuple: got %q", d.Tuple)
	}
	if d.CallerWorkspace != "wfrom" || d.PeerWorkspace != "wto" {
		t.Fatalf("from/to: got from=%q to=%q", d.CallerWorkspace, d.PeerWorkspace)
	}
}

func TestAuthorize_CrossTenant_DenyWhenNoGrant(t *testing.T) {
	t.Parallel()
	// Empty store: no messageable_from tuple → fail-closed deny.
	fga := &fakeFGA{grant: ""}
	req := a2aReq("wfrom", "wto", extauth.A2AScopeCrossTenant)

	d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), fga)

	if d.Allowed {
		t.Fatalf("expected fail-closed deny on absent tuple")
	}
	if d.Reason != extauth.ReasonA2ADenied {
		t.Fatalf("reason: got %q want %q", d.Reason, extauth.ReasonA2ADenied)
	}
}

func TestAuthorize_CrossTenant_DirectionAsymmetric(t *testing.T) {
	t.Parallel()
	// Grant only W_from→W_to (wfrom may message wto). The REVERSE request
	// (wto trying to reach wfrom) must be DENIED — a→b granted must NOT
	// imply b→a (rule 05.9, tuple direction is load-bearing).
	fga := &fakeFGA{grant: "workspace:wto#messageable_from@workspace:wfrom"}

	// Forward: wfrom → wto → allow.
	fwd := extauth.Authorize(context.Background(),
		a2aReq("wfrom", "wto", extauth.A2AScopeCrossTenant), extauth.NewResolver(), fga)
	if !fwd.Allowed {
		t.Fatalf("forward wfrom→wto should be allowed; reason=%q", fwd.Reason)
	}

	// Reverse: wto → wfrom → deny (no tuple workspace:wfrom#...@workspace:wto).
	rev := extauth.Authorize(context.Background(),
		a2aReqReverse("wto", "wfrom"), extauth.NewResolver(), fga)
	if rev.Allowed {
		t.Fatalf("reverse wto→wfrom MUST be denied (direction); got allow")
	}
	if rev.Reason != extauth.ReasonA2ADenied {
		t.Fatalf("reverse reason: got %q want %q", rev.Reason, extauth.ReasonA2ADenied)
	}
	// The reverse Check must have flipped user/object.
	if fga.lastUser != "workspace:wto" || fga.lastObject != "workspace:wfrom" {
		t.Fatalf("reverse direction not flipped: user=%q object=%q", fga.lastUser, fga.lastObject)
	}
}

// a2aReqReverse mints a token for the SECOND workspace as caller (the
// reverse direction). Kept separate so the sub claim's UID matches the
// caller, exercising the real ExtractSubject path.
func a2aReqReverse(fromUID, toRef string) *extauth.HTTPRequest {
	return a2aReq(fromUID, toRef, extauth.A2AScopeCrossTenant)
}

func TestAuthorize_CrossTenant_RevokedDenies(t *testing.T) {
	t.Parallel()
	// Simulate revocation: the CTA controller deleted the tuple, so the
	// store no longer grants. Same request that previously allowed now
	// fails-closed.
	granted := &fakeFGA{grant: "workspace:wto#messageable_from@workspace:wfrom"}
	revoked := &fakeFGA{grant: ""} // tuple removed

	req := a2aReq("wfrom", "wto", extauth.A2AScopeCrossTenant)

	if d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), granted); !d.Allowed {
		t.Fatalf("pre-revoke should allow; reason=%q", d.Reason)
	}
	if d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), revoked); d.Allowed {
		t.Fatalf("post-revoke must deny (fail-closed)")
	}
}

func TestAuthorize_CrossTenant_FGAErrorFailsClosed(t *testing.T) {
	t.Parallel()
	fga := &fakeFGA{err: errors.New("openfga unreachable")}
	req := a2aReq("wfrom", "wto", extauth.A2AScopeCrossTenant)

	d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), fga)

	if d.Allowed {
		t.Fatalf("FGA error must fail-closed (deny)")
	}
	if d.Reason != extauth.ReasonFGAError {
		t.Fatalf("reason: got %q want %q", d.Reason, extauth.ReasonFGAError)
	}
}

func TestAuthorize_CrossTenant_MissingPeerFailsClosed(t *testing.T) {
	t.Parallel()
	fga := &fakeFGA{grant: "workspace:wto#messageable_from@workspace:wfrom"}
	// Scope header present but no peer-workspace header — nothing to Check.
	req := a2aReq("wfrom", "", extauth.A2AScopeCrossTenant)

	d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), fga)

	if d.Allowed {
		t.Fatalf("missing peer header must fail-closed (deny)")
	}
	if d.Reason != extauth.ReasonA2APeerMissing {
		t.Fatalf("reason: got %q want %q", d.Reason, extauth.ReasonA2APeerMissing)
	}
	if fga.calls != 0 {
		t.Fatalf("must not Check when peer is missing; got %d calls", fga.calls)
	}
}

func TestAuthorize_CrossTenant_PeerRefNamespaceStripped(t *testing.T) {
	t.Parallel()
	// The sidecar may stamp `<namespace>/<name>`; the FGA object must be the
	// bare id the CTA controller writes tuples with.
	fga := &fakeFGA{grant: "workspace:wto#messageable_from@workspace:wfrom"}
	req := a2aReq("wfrom", "beta-ns/wto", extauth.A2AScopeCrossTenant)

	d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), fga)

	if !d.Allowed {
		t.Fatalf("namespaced peer ref should normalize and allow; reason=%q", d.Reason)
	}
	if fga.lastObject != "workspace:wto" {
		t.Fatalf("peer normalization: object got %q want workspace:wto", fga.lastObject)
	}
}

func TestAuthorize_CrossTenant_SubjectErrorFailsClosed(t *testing.T) {
	t.Parallel()
	fga := &fakeFGA{grant: "workspace:wto#messageable_from@workspace:wfrom"}
	// Cross-tenant scope but no Authorization header → subject extraction fails.
	req := &extauth.HTTPRequest{Headers: map[string]string{
		extauth.HeaderA2AScope:         extauth.A2AScopeCrossTenant,
		extauth.HeaderA2APeerWorkspace: "wto",
	}}

	d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), fga)

	if d.Allowed {
		t.Fatalf("subject error must fail-closed (deny)")
	}
	if d.Reason != extauth.ReasonSubjectError {
		t.Fatalf("reason: got %q want %q", d.Reason, extauth.ReasonSubjectError)
	}
	if fga.calls != 0 {
		t.Fatalf("must not Check when subject extraction fails; got %d calls", fga.calls)
	}
}

func TestAuthorize_NoScopeHeader_TakesToolPath(t *testing.T) {
	t.Parallel()
	// Without the scope header, the request is a normal tool call: it must
	// NOT trigger a messageable_from Check. With an empty resolver it
	// no-binding-matches (deny) — proving the cross-tenant branch did not run.
	fga := &fakeFGA{grant: "workspace:wto#messageable_from@workspace:wfrom"}
	req := a2aReq("wfrom", "wto", "") // no scope header

	d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), fga)

	if d.CrossTenant {
		t.Fatalf("request without scope header must not be treated as cross-tenant")
	}
	if d.Reason != extauth.ReasonNoMatch {
		t.Fatalf("expected tool-path no-match; got reason=%q", d.Reason)
	}
	if fga.calls != 0 {
		t.Fatalf("tool path with empty resolver must not Check; got %d calls", fga.calls)
	}
}
