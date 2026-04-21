<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: reliability
depends:
  - 02-workspace-model.md
  - 04a-openfga-authz-model.md
  - 04c-token-revocation.md
  - 05a-envoy-ai-gateway-topology.md
  - 06-guardrailbinding.md
  - 07-agent-runtime-spi.md
  - 10b-token-accounting.md
  - 18-process-lifecycle.md
related_skills: []
status: current
last_verified: 2026-04-21
rollback: |
  Set `keese-supervision-defaults` ConfigMap `supervision.enabled: false`. In-flight
  witnesses complete naturally. Redeploy prior operator image for ladder defaults.
  No tuple migration — supervision tuples expire with witness WorkflowRun TTL (30 min).
---

# 23 — Agent Supervision (Patrol Pattern)

## Context

An agent pod can loop on a prompt and appear healthy to the kubelet. This design
codifies detection, nudging, and escalation — the B+C patrol pattern.
**Controller (B):** evaluates stuck signals every 30 s; after 2 consecutive concerned
ticks dispatches a **Witness (C)** — a `WorkflowRun` with `spec.witnessOf:
<target-workspace>`, recipe `witness-default`, one per (workspace, 10-min window).

## Stuck definition

Any single signal triggers `WorkspaceConcerned` (OR):

| Signal | Default threshold |
|---|---|
| Zero token usage | 10 min |
| No `WorkflowRun.status.phase` transition | 15 min (in-flight step) |
| ACP session idle | 5 min (work on hook, D25) |
| No git commit / artifact touch | 30 min (`expectsArtifacts: true`) |
| TokenBudget exhaustion event | Immediate |

Thresholds override via `Workspace.spec.supervision.overrides`; cluster defaults in
`ConfigMap keese-supervision-defaults` (`keese-system`). Argo retry backoff
(`WorkflowRun.status.argoRetryInFlight: true`) suppresses evaluation to prevent
false-positives during 3× retry cycles (≤ 5 min).

**Open cross-dep (02 iter-1):** `Workspace.spec.supervision` (overrides + escalationLadder) defined in 02 when current. **02 author MUST add `Workspace.spec.supervision.overrides` matching this threshold schema.**

## Controller (B) — `keese-supervisor-controller`

A reconciler inside the keese operator binary. Shares operator leader lease, OTEL
exporter, and `terminationGracePeriodSeconds: 60s` drain budget per design 18. On
SIGTERM drains alongside all other operator reconcilers within the same 60s window; no
separate drain protocol.

Reconciles `WorkflowRun` + `Workspace` every 30 s via `predicate.GenerationChangedPredicate`
+ time-based requeue. Emits `WorkspaceConcerned` + sets `conditions[SupervisorConcerned:
True]` on first stuck tick. After 2 consecutive ticks: checks
`Workspace.status.activeWitnessRef` — if nil or expired, dispatches witness via SSA
(`fieldOwner: keese-supervisor-controller`). Writes OpenFGA tuples at dispatch; deletes
on completion or 30-min TTL. RBAC: `get/list/watch` on `WorkflowRun`/`Workspace`;
`create/patch` on `WorkflowRun`; `patch` on `Workspace.spec.forceRevoke`.

## Witness agent (C)

`WorkflowRun.spec.witnessOf: <target-workspace>`. SA `ksa-witness-<witness-uid>`;
audience `keese-egress-supervisor-<tenant>` (05a locked) — separate from workspace
audience, preventing upstream model impersonation. Token budget:
`TokenBudget` CR `keese-supervision-<tenant>` (platform cost, NOT against workspace).
Budget exhaustion: skip step 3, jump to step 4, fire `SupervisionBudgetExhausted`.

### Witness GuardrailBinding: `witness-default`

Dedicated cluster-scoped `GuardrailBinding` in `keese-system`
(`keese-guardrail-cluster-admin` scope per 06); default cluster binding too restrictive.
`spec.tools.allow`: `kubectl.describe/logs/get`, `goose.session.dump`, `openfga.check`,
`workspace.patch.forceRevoke`. `spec.tools.deny`: `network.upstream: true` tools except
OpenFGA audit. `spec.tokenBudget`: unset; `spec.contentFilters`: Presidio.
`spec.scope: cluster` immutable; VAP rejects tenant overrides.
Installed by operator install Job. If deleted: `WitnessBindingMissing`; degrade to step 4.

## Escalation ladder

Default; overridable per `Tenant.spec.supervision.escalationLadder` (ordered):

| Step | Action | Actor | Delay | Notes |
|---|---|---|---|---|
| 1 | `AgentRuntime.Resume(ctx, workspace)` nudge (D25) | supervisor controller | 0s | Cheap GUPP; always first |
| 2 | Direct ACP re-prompt: "you appear stuck — what are you doing?" | supervisor controller | 60s after step 1 fails | Uses existing ACP session; no new workload |
| 3 | Dispatch witness `WorkflowRun` | supervisor controller | 120s after step 2 fails | Heavy diagnosis; 10-min dedup window |
| 4 | Restart agent pod | supervisor controller | Witness emits `Unstuck: false` | D24 durable identity; resume from checkpoint |
| 5 | Abort `WorkflowRun` with `SupervisorAborted` | witness (via status patch) | After 2× pod-restart attempts | Terminal for this run |
| 6 | Force-revoke Workspace | witness (via `spec.forceRevoke`) | Step 5 + agent still burning tokens | 04a `can_revoke` authz at admission |
| 7 | Page human via AlertManager | supervisor controller | Any step 5 | `WorkspaceStuckEscalated` alert |

**D25 Resume invariant:** step 4 restart MUST call `AgentRuntime.Resume(ctx, workspace)`. Timeout → `AgentUnresponsive`.

**Step 2 mechanism:** controller calls `AgentRuntime.InjectPrompt(ctx, session, prompt)` on the existing ACP session (no new workload). Gated on `CapabilitySupportsInjectPrompt`; if absent → `InjectPromptUnsupported` + proceed to step 3. Goose: synthetic `user` turn, `source: supervisor`. **07 iter-2 flag:** add `InjectPrompt(ctx, Session, string) error` + `CapabilitySupportsInjectPrompt bool` to `CapabilityMatrix` (minor SPI bump).

## Authorization and tuple shapes

Controller writes at dispatch; deletes on completion or 30-min TTL:
`workspace:W#supervised_by@witness:WIT` and `workspace:W#can_revoke@witness:WIT`.
`Check(workspace:<name>#can_revoke@witness:<uid>)` on `spec.forceRevoke` admission (04a,
`HIGHER_CONSISTENCY`, ≤ 15 ms). Deny → `ForbiddenToRevoke`. Witness read scope:
`get/watch` on supervised Workspace + WorkflowRuns + K8s events; `patch` only
`spec.forceRevoke.*`. Cannot read Memory backend or session artifacts (separate SA
audience, 05a). Shape enforced by `witness-default`; VAP rejects tenant overrides.

## Trade-offs

| Option | Decision | Rationale |
|---|---|---|
| Sidecar vs. controller detection | Controller (B) | Shared reconciler; no per-pod overhead |
| OR vs. AND signals | OR | Any single signal actionable; AND delays escalation on novel modes |
| Witness scope | Per workspace per 10-min window | Dedup prevents pile-up; window allows recovery |
| Supervision tokens | Separate `TokenBudget` | Platform cost isolation from user quota |
| Step 2 actor | Supervisor controller (not witness) | Cheap ACP re-prompt via existing session; no new workload |
| Witness GuardrailBinding | Dedicated `witness-default` | Default binding too restrictive for diagnostic tooling |

## Failure modes

| Failure | Mitigation |
|---|---|
| Supervisor loop failure | Operator restarts; re-evaluate on next reconcile |
| Witness stuck | 30-min hard TTL kills it; `WitnessStuck` alert |
| OpenFGA down at `can_revoke` | Deny fail-closed; fall back to step 4 (pod restart) |
| SIGKILL mid-step | D24 SQLite checkpoint; controller detects stale session, Resume |
| All workspaces stuck | Budget exhaustion → `SupervisionBudgetExhausted` alert |
| `witness-default` deleted | `WitnessBindingMissing`; degrade to step-4-only |
| `InjectPrompt` unsupported | `InjectPromptUnsupported`; skip step 2, dispatch witness |

## Upgrade / rollback

Same operator binary. Feature flag: `keese-supervision-defaults` ConfigMap
`supervision.enabled: false`. Tuple shape changes require 04a drain-and-rollout.
Witness `Recipe` updates versioned via `RecipeSource` OCI digest pinning (16).

## Observability

Events (in `internal/controller/workspace/events.go`): `WorkspaceConcerned`,
`WitnessDispatched`, `WitnessCompleted`, `WitnessStuck`, `AgentUnresponsive`,
`SupervisorAborted`, `SupervisionBudgetExhausted`, `WorkspaceStuckEscalated`,
`WitnessBindingMissing`, `InjectPromptUnsupported`.

OTEL span: `supervisor.evaluate` (`workspace`, `tenant`, `signals_triggered`,
`action_taken`, `witness_uid`). Metric:
`keese_supervision_escalation_total{tenant,workspace,step}`. Alert:
`WorkspaceStuckEscalated` (step 5+, pages on-call).

## Refs

- [02-workspace-model.md](02-workspace-model.md) — `spec.supervision.overrides` (stub; hard dep)
- [04a-openfga-authz-model.md](04a-openfga-authz-model.md) — `supervised_by`, `can_revoke` tuples
- [04c-token-revocation.md](04c-token-revocation.md) — `spec.forceRevoke` admission; `revocationMode`
- [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md) — `keese-egress-supervisor-<tenant>`
- [06-guardrailbinding.md](06-guardrailbinding.md) — `witness-default` role model; cluster-scope VAP
- [07-agent-runtime-spi.md](07-agent-runtime-spi.md) — `Resume` D25 GUPP; `InjectPrompt` flagged iter-2
- [10b-token-accounting.md](10b-token-accounting.md) — supervision `TokenBudget`; exhaustion → step skip
- [18-process-lifecycle.md](18-process-lifecycle.md) — operator 60s drain budget
- [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md) — D24, D25
- [../plans/rubric.md](../plans/rubric.md)
- [Steve Yegge — Welcome to Gas Town](https://steve-yegge.medium.com/welcome-to-gas-town-4f25ee16dd04)

## Iteration log

### Iteration 1 — 2026-04-21 — Score **87.5** — Verdict: SHIP (held draft)

Five concerns unresolved: (1) ladder step-2 actor contradiction (witness before dispatch); (2) controller drain budget open; (3) `spec.supervision.overrides` schema not deferred to 02; (4) witness `GuardrailBinding` unspecified; (5) step-2 delivery mechanism vague.

### Iteration 2 — 2026-04-21 — Score **91.5** — Verdict: SHIP

| # | Category | Wt | Ratio | Score |
|---|---|---:|---:|---:|
| 1 | Scope clarity | 10 | 1.0 | 10 |
| 2 | Architecture fit | 10 | 1.0 | 10 |
| 3 | Security posture | 15 | 1.0 | 15 |
| 4 | Automatability | 10 | 0.65 | 6.5 |
| 5 | Verifiability | 15 | 0.75 | 11.25 |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 |
| 7 | Context efficiency | 10 | 1.0 | 10 |
| 8 | Docs quality | 5 | 1.0 | 5 |
| 9 | Observability | 5 | 1.0 | 5 |
| 10 | Operational readiness | 10 | 1.0 | 10 |
| | **Total** | 100 | | **91.5** |

Honest gaps: Cat 4 (−3.5): `InjectPrompt` adds 07 iter-2 scaffolding item; binding
install Job named but not scripted (pre-gate). Cat 5 (−3.75): 5 named test cases
unimplemented (pre-gate per P8): stuck-signal unit, ladder-step envtest, binding-missing
path, InjectPromptUnsupported fallback, ACP re-prompt audit turn.

Cross-dep flags: (1) **02 iter-1 MUST add** `Workspace.spec.supervision.overrides`
matching threshold schema. (2) **07 iter-2 MUST add** `InjectPrompt(ctx, Session,
string) error` + `CapabilitySupportsInjectPrompt bool`.
