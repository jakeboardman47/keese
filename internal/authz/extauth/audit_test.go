// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package extauth_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"

	"github.com/keese-ai/keese/internal/authz/extauth"
)

// captureAudit runs Authorize for the given request and returns the single
// formatted audit log line LogAudit emitted. funcr renders key/value pairs
// the same way the production zap logger does, so asserting on this string
// is asserting on the real audit shape (rule 06: test behavior, not a mock).
func captureAudit(req *extauth.HTTPRequest, fga extauth.FGAChecker) string {
	var line string
	// Verbosity 1 so the allow path (LogAudit logs allow at V(1)) is
	// captured alongside deny (info) and error.
	log := funcr.New(func(prefix, args string) { line = args }, funcr.Options{Verbosity: 1})
	d := extauth.Authorize(context.Background(), req, extauth.NewResolver(), fga)
	extauth.LogAudit(log, extauth.AuditFromDecision(d, req, "req-123", 7*time.Millisecond))
	return line
}

func TestAudit_CrossTenant_RecordsTupleFromToDecision(t *testing.T) {
	t.Parallel()
	// A denied cross-tenant request must audit (tuple, from, to, decision)
	// per rule 05.10 — and never the SA token.
	jwt := makeJWT(map[string]any{"sub": "system:serviceaccount:alpha:ksa-wfrom"})
	bearer := "Bearer " + jwt
	req := &extauth.HTTPRequest{Headers: map[string]string{
		"authorization":                bearer,
		extauth.HeaderA2AScope:         extauth.A2AScopeCrossTenant,
		extauth.HeaderA2APeerWorkspace: "wto",
	}}
	fga := &fakeFGA{grant: ""} // deny

	line := captureAudit(req, fga)

	for _, want := range []string{
		`"decision"="deny"`,
		`"tuple"="workspace:wto#messageable_from@workspace:wfrom"`,
		`"from_workspace"="wfrom"`,
		`"to_workspace"="wto"`,
		`"reason"="` + extauth.ReasonA2ADenied + `"`,
		`"user"="service_account:ksa-wfrom"`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("audit line missing %s\n  got: %s", want, line)
		}
	}

	// Rule 05.10 / 02: the token, bearer prefix, and JWT material must
	// never appear in the audit line.
	for _, forbidden := range []string{jwt, "Bearer ", "eyJ"} {
		if strings.Contains(line, forbidden) {
			t.Errorf("audit line leaked forbidden material %q\n  got: %s", forbidden, line)
		}
	}
}

func TestAudit_CrossTenant_AllowDecision(t *testing.T) {
	t.Parallel()
	jwt := makeJWT(map[string]any{"sub": "system:serviceaccount:alpha:ksa-wfrom"})
	req := &extauth.HTTPRequest{Headers: map[string]string{
		"authorization":                "Bearer " + jwt,
		extauth.HeaderA2AScope:         extauth.A2AScopeCrossTenant,
		extauth.HeaderA2APeerWorkspace: "wto",
	}}
	fga := &fakeFGA{grant: "workspace:wto#messageable_from@workspace:wfrom"}

	line := captureAudit(req, fga)

	if !strings.Contains(line, `"decision"="allow"`) {
		t.Fatalf("expected allow decision in audit; got: %s", line)
	}
	if !strings.Contains(line, `"to_workspace"="wto"`) {
		t.Fatalf("expected to_workspace in audit; got: %s", line)
	}
}

func TestAudit_EgressPath_OmitsCrossTenantFields(t *testing.T) {
	t.Parallel()
	// A normal tool-path decision (no scope header) must NOT carry the
	// cross-tenant audit fields — they would be empty/misleading noise.
	jwt := makeJWT(map[string]any{"sub": "system:serviceaccount:alpha:ksa-wfrom"})
	req := &extauth.HTTPRequest{
		Path:    "/v1/messages",
		Headers: map[string]string{"authorization": "Bearer " + jwt},
	}
	fga := &fakeFGA{} // empty resolver → no-match, fga not consulted

	line := captureAudit(req, fga)

	for _, absent := range []string{"tuple", "from_workspace", "to_workspace"} {
		if strings.Contains(line, `"`+absent+`"=`) {
			t.Errorf("egress-path audit must omit %q; got: %s", absent, line)
		}
	}
}
