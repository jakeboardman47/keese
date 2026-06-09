<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - EH3-sigterm-drain-wiring.md
  - ../../../cmd/keese-drain/main.go
  - ../../../tests/e2e/agentruntime-drain
related_skills: [plan-management]
status: planned
last_verified: 2026-06-09
phase: EH10
model_tier: sonnet
depends_on: [EH3]
agent: test-engineer
outputs:
  - tests/e2e/agentruntime-drain
---

# EH10 — Real drain/SIGTERM e2e (replace stand-ins)

**Goal.** `tests/e2e/agentruntime-drain/` simulates drain with **busybox
stand-ins** (a hand-written `session.sqlite` + JSON marker), not the real
`keese-drain` binary, and `00-assert-prereqs.yaml` asserts a Workspace
`drain-test-ws` at phase Running that **nothing in the suite creates** (orphan).
Make it exercise the shipped code.

## Deliverables

Rework `tests/e2e/agentruntime-drain/`:

1. **Fix the orphan prereq** — either provision `drain-test-ws` (Tenant +
   AgentRuntime + Workspace + WorkspaceSession) in a setup step, or remove the
   dangling assert. The suite must be self-contained.
2. **Real `keese-drain`** — drive the actual `keese-drain` binary (its `run()`
   seam + Go test landed in EH3) as the session pod's preStop/init drain, not a
   busybox echo. Send a **real SIGTERM** (`kubectl delete pod` with the configured
   `terminationGracePeriodSeconds`).
3. **Assert the real contract** — after pod replacement, assert the real
   checkpoint (`sessions/<uid>/draining`) **and** the session SQLite survive on the
   PVC, and that the structured `shutdown` event (rule 06 §4, with
   `reason`/`drain_duration_ms`/`checkpoint_location`) appears in the drained pod's
   logs.

## Acceptance

- Suite green under `make test-e2e` on a cluster where the `keese-drain` image is
  deployed; asserts real-binary drain + checkpoint survival + the shutdown event.
- No orphan asserts; suite is self-contained.

## Notes for the agent

- If the `keese-drain` image isn't built/loaded in the local bootstrap, keep the
  real-binary wiring but mark the in-cluster drain-run step skipped, add
  `revisit_when_drain_image_live`, set `status: shipped-with-stubs` — but **always**
  fix the orphan prereq + make the suite self-contained (that part has no infra dep).
- Stay strictly inside `tests/e2e/agentruntime-drain/`. Do not touch
  `cmd/keese-drain` (EH3 owns it) or other suites.
