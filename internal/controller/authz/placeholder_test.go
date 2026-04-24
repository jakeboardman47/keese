// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Unit tests for detectPlaceholderIssuer — no build tag, runs in the default tier.

package authz

import (
	"testing"
)

func TestDetectPlaceholderIssuer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		issuer string
		want   bool
	}{
		// --- positive cases (placeholders present) ---
		{
			name:   "azure entra curly-brace tenant-id",
			issuer: "https://login.microsoftonline.com/{tenant-id}/v2.0",
			want:   true,
		},
		{
			name:   "okta angle-bracket domain",
			issuer: "https://<okta-domain>.okta.com",
			want:   true,
		},
		{
			name:   "keycloak angle-bracket host and realm",
			issuer: "https://<keycloak-host>/realms/<realm>",
			want:   true,
		},
		{
			name:   "generic angle-bracket single token",
			issuer: "https://idp.example.com/<tenant>",
			want:   true,
		},
		{
			name:   "generic curly-brace underscore token",
			issuer: "https://idp.example.com/{org_id}/oidc",
			want:   true,
		},

		// --- negative cases (real issuers, no placeholders) ---
		{
			name:   "azure entra real tenant id without placeholder",
			issuer: "https://login.microsoftonline.com/acme.onmicrosoft.com/v2.0",
			want:   false,
		},
		{
			name:   "real okta domain",
			issuer: "https://mycompany.okta.com",
			want:   false,
		},
		{
			name:   "real keycloak issuer",
			issuer: "https://keycloak.example.com/realms/main",
			want:   false,
		},
		{
			name:   "kubernetes default in-cluster issuer",
			issuer: "https://kubernetes.default.svc.cluster.local",
			want:   false,
		},
		{
			name:   "kubernetes default short form used in tests",
			issuer: "https://kubernetes.default.svc",
			want:   false,
		},
		{
			name:   "google accounts issuer",
			issuer: "https://accounts.google.com",
			want:   false,
		},
		{
			name:   "github actions issuer",
			issuer: "https://token.actions.githubusercontent.com",
			want:   false,
		},

		// --- edge cases ---
		{
			name:   "empty string",
			issuer: "",
			want:   false,
		},
		{
			name:   "mismatched opening curly no closing brace",
			issuer: "https://{broken-",
			want:   false,
		},
		{
			name:   "mismatched opening angle no closing bracket",
			issuer: "https://<broken-",
			want:   false,
		},
		{
			name: "single digit after brace not a word char start — no match",
			// Token must start with letter or underscore; digit-only would not match.
			issuer: "https://example.com/{1invalid}",
			want:   false,
		},
	}

	for _, tc := range tests {
		tc := tc // capture for parallel subtests
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := detectPlaceholderIssuer(tc.issuer)
			if got != tc.want {
				t.Errorf("detectPlaceholderIssuer(%q) = %v, want %v", tc.issuer, got, tc.want)
			}
		})
	}
}
