// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package extauth_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
	"github.com/keese-ai/keese/internal/authz/extauth"
)

// makeJWT builds an unsigned JWT with the given claims (signature
// segment is gibberish — keese-authz never verifies, that's
// jwt_authn's job).
func makeJWT(claims map[string]any) string {
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT"})
	body, _ := json.Marshal(claims)
	enc := base64.RawURLEncoding
	return enc.EncodeToString(header) + "." + enc.EncodeToString(body) + ".sigplaceholder"
}

func TestExtractSubject_Defaults(t *testing.T) {
	t.Parallel()
	jwt := makeJWT(map[string]any{
		"sub": "system:serviceaccount:alpha:ksa-abcba018",
	})
	req := &extauth.HTTPRequest{
		Headers: map[string]string{"authorization": "Bearer " + jwt},
	}
	subj, err := extauth.ExtractSubject(req, "", "", "")
	if err != nil {
		t.Fatalf("ExtractSubject: %v", err)
	}
	if subj.User != "service_account:ksa-abcba018" {
		t.Fatalf("user: got %q", subj.User)
	}
	if subj.Workspace.Namespace != "alpha" {
		t.Fatalf("namespace: got %q", subj.Workspace.Namespace)
	}
	if subj.Workspace.UID != "abcba018" {
		t.Fatalf("uid: got %q", subj.Workspace.UID)
	}
}

func TestExtractSubject_JWTClaim(t *testing.T) {
	t.Parallel()
	jwt := makeJWT(map[string]any{
		"sub":             "system:serviceaccount:alpha:ksa-abcba018",
		"keese.user":      "user:alice@example.com",
		"keese.workspace": "alpha/my-ws",
	})
	req := &extauth.HTTPRequest{
		Headers: map[string]string{"authorization": "Bearer " + jwt},
	}
	subj, err := extauth.ExtractSubject(req,
		authzv1alpha1.SubjectFromJWTClaim, authzv1alpha1.WorkspaceFromServiceAccountName, "keese.user")
	if err != nil {
		t.Fatalf("ExtractSubject: %v", err)
	}
	if subj.User != "user:alice@example.com" {
		t.Fatalf("user: got %q", subj.User)
	}
	// workspaceFrom with the same claim name reads `keese.user` —
	// we want a SECOND extraction with workspace claim. Demonstrate
	// the `<ns>/<name>` shape via a fresh call.
	subj2, err := extauth.ExtractSubject(req,
		authzv1alpha1.SubjectFromServiceAccountSubject,
		authzv1alpha1.WorkspaceFromJWTClaim, "keese.workspace")
	if err != nil {
		t.Fatalf("workspace JWTClaim: %v", err)
	}
	if subj2.Workspace.Namespace != "alpha" || subj2.Workspace.Name != "my-ws" {
		t.Fatalf("workspace: got %+v", subj2.Workspace)
	}
}

func TestExtractSubject_Errors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		headers map[string]string
		wantErr error
	}{
		{
			name:    "missing-authz",
			headers: map[string]string{},
			wantErr: extauth.ErrMissingAuthorization,
		},
		{
			name:    "no-bearer",
			headers: map[string]string{"authorization": "Basic abc"},
			wantErr: extauth.ErrMalformedJWT,
		},
		{
			name:    "garbage-jwt",
			headers: map[string]string{"authorization": "Bearer notajwt"},
			// Will be ErrMalformedJWT (single-segment string)
			wantErr: extauth.ErrMalformedJWT,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := extauth.ExtractSubject(&extauth.HTTPRequest{Headers: c.headers}, "", "", "")
			if !errors.Is(err, c.wantErr) && !strings.Contains(err.Error(), c.wantErr.Error()) {
				t.Fatalf("got %v want %v", err, c.wantErr)
			}
		})
	}
}

func TestExtractSubject_NotKeeseSA(t *testing.T) {
	t.Parallel()
	jwt := makeJWT(map[string]any{"sub": "system:serviceaccount:default:default"})
	req := &extauth.HTTPRequest{Headers: map[string]string{"authorization": "Bearer " + jwt}}
	_, err := extauth.ExtractSubject(req, "", "", "")
	if !errors.Is(err, extauth.ErrUnknownSubject) {
		t.Fatalf("got %v want ErrUnknownSubject", err)
	}
}
