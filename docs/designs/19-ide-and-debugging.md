<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: developer-experience
depends: [02-workspace-model.md, 13-cli-tunnel-wireguard.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 19 — IDE and Debugging

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? GoLand (primary) and
VSCode (secondary) IDE configs for the keese operator, including dlv-in-kind
remote debugging, `kubectl-keese attach`, and workspace status debugging fields._

## Open questions (must be answered before `status: current`)

1. What is the GoLand run configuration for remote dlv attach to the operator
   pod running in kind — what port, host, and source-root mapping?
2. How does `make ide-config` stamp out the GoLand `.idea/` and VSCode
   `.vscode/` debug configs reproducibly without overwriting local overrides?
3. What workspace `status` fields are added specifically to aid debugging
   (last reconcile error, reconcile count, phase transition timestamps)?
4. How does `kubectl-keese attach` authenticate, and does it require the
   WireGuard tunnel (design 13) or can it fall back to `kubectl exec`?
5. How do JetBrains ACP and VSCode `block.vscode-goose` extensions configure
   the goose ACP endpoint — via kubeconfig context or direct service URL?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [13-cli-tunnel-wireguard.md](13-cli-tunnel-wireguard.md)
- [../references/ide-and-debugging.md](../references/ide-and-debugging.md)

TODO(design-gate)
