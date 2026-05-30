// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import "context"

// CacheFlusher sends a cache-flush signal to all keese-ext-authz gateway pods.
// The flush instructs the ext_authz sidecar to evict cached OIDC public keys and
// token verifications for the named provider, preventing stale-key attacks on CR
// deletion or rotation.
//
// Feature status: deferred — paired with the keese-authz token cache itself.
// Today keese-authz performs no token caching (every request → OpenFGA Check),
// so there is no cache to flush; FakeCacheFlusher's no-op is the correct
// production behavior in this build. When the token cache lands, three changes
// go together:
//
//  1. keese-authz adds a TTL-cached token verifier (internal/authz/extauth)
//  2. keese-authz exposes POST /admin/cache/flush?provider=<name> on its
//     management port (8081)
//  3. This interface gains an HTTPCacheFlusher that discovers all keese-authz
//     pods via the Endpoints API and POSTs in parallel with maxFlushTimeout
type CacheFlusher interface {
	// Flush sends a cache invalidation signal for the named provider.
	// Returns nil if all reachable gateway pods ACKed; returns error on partial
	// failure. The reconciler treats error as "retry after backoff" and surfaces
	// the failure as a warning event. After maxFlushTimeout the caller proceeds
	// with deletion regardless.
	Flush(ctx context.Context, providerName string) error
}

// FakeCacheFlusher is a test double for CacheFlusher.
// Set Err to simulate a flush failure; leave nil for success.
type FakeCacheFlusher struct {
	// Err is returned by Flush if non-nil.
	Err error
	// Calls records how many times Flush was called.
	Calls int
	// LastProvider is the last provider name passed to Flush.
	LastProvider string
}

func (f *FakeCacheFlusher) Flush(_ context.Context, providerName string) error {
	f.Calls++
	f.LastProvider = providerName
	return f.Err
}

var _ CacheFlusher = &FakeCacheFlusher{}
