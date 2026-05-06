// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// OCIArtifact is the result of a successful OCI pull.
type OCIArtifact struct {
	// Digest is the content-addressable digest, e.g. "sha256:abc123...".
	Digest string
	// LocalPath is the directory on the local filesystem where the artifact was unpacked.
	LocalPath string
}

// OCIFetcher abstracts pulling and cosign-verifying an OCI artifact.
// The real implementation calls oras + cosign; FakeOCIFetcher is used in tests.
type OCIFetcher interface {
	// Pull fetches the OCI artifact identified by (registry, repository, tagOrDigest)
	// into a local cache directory and returns the resolved digest.
	Pull(ctx context.Context, registry, repository, tagOrDigest string) (*OCIArtifact, error)

	// Verify runs cosign verification against the resolved digest.
	// Identity regexp: https://github.com/keese-ai/keese/.github/workflows/.*
	// OIDC issuer:     https://token.actions.githubusercontent.com
	// Fail-closed: returns an error (triggering RecipeImageUnverified) on any verification failure.
	Verify(ctx context.Context, registry, repository, digest string) error
}

// FakeOCIFetcher is a test double for OCIFetcher.
// PullErr and VerifyErr allow tests to inject failures.
type FakeOCIFetcher struct {
	// PulledDigest is returned on a successful Pull.
	PulledDigest string
	// PulledLocalPath is the path returned on a successful Pull.
	PulledLocalPath string
	// PullErr is returned from Pull when non-nil.
	PullErr error
	// VerifyErr is returned from Verify when non-nil.
	VerifyErr error

	// Recorded calls for assertions.
	PullCalls   []OCIPullCall
	VerifyCalls []OCIVerifyCall
}

// OCIPullCall records a Pull invocation.
type OCIPullCall struct {
	Registry, Repository, TagOrDigest string
}

// OCIVerifyCall records a Verify invocation.
type OCIVerifyCall struct {
	Registry, Repository, Digest string
}

func (f *FakeOCIFetcher) Pull(_ context.Context, registry, repository, tagOrDigest string) (*OCIArtifact, error) {
	f.PullCalls = append(f.PullCalls, OCIPullCall{registry, repository, tagOrDigest})
	if f.PullErr != nil {
		return nil, f.PullErr
	}
	digest := f.PulledDigest
	if digest == "" {
		digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	}
	localPath := f.PulledLocalPath
	if localPath == "" {
		localPath = "/tmp/fake-recipe-cache/" + digest
	}
	return &OCIArtifact{Digest: digest, LocalPath: localPath}, nil
}

func (f *FakeOCIFetcher) Verify(_ context.Context, registry, repository, digest string) error {
	f.VerifyCalls = append(f.VerifyCalls, OCIVerifyCall{registry, repository, digest})
	return f.VerifyErr
}

var _ OCIFetcher = &FakeOCIFetcher{}
