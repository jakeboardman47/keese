<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: reliability
depends: [07-agent-runtime-spi.md, 18-process-lifecycle.md, 10b-token-accounting.md, 04a-openfga-authz-model.md]
related_skills: []
status: draft
last_verified: 2026-04-20
rollback: TODO — document migration path when status flips to current
---

# 23 — Agent Supervision (Patrol Pattern)

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. Kubernetes liveness probes detect stuck pods; they do not
detect stuck agents. An agent pod can be looping on a prompt, burning tokens,
and appear perfectly healthy to the kubelet. This design codifies how keese
detects, nudges, and escalates stuck AgentRuntimes — the supervisor patrol
pattern. Inspired by Steve Yegge, "Welcome to Gas Town" (2026): the
Witness / Deacon roles exist precisely because workers (Polecats, Refinery)
"get stuck" and require external nudging._

## Open questions (must be answered before `status: current`)

1. **What counts as "stuck"?** Candidate signals: (a) zero token usage for N
   minutes, (b) no `WorkflowRun.status.phase` transition for P reconciles,
   (c) ACP session idle for M minutes, (d) no git commit or output artifact
   for Q minutes. Which signals are load-bearing? How are they combined
   (`OR` / `AND` / weighted)? What are the default thresholds?
2. **Who supervises?** Options: (a) a sidecar per agent pod, (b) a
   controller-side CEL expression on `WorkflowRun.status`, (c) a scheduled
   Witness agent (`goose run --recipe=witness.yaml`) dispatched as its own
   `WorkflowRun`, (d) a dedicated controller reconciling `WorkflowRun`
   staleness into a `WitnessRun`. Which layer owns detection, and why?
3. **What is the escalation ladder?** Proposed default: Resume-nudge →
   re-prompt with context → restart pod → abort `WorkflowRun` with reason →
   page human via AlertManager. At which step does human notification happen?
   Does supervision token spend count against the supervised workspace's
   `TokenBudget`, a cluster-wide supervision budget, or both?
4. **Authorization for supervisors.** Does a `WitnessRun` inherit the
   supervised Workspace's identity, or carry its own limited SA? What is the
   OpenFGA tuple shape for witness relations (e.g.
   `workspace:X#supervised_by@witness:Y`)? What can a witness read/write on
   the supervised Workspace?
5. **Interaction with Argo retry semantics.** Argo Workflows already provides
   `onExit`, `retryStrategy`, and timeout handling per step (D20). Is
   supervision a *wrapper* around Argo's primitives, or an orthogonal concern
   that owns a different timescale (per-agent rather than per-step)? Avoid
   double-retry races.

## Load-bearing invariants

- **D24 — durable identity.** Agent state must survive pod restarts the
  supervisor triggers. Pod restart is a first-class supervision action
  because D24 guarantees no side-effect doubling on resume.
- **D25 — GUPP.** After a supervisor action, the controller MUST invoke
  `Resume(ctx, workspace)` to restart the work loop; otherwise the agent
  sits idle while work waits on the hook.

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md) — D24, D25
- [07-agent-runtime-spi.md](07-agent-runtime-spi.md)
- [18-process-lifecycle.md](18-process-lifecycle.md)
- [10b-token-accounting.md](10b-token-accounting.md)
- [04a-openfga-authz-model.md](04a-openfga-authz-model.md)
- [Steve Yegge — Welcome to Gas Town](https://steve-yegge.medium.com/welcome-to-gas-town-4f25ee16dd04) (inspiration)

TODO(design-gate)
