<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - EH2-coverage-enforcement.md
  - ../../../internal/runtime/providers/adkgo
  - ../../../internal/runtime/providers/adkpython
  - ../../../internal/runtime/podexec
related_skills: [plan-management]
status: complete
last_verified: 2026-06-10
phase: EH13
model_tier: sonnet
depends_on: []
agent: test-engineer
outputs:
  - internal/runtime/providers/adkgo
  - internal/runtime/providers/adkpython
  - internal/runtime/podexec
  - test/coverage-targets.yaml
---

# EH13 — Runtime SPI unit tests (adkgo / adkpython / podexec)

**Goal.** `internal/runtime/providers/adkgo`, `internal/runtime/providers/adkpython`,
and `internal/runtime/podexec` have **zero** tests (pinned at `0.0` in the EH2
coverage ratchet). Add real unit tests and raise their floors.

## Deliverables

1. **adkgo / adkpython** — these are E0 skeletons (SPI methods return
   `ErrUnsupported`). Table-driven tests asserting: the provider registers under
   the correct name; each SPI method returns the documented `ErrUnsupported` (or
   its real behavior where implemented); capability matrix is zeroed as designed.
2. **podexec** — unit-test the exec/stream logic with a fake
   `remotecommand`/clientset (do not hit a real cluster); cover the error paths
   (non-zero exit, stream setup failure, timeout).
3. **Raise the EH2 floors** — re-measure with `make coverage-check` and lift the
   `0.0` entries for these three packages in `test/coverage-targets.yaml` to the
   new measured floor (a ratchet — at/below measured).

## Acceptance

- `CGO_ENABLED=0 go test -race ./internal/runtime/providers/adkgo/...
  ./internal/runtime/providers/adkpython/... ./internal/runtime/podexec/...` green.
- `make coverage-check` green with the three floors raised above `0.0`.
- `make lint` clean.

## Notes for the agent

- Race-clean, table-driven, no `sleep`, fakes you own (rule 06). Do NOT change
  production behavior — tests only (plus the `coverage-targets.yaml` floor bump).
- macOS gotcha: build/test with `CGO_ENABLED=0` (the `_SecTrustCopyCertificateChain`
  linker bug is environmental; Linux CI is unaffected).
- Stay inside the four `outputs:` paths.
