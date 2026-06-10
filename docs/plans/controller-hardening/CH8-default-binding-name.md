<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../../config/default/bootstrap/guardrailbinding-cluster-default.yaml
  - ../../../internal/controller/keese
related_skills: [plan-management, controller-authoring]
status: planned
last_verified: 2026-06-10
phase: CH8
model_tier: sonnet
depends_on: []
agent: controller-author
outputs:
  - config/default/bootstrap
---

# CH8 — Fix default GuardrailBinding name mismatch

**Goal.** The bootstrap manifest
`config/default/bootstrap/guardrailbinding-cluster-default.yaml` creates a binding
named **`keese-default`**, but the GuardrailBinding controller's
`defaultBindingName` const (`internal/controller/authz/guardrailbinding_controller.go:35`)
is **`keese.ai-default`** — so the controller never finds the default binding and
emits a non-fatal `DefaultBindingMissing` warning; default-inherit (design 06,
EH5) silently has no parent. Make them match.

## Deliverables

1. Rename the bootstrap binding to **`keese.ai-default`** (the convention-correct
   value the const already uses — `keese.ai`-prefixed, like every other keese name).
   Update the manifest's `metadata.name` (and any in-file references).
2. `git grep -n 'keese-default'` and fix any **other** stale references to the old
   name (samples, docs, e2e fixtures) so nothing else expects `keese-default`.
   (If a reference is in a path you don't own — e.g. `tests/e2e/` — list it for the
   orchestrator instead of editing.)

## Acceptance

- `config/default/bootstrap/guardrailbinding-cluster-default.yaml` `metadata.name`
  == the controller's `defaultBindingName` (`keese.ai-default`).
- `kubectl apply --dry-run=client` (or kubeconform) on the manifest is valid.
- No remaining `keese-default` references that should be `keese.ai-default`.

## Notes for the agent

- Tiny, surgical fix. Stay inside `config/default/bootstrap/`. Do NOT change the
  controller const (it's already convention-correct) or touch `internal/`, other
  `config/` overlays (CH7), `.github/**`, conductor/, .claude/, scripts/lib/**.
- **Never run bare `git stash`/`pop`/`reset`/`checkout <branch>`** (hits the shared
  checkout). This unblocks EH5's default-inherit assertion against a real cluster.
