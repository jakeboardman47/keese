// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import "context"

// CacheFlusher sends a cache-flush signal to all keese-ext-authz gateway pods.
// The flush instructs the ext_authz sidecar to evict cached OIDC public keys and
// token verifications for the named provider, preventing stale-key attacks on CR
// deletion or rotation.
//
// The real implementation will call gRPC to the keese-ext-authz admin endpoint.
// Until the ext-authz service is implemented, use FakeCacheFlusher in tests.
//
// TODO(spec-followup): wire up real gRPC call once keese-ext-authz admin API is specified.
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
