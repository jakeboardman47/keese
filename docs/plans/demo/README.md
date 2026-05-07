<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: index
depends: [../README.md, ../rubric.md, ../../../MEMORY.md]
related_skills: [plan-management]
status: current
last_verified: 2026-04-25
---

# Demo track — first working agent on a real cluster

> **Goal.** End-to-end demo on a **local kind cluster**: provision the
> stack, install keese, and run one Workspace whose pod calls Anthropic
> through the Envoy AI Gateway, returns a result, and writes to sqlite
> memory. Cloud deploy (D4) is deferred; D5 retargeted to kind T1+T2 on
> 2026-05-06 — see iteration 2 in [D5-demo-smoke.md](D5-demo-smoke.md).
>
> **Non-goal.** Production readiness. The audit on 2026-04-25 found 23
> stub paths under [internal/controller/](../../../internal/controller/);
> this track lands the minimum subset that makes the happy path work.
> Everything else is captured in [tech-debt.md](tech-debt.md) for the 1–4
> week tail.

## Phase index

| Phase | Title | Depends on | Effort | Status |
|---|---|---|---|---|
| D1 | [Controller wiring + samples + bundle regen](D1-controller-wiring.md) | — | 3–4 h | planned |
| D2 | [Agent runtime minimum SPI (real goose pod + sqlite memory)](D2-runtime-spi-minimum.md) | D1 | 5–7 h | planned |
| D3 | [Cluster bootstrap + Anthropic LLM wiring](D3-cluster-bootstrap.md) | — (parallel with D2) | 3–4 h | planned |
| D4 | [Cloud deploy (single-cloud, single-tenant, single-LLM)](D4-cloud-deploy.md) | D1, D2, D3 | 3–4 h | planned |
| D5 | [Demo smoke (kind, T1+T2 only)](D5-demo-smoke.md) | — (kind only; D4 deferred) | 1 h | in-progress |
| TD | [Tech-debt register (post-demo cleanup)](tech-debt.md) | — | tracked separately | open |

## Wall-clock map (Sat 2026-04-25 ~ Mon AM 2026-04-27)

```
Sat PM  ────────► D1 controller wiring                 ───┐
Sat PM  ────────► D3 cluster bootstrap (parallel)         ├── merge points
Sat eve ────────► D2 runtime SPI minimum              ───┘
Sun AM  ────────► D4 cloud deploy
Sun PM  ────────► D5 demo smoke + runbook
Sun eve ────────► buffer / fix the inevitable surprise
Mon AM  ────────► demo
```

D1 + D3 are independent — run them in parallel agents/worktrees.
D2 depends on D1 only for the bundle regen + main.go wiring; if you
have two hands, start D2 against an unmerged D1 worktree.

## Refinement passes

Each phase carries an iteration log scored against [../rubric.md](../rubric.md).
Demo phases target SHIP ≥ 85 (the rubric threshold), not the post-gate ≥ 90
target — operational-readiness gaps are captured as tech debt rather than
re-iterated to perfection.

## Decisions specific to this track

| ID | Decision | Why |
|---|---|---|
| DD-1 | Single cloud: GKE Autopilot. | Fastest control-plane provisioning (~5 min); managed node pools; CSI/PDs work without IAM gymnastics. EKS is fine if Google access is a problem; AKS adds 30+ min. |
| DD-2 | Single LLM: Anthropic via static API key + ExternalSecret. | Bypasses OIDC-STS dance ([05b](../../designs/05b-credential-injection-patterns.md) complex paths) for demo. |
| DD-3 | Single Tenant, single user. | Avoids depending on real OpenFGA tuple writes; the existing fake-rebac stub suffices for the happy path. |
| DD-4 | Goose runtime image: `ghcr.io/block/goose:latest` upstream, no keese fork. | The keese-side SPI wrapper (`internal/runtime/providers/goose/`) is post-demo work. |
| DD-5 | Memory: sqlite on PVC. Other backends deferred. | Removes Redis/Qdrant/pgvector chart dependencies from the critical path. |
| DD-6 | No webhooks (validating/defaulting/conversion) for demo. | None are wired today; the default GuardrailBinding admission is satisfied by shipping the CR, not a webhook. |
| DD-7 | OLM install via `operator-sdk run bundle`, not OperatorHub catalog. | Simplest path: skip CatalogSource + private catalog image; install bundle directly. |

## Definition of done (demo green)

1. `kubectl get tenant alpha` → `Ready: True`.
2. `kubectl get workspace -n alpha my-ws` → `Ready: True`, PVC bound.
3. `kubectl get workspacesession -n alpha my-session` → `Ready: True`, pod `Running`.
4. `kubectl logs -n alpha <session-pod>` shows a successful Anthropic round-trip.
5. `kubectl exec` into the pod and confirm sqlite memory file at `/var/run/keese/memory/session.db` exists and grew between two prompts.
6. `kubectl delete workspacesession my-session && kubectl apply -f session.yaml` re-attaches and recovers prior memory.

## Tech-debt linkage

Every stub or shortcut taken in D1–D5 is logged in [tech-debt.md](tech-debt.md)
with a severity tag (P1 = ship-this-week, P2 = ship-this-month,
P3 = next-quarter) and a pointer to the originating phase doc.
