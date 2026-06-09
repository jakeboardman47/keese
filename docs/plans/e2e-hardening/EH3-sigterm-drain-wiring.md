<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../../.claude/rules/06-signal-handling.md
  - ../../../scripts/dev/sigterm-drain-test.sh
related_skills: [plan-management, makefile-authoring]
status: complete
last_verified: 2026-06-09
phase: EH3
model_tier: sonnet
depends_on: []
agent: test-engineer
outputs:
  - Makefile
  - scripts/dev/sigterm-drain-test.sh
  - cmd/keese-drain
related_paths:
  - cmd/
---

# EH3 — SIGTERM-drain test wiring

**Goal.** [`rule 06-signal-handling`](../../../.claude/rules/06-signal-handling.md)
§10 requires every `cmd/**` binary to have a test that sends SIGTERM and asserts
(a) work drains, (b) exit 0, (c) a structured `shutdown` event. Today
`scripts/dev/sigterm-drain-test.sh` exists but is wired into **nothing** (no make
target, no kuttl step, no CI). Make it runnable and enforce the contract.

## Deliverables

1. **`make sigterm-drain-test`** target wrapping `scripts/dev/sigterm-drain-test.sh`
   (guarded by `$(GUARD_CONTEXT)`), so it is invocable locally and from CI.
2. **Robustify `scripts/dev/sigterm-drain-test.sh`**: parameterize the target
   binary/deployment so it covers each long-running `cmd/**` (operator,
   `keese-drain`, `keese-wf-launcher`, `keese-cosign-webhook` where it has a drain
   loop), not just the operator. Keep the graceful "skip if pod absent" behavior.
   Assert exit 0 within `terminationGracePeriodSeconds`, leader-lease release
   (operator), and a structured `shutdown` log line with
   `(reason, drain_duration_ms, checkpoint_location)`.
3. **Per-binary unit/envtest** under `cmd/keese-drain` (and any `cmd/**` lacking
   one): a Go test that starts the drain loop, sends SIGTERM via the
   `signal.NotifyContext`, and asserts checkpoint written + exit 0 (rule 06 §10).
   Use the existing operator drain test as the pattern if present.

## Acceptance

- `make sigterm-drain-test` runs end-to-end against a bootstrapped kind cluster
  (or skips cleanly when no cluster/pod).
- `go test ./cmd/...` exercises the SIGTERM path for each drain-bearing binary;
  `-race` clean.
- `make lint && make test` green.

## Notes for the agent

- CI wiring is **out of scope** (EH1, orchestrator) — just expose the make target
  + green tests. Stay inside `outputs:`.
- Do not weaken `scripts/check-signal-handling.sh` (pre-commit) — your new tests
  should satisfy it.
