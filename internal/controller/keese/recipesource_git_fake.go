// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

// FakeGitCloner is a test double for GitCloner.
// ResolvedSHA and TreeDigest are returned on success; CloneErr injects failures.
type FakeGitCloner struct {
	// ResolvedSHA is the full commit SHA returned on success.
	ResolvedSHA string
	// TreeDigest is the tree digest returned on success.
	TreeDigest string
	// CloneErr is returned from Clone when non-nil.
	CloneErr error

	// Recorded calls for assertions.
	CloneCalls []FakeCloneCall
}

// FakeCloneCall records a Clone invocation.
type FakeCloneCall struct {
	URL, RefOrSHA string
}

// Clone implements GitCloner.
func (f *FakeGitCloner) Clone(_ context.Context, url, refOrSHA string, _ transport.AuthMethod) (string, string, error) {
	f.CloneCalls = append(f.CloneCalls, FakeCloneCall{URL: url, RefOrSHA: refOrSHA})
	if f.CloneErr != nil {
		return "", "", f.CloneErr
	}
	sha := f.ResolvedSHA
	if sha == "" {
		sha = "aabbccdd" + fmt.Sprintf("%032s", refOrSHA)[:32]
	}
	digest := f.TreeDigest
	if digest == "" {
		digest = "sha256:" + fmt.Sprintf("%064s", sha)[:64]
	}
	return sha, digest, nil
}

var _ GitCloner = &FakeGitCloner{}
