// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package tenancy

// CosignVerifier verifies a Sigstore cosign keyless OIDC signature.
// The real implementation calls the cosign SDK; the fake is used in tests.
//
// TODO(spec-followup): implement real cosign verification once sigstore/cosign
// is added to go.mod. The expected subject is the OIDC email of the approver.
type CosignVerifier interface {
	// Verify checks that token is a valid cosign keyless OIDC signature whose
	// subject matches expectedSubject. Returns nil on success, error on failure.
	Verify(token, expectedSubject string) error
}

// SATokenHmacVerifier verifies an SA-token HMAC signature (used by CI pipelines).
// The HMAC key is fetched from OpenBao at verification time.
//
// TODO(spec-followup): implement real SA-token HMAC verification using the
// shared-secret stored in OpenBao once the credential broker is wired.
type SATokenHmacVerifier interface {
	// Verify checks that token is a valid HMAC over the approval payload for the
	// given audience. Returns nil on success, error on failure.
	Verify(token, audience string) error
}

// FakeCosignVerifier is a test-double that always succeeds (or fails if FailNext is true).
type FakeCosignVerifier struct {
	// FailNext causes the next Verify call to return an error.
	FailNext bool
}

func (f *FakeCosignVerifier) Verify(_, _ string) error {
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

func (f *FakeSATokenHmacVerifier) Verify(_, _ string) error {
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

var errSignatureVerificationFailed = sigVerifyError{"signature verification failed (fake)"}
