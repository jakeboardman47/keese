// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import "context"

// CosignVerifier verifies a Sigstore cosign keyless OIDC signature.
// The real implementation calls the cosign SDK; the fake is used in tests.
//
// Deferred — needs the sigstore/cosign SDK in go.mod. Until that lands, the
// CrossTenantAgreement controller falls back to the FakeCosignVerifier which
// FailNext-gates test scenarios. Production cluster operators must not enable
// OIDC-keyless approvals until a real verifier is wired (rule 05.16).
type CosignVerifier interface {
	// Verify checks that token is a valid cosign keyless OIDC signature whose
	// subject matches expectedSubject. Returns nil on success, error on failure.
	Verify(ctx context.Context, token, expectedSubject string) error
}

// SATokenHmacVerifier verifies an SA-token HMAC signature (used by CI pipelines).
// HMACSATokenVerifier is the production wiring; FakeSATokenHmacVerifier is the
// test double.
type SATokenHmacVerifier interface {
	// Verify checks that token is a valid HMAC over the audience string.
	// Implementations return errSignatureVerificationFailed on mismatch.
	Verify(ctx context.Context, token, audience string) error
}

// FakeCosignVerifier is a test-double that always succeeds (or fails if FailNext is true).
type FakeCosignVerifier struct {
	// FailNext causes the next Verify call to return an error.
	FailNext bool
}

func (f *FakeCosignVerifier) Verify(_ context.Context, _, _ string) error {
	if f.FailNext {
		f.FailNext = false
		return errSignatureVerificationFailed
	}
	return nil
}

var _ CosignVerifier = &FakeCosignVerifier{}

// FakeSATokenHmacVerifier is a test-double that always succeeds (or fails if FailNext is true).
type FakeSATokenHmacVerifier struct {
	// FailNext causes the next Verify call to return an error.
	FailNext bool
}

func (f *FakeSATokenHmacVerifier) Verify(_ context.Context, _, _ string) error {
	if f.FailNext {
		f.FailNext = false
		return errSignatureVerificationFailed
	}
	return nil
}

var _ SATokenHmacVerifier = &FakeSATokenHmacVerifier{}

// errSignatureVerificationFailed is the sentinel error returned by fake verifiers
// when FailNext is set.
type sigVerifyError struct{ msg string }

func (e sigVerifyError) Error() string { return e.msg }

var errSignatureVerificationFailed = sigVerifyError{msg: "signature verification failed"}
