<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../../.claude/rules/06-testing.md
  - ../../../Makefile
related_skills: [plan-management, makefile-authoring]
status: complete
last_verified: 2026-06-09
phase: EH2
model_tier: sonnet
depends_on: []
agent: test-engineer
outputs:
  - scripts/coverage-check.sh
  - test/coverage-targets.yaml
  - Makefile
---

# EH2 — Coverage enforcement infra

**Goal.** Implement the per-package coverage gate that
[`.claude/rules/06-testing.md`](../../../.claude/rules/06-testing.md) §Coverage
mandates but that does not exist (`test/coverage-targets.yaml` and
`scripts/coverage-check.sh` are both missing; no CI workflow computes coverage).

## Deliverables

1. **`test/coverage-targets.yaml`** — per-package minimum line-coverage thresholds.
   Seed it from a real measured run (`go test -short -coverprofile`), set each
   threshold at or just below the current measured value (never above — this is a
   ratchet, not a wish). Cover at minimum `internal/controller/...`,
   `internal/authz/...`, `internal/runtime/...`, `internal/admission/...`,
   `internal/rebac`, `internal/featuregate`, `internal/wflauncher`. Document the
   schema at the top of the file.
2. **`scripts/coverage-check.sh`** — `#!/usr/bin/env bash`, `set -euo pipefail`,
   SPDX header, sources `scripts/lib/log.sh`. Runs `go test -short
   -coverprofile=<tmp> ./...`, computes per-package coverage via `go tool cover
   -func`, compares against `test/coverage-targets.yaml`, prints a table, and
   exits non-zero listing every package below its target. A package with no entry
   is informational (warn), not a failure. Idempotent; shellcheck-clean (`-x`),
   `shfmt -i 2 -ci -bn` clean.
3. **`Makefile`** target `coverage-check` that invokes the script. Add it to the
   `verify` aggregate. Do **not** edit `.github/**` (protected — EH1 wires CI).

## Acceptance

- `make coverage-check` passes on the current tree (thresholds = measured floor).
- Lowering any threshold later is a deliberate, reviewable diff.
- `make lint` clean; script passes `scripts/`-style pre-commit hooks.

## Notes for the agent

- Stay inside `outputs:`. The CI wiring (`test.yaml`) is **out of scope** —
  EH1 (orchestrator) does it; just make the script CI-invocable (clean exit codes,
  no interactive prompts, honors `$KUBEBUILDER_ASSETS` not required for `-short`).
- If measuring reveals a package at ~0% (e.g. `adkgo`/`adkpython`/`podexec`),
  set its target to the measured floor and note it — EH13 raises it later.
