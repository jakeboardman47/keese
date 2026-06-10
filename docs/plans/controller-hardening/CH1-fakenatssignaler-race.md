<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../../internal/controller/policy/nats.go
  - ../../../internal/controller/policy/tokenbudget_controller_test.go
related_skills: [plan-management, controller-authoring, testing]
status: planned
last_verified: 2026-06-10
phase: CH1
model_tier: sonnet
depends_on: []
agent: controller-author
outputs:
  - internal/controller/policy
---

# CH1 — Fix FakeNatsSignaler data race (unblock -race CI)

**Goal.** `FakeNatsSignaler` (`internal/controller/policy/nats.go`) has unguarded
fields (`SetCalls`/`ClearCalls`/`Exceeded`) written by the reconciler goroutine
and read directly by `Eventually` assertions in
`tokenbudget_controller_test.go` (~lines 220–222, 244). Under `-race` this is a
data race → `make test-integration` (`go test -race -tags=integration
./internal/controller/...`) is **red on `main`** (surfaced by EH14). Fix it so the
race detector is clean.

## Deliverables

1. Make `FakeNatsSignaler` concurrency-safe: add a `sync.Mutex` (or `sync/atomic`
   for the counters) guarding every field write the reconciler performs and every
   read; expose **guarded accessor methods** (e.g. `SetCallCount()`,
   `Exceeded(scope)`), don't export raw fields for cross-goroutine reads.
2. Update `tokenbudget_controller_test.go` reads to use the accessors (no direct
   field reads from the test goroutine).
3. This is a **test-double** fix — do **not** change the real `NatsSignaler`
   interface or any reconciler behavior.

## Acceptance

- `CGO_ENABLED=0 go test -race -count=1 -tags=integration ./internal/controller/policy/...`
  → **green, zero data races** (needs envtest; run `make envtest-setup` /
  setup-envtest if `KUBEBUILDER_ASSETS` is unset).
- `make lint` clean. No change to production reconciler logic or public interfaces.

## Notes for the agent

- macOS gotcha: `CGO_ENABLED=0` for local runs (`_SecTrustCopyCertificateChain`
  linker bug is environmental; Linux CI is unaffected).
- Confirm whether `FakeNatsSignaler` lives in `nats.go` (non-test) or a `_test.go`
  helper and keep it where it is; just make it race-safe. Stay inside
  `internal/controller/policy/`. SSA-only if you touch any write path (you should
  not — this is test-harness only).
