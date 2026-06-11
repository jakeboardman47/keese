// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package extauth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/keese-ai/keese/internal/authz/extauth"
)

// a2aEndpointReq builds an HTTPRequest carrying the synchronous A2A HTTP/SSE
// endpoint discriminator (x-keese-a2a-call: true): a workspace SA token for
// fromUID (the caller W_from) + the peer header for toRef (the callee W_to).
func a2aEndpointReq(fromUID, toRef string) *extauth.HTTPRequest {
	jwt := makeJWT(map[string]any{
		"sub": "system:serviceaccount:alpha:ksa-" + fromUID,
	})
	h := map[string]string{
		"authorization":                "Bearer " + jwt,
		extauth.HeaderA2ACall:          extauth.A2ACallTrue,
		extauth.HeaderA2APeerWorkspace: toRef,
	}
	return &extauth.HTTPRequest{Headers: h}
}

// TestAuthorize_A2AEndpoint_IntraTenantSelfGrant models the intra-tenant case:
// the Workspace controller wrote the self tuple
// workspace:W#a2a_callable_by@workspace:W, so a call from W into W's own A2A
// endpoint is admitted. Tuple present → allow.
func TestAuthorize_A2AEndpoint_IntraTenantSelfGrant(t *testing.T) {
	t.Parallel()
	fga := &fakeFGA{grant: "workspace:w1#a2a_callable_by@workspace:w1"}
	req := a2aEndpointReq("w1", "w1")

	d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), fga)

	if !d.Allowed {
		t.Fatalf("expected allow; got deny reason=%q", d.Reason)
	}
	if d.Reason != extauth.ReasonAllowed {
		t.Fatalf("reason: got %q want %q", d.Reason, extauth.ReasonAllowed)
	}
	// Direction (rule 05.9): caller is the FGA user, peer is the FGA object.
	if fga.lastUser != "workspace:w1" || fga.lastObject != "workspace:w1" {
		t.Fatalf("direction: user=%q object=%q", fga.lastUser, fga.lastObject)
	}
	if fga.lastRelation != "a2a_callable_by" {
		t.Fatalf("relation: got %q want a2a_callable_by", fga.lastRelation)
	}
}

// TestAuthorize_A2AEndpoint_DeniedAfterTupleRemoved models the "fails after the
// tuple is removed" path: no matching grant in the store → deny.
func TestAuthorize_A2AEndpoint_DeniedAfterTupleRemoved(t *testing.T) {
	t.Parallel()
	// Empty grant: the a2a_callable_by tuple was removed (A2A disabled).
	fga := &fakeFGA{grant: ""}
	req := a2aEndpointReq("w1", "w2")

	d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), fga)

	if d.Allowed {
		t.Fatalf("expected deny; got allow")
	}
	if d.Reason != extauth.ReasonA2AEndpointDenied {
		t.Fatalf("reason: got %q want %q", d.Reason, extauth.ReasonA2AEndpointDenied)
	}
}

// TestAuthorize_A2AEndpoint_CrossTenantAllowedWithGrant models the cross-tenant
// case where an Approved CTA caused the controller to write
// workspace:w2#a2a_callable_by@workspace:w1 → allow.
func TestAuthorize_A2AEndpoint_CrossTenantAllowedWithGrant(t *testing.T) {
	t.Parallel()
	fga := &fakeFGA{grant: "workspace:w2#a2a_callable_by@workspace:w1"}
	req := a2aEndpointReq("w1", "w2")

	d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), fga)

	if !d.Allowed {
		t.Fatalf("expected allow; got deny reason=%q", d.Reason)
	}
	if d.Tuple != "workspace:w2#a2a_callable_by@workspace:w1" {
		t.Fatalf("audited tuple: got %q", d.Tuple)
	}
}

// TestAuthorize_A2AEndpoint_FailClosedOnFGAError asserts rule 05.4: if OpenFGA
// is unreachable, the decision is deny (Allowed=false), not allow.
func TestAuthorize_A2AEndpoint_FailClosedOnFGAError(t *testing.T) {
	t.Parallel()
	fga := &fakeFGA{err: errors.New("openfga unreachable")}
	req := a2aEndpointReq("w1", "w2")

	d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), fga)

	if d.Allowed {
		t.Fatalf("fail-closed violated: FGA error produced allow")
	}
	if d.Reason != extauth.ReasonFGAError {
		t.Fatalf("reason: got %q want %q", d.Reason, extauth.ReasonFGAError)
	}
}

// TestAuthorize_A2AEndpoint_FailClosedOnMissingToken asserts a missing/malformed
// caller token denies (no subject → cannot establish W_from).
func TestAuthorize_A2AEndpoint_FailClosedOnMissingToken(t *testing.T) {
	t.Parallel()
	fga := &fakeFGA{grant: "workspace:w2#a2a_callable_by@workspace:w1"}
	// No authorization header → subject extraction fails.
	req := &extauth.HTTPRequest{Headers: map[string]string{
		extauth.HeaderA2ACall:          extauth.A2ACallTrue,
		extauth.HeaderA2APeerWorkspace: "w2",
	}}

	d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), fga)

	if d.Allowed {
		t.Fatalf("fail-closed violated: missing token produced allow")
	}
	if d.Reason != extauth.ReasonSubjectError {
		t.Fatalf("reason: got %q want %q", d.Reason, extauth.ReasonSubjectError)
	}
	if fga.calls != 0 {
		t.Fatalf("FGA should not be called without a subject; calls=%d", fga.calls)
	}
}

// TestAuthorize_A2AEndpoint_FailClosedOnMissingPeer asserts a missing peer
// header denies (nothing to Check).
func TestAuthorize_A2AEndpoint_FailClosedOnMissingPeer(t *testing.T) {
	t.Parallel()
	fga := &fakeFGA{grant: "workspace:w2#a2a_callable_by@workspace:w1"}
	jwt := makeJWT(map[string]any{"sub": "system:serviceaccount:alpha:ksa-w1"})
	req := &extauth.HTTPRequest{Headers: map[string]string{
		"authorization":       "Bearer " + jwt,
		extauth.HeaderA2ACall: extauth.A2ACallTrue,
		// peer header omitted
	}}

	d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), fga)

	if d.Allowed {
		t.Fatalf("fail-closed violated: missing peer produced allow")
	}
	if d.Reason != extauth.ReasonA2APeerMissing {
		t.Fatalf("reason: got %q want %q", d.Reason, extauth.ReasonA2APeerMissing)
	}
}
