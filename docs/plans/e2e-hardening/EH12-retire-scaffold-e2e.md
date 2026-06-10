<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../../test/e2e
related_skills: [plan-management]
status: complete
last_verified: 2026-06-10
phase: EH12
model_tier: sonnet
depends_on: []
agent: test-engineer
outputs:
  - test/e2e
  - Makefile
---

# EH12 — Retire/replace the scaffold `test/e2e/`

**Goal.** `test/e2e/` is the **unmodified kubebuilder scaffold** (Ginkgo;
`example.com/keese:v0.0.1`; still carries `TODO(user)` +
`+kubebuilder:scaffold:e2e-webhooks-checks`). It exercises **zero keese CRDs** —
only checks the manager pod is up + serves `/metrics`. It is misleading (wrong
image, no keese coverage) now that the real e2e lives in `tests/e2e/` (kuttl).

## Deliverables

Choose, and justify in the SUMMARY, ONE of:

- **(preferred) Retire it** — delete `test/e2e/` and remove any references
  (`Makefile` `test-e2e-go`/equivalent target, `go.mod` test deps only used by it,
  `.github/**` is out of scope — flag any CI ref for the orchestrator). The kuttl
  `tests/e2e/` suite is the real e2e.
- **Or repoint it** — rewrite the Ginkgo suite to a minimal **real keese** smoke
  (deploy the operator from `config/`, apply a Workspace, assert it reconciles to
  Ready) and fix the image to the real operator image.

Either way: no dangling `example.com/keese` image, no `TODO(user)` scaffold
markers, no reference to a deleted path.

## Acceptance

- `make lint` clean; `go build ./...` + `go vet ./...` clean (no broken imports
  from a removed package). If a `Makefile` target referenced `test/e2e`, it is
  updated/removed consistently.

## Notes for the agent

- If you retire it: grep the repo for `test/e2e` references first
  (`Makefile`, `go.mod`, `.github/**`, docs) and handle the non-protected ones;
  list any `.github/**` references for the orchestrator (protected).
- Stay inside `test/e2e/` + `Makefile`. Do NOT touch `tests/e2e/` (the real suite),
  `.github/**`, conductor/, .claude/, scripts/lib/**.
