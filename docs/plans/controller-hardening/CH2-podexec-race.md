<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../../internal/runtime/podexec/podexec.go
  - ../../../internal/runtime/podexec/podexec_test.go
related_skills: [plan-management, controller-authoring, testing]
status: complete
last_verified: 2026-06-10
phase: CH2
model_tier: sonnet
depends_on: []
agent: controller-author
outputs:
  - internal/runtime/podexec
---

# CH2 — Fix podexec context-timeout data race

**Goal.** `internal/runtime/podexec/podexec.go` (~line 65): when
`remotecommand.StreamWithContext` returns on a mid-stream deadline, its
background stdout/stderr copy goroutine keeps writing the `bytes.Buffer`s that
`Exec` then reads via `so.Bytes()`/`se.Bytes()` → data race (EH13 flagged it
deterministically under `-race`; EH13's timeout test deliberately cancels at
connection-setup to avoid it).

## Deliverables

1. Make `Exec` race-free on the cancel/timeout path: ensure the streaming copy
   goroutine has **finished writing** the buffers before `Exec` reads them — e.g.
   bound the stream with an `errgroup`/`sync.WaitGroup` and read the bytes only
   after the executor returns *and* the copies are joined, or use a
   `sync`-guarded writer. Preserve the existing contract: on error `Exec` still
   returns whatever partial output was captured + the error (and the
   `utilexec.CodeExitError` exit code on non-zero exit).
2. **Un-skip / add** a test that exercises the **mid-stream** timeout (cancel
   after some bytes are streamed, not just at setup) and asserts it is race-clean
   under `-race`.

## Acceptance

- `CGO_ENABLED=0 go test -race -count=1 ./internal/runtime/podexec/...` → green,
  including the mid-stream-timeout test; **zero data races**.
- `Exec`'s success / non-zero-exit / setup-failure behavior is unchanged
  (existing EH13 tests still pass).
- `make lint` clean.

## Notes for the agent

- This is a **production concurrency fix** — keep the change minimal and the
  public `Exec` signature/contract intact. Model the mid-stream test on
  client-go's `remotecommand/spdy_test.go` fake exec server (EH13's
  `podexec_test.go` already has the owned SPDY v4 harness to extend).
- macOS gotcha: `CGO_ENABLED=0`. Stay inside `internal/runtime/podexec/`.
