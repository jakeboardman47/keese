// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Package rebac provides the keese OpenFGA client used by every
// reconciler that emits ReBAC tuples. Each domain controller defines
// its own narrow Writer interface (returning local tuple shapes) and
// wires an adapter from this package; the adapter translates the
// per-domain Sync/Delete calls into OpenFGA Write/Delete operations.
//
// The package supports two write semantics:
//
//   - Write(ctx, object, relation, user) — idempotent; "already exists"
//     errors are swallowed.
//   - Delete(ctx, object, relation, user) — idempotent; "not found"
//     errors are swallowed.
//
// Higher-level reconcile-time semantics (full-state replace, diff-write)
// are deliberately not provided here: controllers track their desired
// tuple set explicitly and call Sync/Delete on transition. Stale-tuple
// cleanup mid-resource-lifetime is the controller's responsibility, not
// the writer's.
//
// Configuration is sourced from environment variables in cmd/main.go:
//
//   - OPENFGA_API_URL                 — http(s) endpoint for the FGA store
//   - OPENFGA_STORE_ID                — store UUID created by the seed Job
//   - OPENFGA_AUTHORIZATION_MODEL_ID  — model UUID written by the seed Job
//
// When OPENFGA_API_URL is unset the operator falls back to the per-package
// NoopRebacWriter implementations (preserves envtest + out-of-cluster local-run
// paths). Tests inject per-domain fake writers (defined in *_fake_test.go files).
package rebac
