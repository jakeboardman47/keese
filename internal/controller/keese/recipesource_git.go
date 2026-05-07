// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	billy "github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GitCloner resolves, clones, and digests a pinned git revision into memory.
// The operator pod has readOnlyRootFilesystem: true (rule 05.11), so we use
// go-git's in-memory storer + billy memfs — no disk writes ever occur.
type GitCloner interface {
	// Clone fetches the repository at url, resolves refOrSHA (branch, tag, or 40-char SHA),
	// and returns the resolved commit SHA plus a deterministic SHA-256 of the repo tree.
	Clone(ctx context.Context, url, refOrSHA string, auth transport.AuthMethod) (resolvedSHA, treeDigest string, err error)
}

// DefaultGitCloner is the production GitCloner. Exported so tests can substitute.
type DefaultGitCloner struct{}

// Clone performs an in-memory shallow clone of url at refOrSHA.
// Returns the resolved full commit SHA and a deterministic SHA-256 tree digest.
func (g *DefaultGitCloner) Clone(ctx context.Context, url, refOrSHA string, auth transport.AuthMethod) (string, string, error) {
	stor := memory.NewStorage()
	fs := memfs.New()

	isHash := looksLikeCommitHash(refOrSHA)

	opts := &gogit.CloneOptions{
		URL:          url,
		Auth:         auth,
		SingleBranch: !isHash,
		NoCheckout:   false,
		Tags:         gogit.NoTags,
	}

	if !isHash {
		// Fetch only the named branch so we get a shallow clone.
		// Tag refs are tried later via resolveRef if the branch lookup fails.
		opts.ReferenceName = plumbing.NewBranchReferenceName(refOrSHA)
	}

	repo, err := gogit.CloneContext(ctx, stor, fs, opts)
	if err != nil {
		// Retry as a tag clone if the branch ref failed.
		if !isHash {
			opts2 := &gogit.CloneOptions{
				URL:           url,
				Auth:          auth,
				SingleBranch:  true,
				NoCheckout:    false,
				Tags:          gogit.NoTags,
				ReferenceName: plumbing.NewTagReferenceName(refOrSHA),
			}
			fs2 := memfs.New()
			repo, err = gogit.CloneContext(ctx, memory.NewStorage(), fs2, opts2)
			if err != nil {
				return "", "", fmt.Errorf("clone %s ref=%s: %w", sanitizeURL(url), refOrSHA, err)
			}
			fs = fs2
		} else {
			return "", "", fmt.Errorf("clone %s: %w", sanitizeURL(url), err)
		}
	}

	resolvedSHA, err := resolveGitRef(repo, refOrSHA, isHash)
	if err != nil {
		return "", "", fmt.Errorf("resolve ref %q in %s: %w", refOrSHA, sanitizeURL(url), err)
	}

	digest, err := billyTreeDigest(fs, "")
	if err != nil {
		return "", "", fmt.Errorf("computing tree digest for %s@%s: %w", sanitizeURL(url), resolvedSHA, err)
	}

	return resolvedSHA, digest, nil
}

// resolveGitRef resolves a reference to its full commit SHA.
func resolveGitRef(repo *gogit.Repository, refOrSHA string, isHash bool) (string, error) {
	if isHash {
		h := plumbing.NewHash(refOrSHA)
		if _, err := repo.CommitObject(h); err != nil {
			return "", fmt.Errorf("commit %s not found: %w", refOrSHA, err)
		}
		return refOrSHA, nil
	}

	// Try branch.
	if ref, err := repo.Reference(plumbing.NewBranchReferenceName(refOrSHA), true); err == nil {
		return ref.Hash().String(), nil
	}

	// Try tag.
	if ref, err := repo.Reference(plumbing.NewTagReferenceName(refOrSHA), true); err == nil {
		// Annotated tag → dereference to the commit.
		if tagObj, err := repo.TagObject(ref.Hash()); err == nil {
			return tagObj.Target.String(), nil
		}
		return ref.Hash().String(), nil
	}

	// HEAD fallback.
	if ref, err := repo.Head(); err == nil {
		return ref.Hash().String(), nil
	}

	return "", fmt.Errorf("ref %q not found as branch, tag, or HEAD", refOrSHA)
}

// billyTreeDigest walks a billy.Filesystem and produces a deterministic
// SHA-256 by writing a sorted canonical tar stream. Files are sorted by path
// so the digest is reproducible across clones of identical content.
func billyTreeDigest(fs billy.Filesystem, dir string) (string, error) {
	h := sha256.New()
	tw := tar.NewWriter(h)
	if err := walkBillyFS(tw, fs, dir); err != nil {
		return "", err
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// walkBillyFS recursively adds files from fs at dir into tw in sorted order.
func walkBillyFS(tw *tar.Writer, fs billy.Filesystem, dir string) error {
	var listPath string
	if dir == "" {
		listPath = "."
	} else {
		listPath = dir
	}

	entries, err := fs.ReadDir(listPath)
	if err != nil {
		return fmt.Errorf("readdir %q: %w", listPath, err)
	}

	// Sort for determinism.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, e := range entries {
		var fullPath string
		if dir == "" {
			fullPath = e.Name()
		} else {
			fullPath = dir + "/" + e.Name()
		}

		if e.IsDir() {
			if err := walkBillyFS(tw, fs, fullPath); err != nil {
				return err
			}
			continue
		}

		f, err := fs.Open(fullPath)
		if err != nil {
			return fmt.Errorf("open %q: %w", fullPath, err)
		}

		hdr := &tar.Header{
			Name: fullPath,
			Mode: 0o644,
			Size: e.Size(),
		}
		if werr := tw.WriteHeader(hdr); werr != nil {
			_ = f.Close()
			return werr
		}
		if _, cerr := io.Copy(tw, f); cerr != nil {
			_ = f.Close()
			return cerr
		}
		_ = f.Close()
	}
	return nil
}

// looksLikeCommitHash returns true when s is a 40-character lowercase hex string.
func looksLikeCommitHash(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// sanitizeURL strips any embedded credentials from a URL before logging or
// recording in an event (rule 02 — never log credentials).
func sanitizeURL(u string) string {
	if i := strings.Index(u, "@"); i != -1 {
		if j := strings.Index(u, "://"); j != -1 {
			return u[:j+3] + "<redacted>@" + u[i+1:]
		}
	}
	return u
}

// loadGitAuth builds a go-git AuthMethod from a SecretRef.
// Secrets must be in the operator namespace (controller has access) and contain
// "username" + "password" keys (or "token" for a GitHub PAT).
// Per rule 05.7, the secret is fetched via the Kubernetes API; the operator pod
// does NOT need the secret projected — only the operator's own SA needs RBAC read access.
// Returns nil auth for public repos (nil secretRef).
func loadGitAuth(ctx context.Context, c client.Client, operatorNamespace string, secretRef *corev1.LocalObjectReference) (transport.AuthMethod, error) {
	if secretRef == nil {
		return nil, nil
	}

	var secret corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{
		Namespace: operatorNamespace,
		Name:      secretRef.Name,
	}, &secret); err != nil {
		return nil, fmt.Errorf("getting git credential secret %s/%s: %w", operatorNamespace, secretRef.Name, err)
	}

	username := string(secret.Data["username"])
	password := string(secret.Data["password"])
	if password == "" {
		// Accept "token" key (GitHub PAT / GitLab access token).
		password = string(secret.Data["token"])
		if username == "" {
			username = "git" // conventional username for token-based auth
		}
	}

	if password == "" {
		// Secret exists but contains no usable credential — treat as unauthenticated.
		return nil, nil
	}

	return &gogithttp.BasicAuth{
		Username: username,
		// Password is not logged anywhere (rule 02).
		Password: password,
	}, nil
}
