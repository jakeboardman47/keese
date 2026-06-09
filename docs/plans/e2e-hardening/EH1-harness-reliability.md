<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../../.github/workflows/e2e.yaml
  - ../../../.github/workflows/test.yaml
related_skills: [plan-management]
status: complete
last_verified: 2026-06-09
phase: EH1
model_tier: sonnet
depends_on: [EH2, EH3]
agent: infra-bootstrap
dispatch: manual
outputs:
  - .github/workflows/e2e.yaml
  - .github/workflows/test.yaml
---

# EH1 — e2e harness reliability + CI wiring

**Goal.** Make CI actually run + gate the e2e/coverage/sigterm work. `.github/**`
is a protected path (`worktree-merge.sh` rejects it), so this phase is
`dispatch: manual` — the orchestrator authors it on `main`.

## Items

- [x] **Install kuttl in `e2e.yaml`** — the nightly ran `make test-e2e` which
  guards on `command -v kuttl`, but the runner never installed it (job exited 1
  / silently broken). Added a pinned `kubectl-kuttl` v0.15.0 download + `kuttl`
  symlink + `$GITHUB_PATH`. The nightly + new `rebac-decision` suite now run.
- [x] **Gate `coverage-check` in `test.yaml`** — added a `coverage gate` step
  running `make coverage-check` (EH2 ratchet) to the unit job, so PRs enforce the
  per-package floors.
- [x] **Wire the SIGTERM-drain job** — `e2e.yaml` runs `make sigterm-drain-test`
  (EH3, rule 06 §10) after the kuttl suite; skips cleanly if a target pod is absent.
- [ ] (deferred, optional) **Harden the `config/`-absent skip** — `e2e.yaml`
  pass-by-skips when `config/` is absent; make a genuinely-missing harness fail
  loudly. Low priority (`config/` always present today).
- [ ] (deferred, optional) **PR smoke** — a label-gated / fast-subset kuttl run on
  PRs so e2e is not purely nightly.

## Notes

- kuttl version: CI pins v0.15.0; the dev shell gets `kuttl` from nixpkgs
  (`flake.nix`). Both run the stable `kuttl.dev/v1beta1` `TestSuite` schema.
- Supply-chain follow-up: pin the kuttl binary by sha256 (verify against the
  release `checksums.txt`) when e2e graduates to a PR-gating check.
