// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package cosign

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// VerifierConfig pins the keese supply-chain identity. The defaults
// match rule 05.12 + design 14a §4. Tests override these via the
// fields below.
type VerifierConfig struct {
	// CosignBinary is the path to the cosign executable. Defaults to
	// "cosign" (resolved on PATH).
	CosignBinary string

	// CertificateIdentityRegexp must match the GitHub Actions workflow
	// that signed the image (keyless OIDC).
	CertificateIdentityRegexp string

	// CertificateOIDCIssuer must match the OIDC issuer that minted the
	// signing identity (always token.actions.githubusercontent.com for
	// keese, per rule 05.12).
	CertificateOIDCIssuer string

	// AllowedRegistryPrefixes lists registry path prefixes whose
	// images this verifier gates. Images outside the prefix list pass
	// through unconditionally (other operators' bundles are not
	// keese's supply chain to verify).
	AllowedRegistryPrefixes []string

	// VerifyTimeout caps each cosign invocation. Defaults to 30s.
	VerifyTimeout time.Duration
}

// DefaultVerifierConfig returns the production config: identity
// pinned to keese-ai's GitHub Actions workflows, GitHub OIDC issuer,
// keese-ai ghcr.io prefix.
func DefaultVerifierConfig() VerifierConfig {
	return VerifierConfig{
		CosignBinary:              "cosign",
		CertificateIdentityRegexp: `^https://github\.com/keese-ai/keese/\.github/workflows/.*$`,
		CertificateOIDCIssuer:     "https://token.actions.githubusercontent.com",
		AllowedRegistryPrefixes:   []string{"ghcr.io/keese-ai/"},
		VerifyTimeout:             30 * time.Second,
	}
}

// Verifier shells out to the cosign binary to verify image
// signatures. It is concurrency-safe; cosign manages its own
// per-call state.
type Verifier struct {
	cfg VerifierConfig
}

// NewVerifier returns a Verifier with the supplied config. Empty
// fields fall back to DefaultVerifierConfig values where sensible.
func NewVerifier(cfg VerifierConfig) (*Verifier, error) {
	d := DefaultVerifierConfig()
	if cfg.CosignBinary == "" {
		cfg.CosignBinary = d.CosignBinary
	}
	if cfg.CertificateIdentityRegexp == "" {
		cfg.CertificateIdentityRegexp = d.CertificateIdentityRegexp
	}
	if cfg.CertificateOIDCIssuer == "" {
		cfg.CertificateOIDCIssuer = d.CertificateOIDCIssuer
	}
	if cfg.VerifyTimeout == 0 {
		cfg.VerifyTimeout = d.VerifyTimeout
	}
	if len(cfg.AllowedRegistryPrefixes) == 0 {
		cfg.AllowedRegistryPrefixes = d.AllowedRegistryPrefixes
	}
	if _, err := regexp.Compile(cfg.CertificateIdentityRegexp); err != nil {
		return nil, fmt.Errorf("invalid certificate-identity-regexp: %w", err)
	}
	return &Verifier{cfg: cfg}, nil
}

// Gates returns true if imageRef sits behind one of the configured
// registry prefixes — i.e. the verifier takes responsibility for
// signature verification on it.
func (v *Verifier) Gates(imageRef string) bool {
	for _, p := range v.cfg.AllowedRegistryPrefixes {
		if strings.HasPrefix(imageRef, p) {
			return true
		}
	}
	return false
}

// Errors returned by Verify. Tests + the handler discriminate on
// these; callers should errors.Is rather than string-match.
var (
	ErrNotDigestPinned = errors.New("image is not digest-pinned (must end in @sha256:…)")
	ErrSignatureCheck  = errors.New("cosign signature verification failed")
)

// Verify runs cosign verify against imageRef. It enforces:
//
//   - Digest pinning: imageRef must contain "@sha256:" — tag-only
//     refs are rejected pre-flight (rule 05.12).
//   - Keyless OIDC: --certificate-identity-regexp +
//     --certificate-oidc-issuer pinned to keese-ai workflows.
//
// On signature failure or missing signature, Verify returns
// ErrSignatureCheck wrapped with the cosign stderr tail (last 1024
// bytes) for the audit log. Callers must treat any non-nil error as
// a deny decision — no fallback paths.
func (v *Verifier) Verify(ctx context.Context, imageRef string) error {
	if !strings.Contains(imageRef, "@sha256:") {
		return fmt.Errorf("%w: %s", ErrNotDigestPinned, imageRef)
	}

	cctx, cancel := context.WithTimeout(ctx, v.cfg.VerifyTimeout)
	defer cancel()

	args := []string{
		"verify",
		"--certificate-identity-regexp", v.cfg.CertificateIdentityRegexp,
		"--certificate-oidc-issuer", v.cfg.CertificateOIDCIssuer,
		imageRef,
	}
	cmd := exec.CommandContext(cctx, v.cfg.CosignBinary, args...) //nolint:gosec
	out, err := cmd.CombinedOutput()
	if err != nil {
		tail := string(out)
		if len(tail) > 1024 {
			tail = tail[len(tail)-1024:]
		}
		return fmt.Errorf("%w: %s: %s", ErrSignatureCheck, err.Error(), tail)
	}
	return nil
}
