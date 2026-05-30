// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package cosign

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeCosign writes a stub cosign binary that echoes a fixed exit
// code into the test temp dir, returning its path.
func fakeCosign(t *testing.T, exitCode int, stdout string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cosign")
	body := "#!/usr/bin/env bash\nset -eu\necho '" + stdout + "'\nexit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake cosign: %v", err)
	}
	return path
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func TestGates(t *testing.T) {
	v, err := NewVerifier(VerifierConfig{
		AllowedRegistryPrefixes: []string{"ghcr.io/keese-ai/", "registry.example/keese/"},
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	cases := []struct {
		ref  string
		want bool
	}{
		{"ghcr.io/keese-ai/keese@sha256:abc", true},
		{"ghcr.io/keese-ai/keese-bundle:v0.0.1", true},
		{"registry.example/keese/op@sha256:def", true},
		{"ghcr.io/other-org/keese@sha256:abc", false},
		{"docker.io/library/nginx@sha256:abc", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := v.Gates(tc.ref); got != tc.want {
			t.Errorf("Gates(%q) = %v; want %v", tc.ref, got, tc.want)
		}
	}
}

func TestVerify_RejectsTagOnly(t *testing.T) {
	v, err := NewVerifier(VerifierConfig{
		CosignBinary: fakeCosign(t, 0, "ok"),
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	err = v.Verify(context.Background(), "ghcr.io/keese-ai/keese:v0.0.1")
	if !errors.Is(err, ErrNotDigestPinned) {
		t.Fatalf("want ErrNotDigestPinned, got %v", err)
	}
}

func TestVerify_PassThroughOnSuccess(t *testing.T) {
	v, err := NewVerifier(VerifierConfig{
		CosignBinary: fakeCosign(t, 0, "Verification for ... ok"),
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	if err := v.Verify(context.Background(),
		"ghcr.io/keese-ai/keese@sha256:1111111111111111111111111111111111111111111111111111111111111111"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestVerify_FailsOnNonZeroExit(t *testing.T) {
	v, err := NewVerifier(VerifierConfig{
		CosignBinary: fakeCosign(t, 1, "no matching signatures"),
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	err = v.Verify(context.Background(),
		"ghcr.io/keese-ai/keese@sha256:1111111111111111111111111111111111111111111111111111111111111111")
	if !errors.Is(err, ErrSignatureCheck) {
		t.Fatalf("want ErrSignatureCheck, got %v", err)
	}
}

func TestNewVerifier_BadIdentityRegex(t *testing.T) {
	_, err := NewVerifier(VerifierConfig{
		CertificateIdentityRegexp: "[invalid",
	})
	if err == nil {
		t.Fatalf("expected error on invalid regex")
	}
}

func TestNewVerifier_DefaultsApplied(t *testing.T) {
	v, err := NewVerifier(VerifierConfig{})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	d := DefaultVerifierConfig()
	if v.cfg.CertificateIdentityRegexp != d.CertificateIdentityRegexp {
		t.Errorf("default identity regexp not applied")
	}
	if v.cfg.CertificateOIDCIssuer != d.CertificateOIDCIssuer {
		t.Errorf("default issuer not applied")
	}
	if len(v.cfg.AllowedRegistryPrefixes) == 0 {
		t.Errorf("default allowed prefixes not applied")
	}
}
