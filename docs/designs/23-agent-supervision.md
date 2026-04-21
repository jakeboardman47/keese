<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: reliability
depends:
  - 04a-openfga-authz-model.md
  - 04c-token-revocation.md
  - 05a-envoy-ai-gateway-topology.md
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

Kubernetes liveness probes detect stuck pods; they do not detect stuck agents. An
agent pod can loop on a prompt, burn tokens, and appear perfectly healthy to the
kubelet. This design codifies how keese detects, nudges, and escalates stuck
AgentRuntimes — the B+C patrol pattern.

**B+C pattern:**

1. **Controller (B) — cheap, always-on.** Evaluates composite stuck signals every
   30 s; after 2 consecutive concerned ticks dispatches a witness.
2. **Witness agent (C) — expensive, rare.** A `WorkflowRun` with
   `spec.witnessOf: <target-workspace>` and recipe `witness-default`. One active
   witness per (workspace, 10-min window); executes the escalation ladder.

## Stuck definition

Any single signal triggers `WorkspaceConcerned` (OR combination):

| Signal | Default threshold | Rationale |
|---|---|---|
| Zero token usage | 10 min | Agent looped without LLM calls — dominant runaway mode |
| No `WorkflowRun.status.phase` transition | 15 min (in-flight step) | Argo step stalled post-retry exhaustion |
| ACP session idle | 5 min (work on hook) | D25 GUPP violation |
| No git commit / artifact touch | 30 min (`expectsArtifacts: true`) | Output-producing workspaces stuck without output |
| TokenBudget exhaustion event | Immediate | 10b signals; no time window needed |

Thresholds override via `Workspace.spec.supervision.overrides`; cluster defaults in
`ConfigMap keese-supervision-defaults` (`keese-system`).

**Argo retry exclusion:** controller excludes WorkflowRuns whose current step is in
Argo retry backoff (`WorkflowRun.status.argoRetryInFlight: true`). This prevents
false-positive stuck detection during normal Argo 3× retry cycles (≤ 5 min).

## Controller (B) — `keese-supervisor-controller`

Separate reconcile loop inside the keese operator binary (shared leader lease + OTEL
exporter). Reconciles `WorkflowRun` + `Workspace` every 30 s via
`predicate.GenerationChangedPredicate` + time-based requeue. Emits
`WorkspaceConcerned` event and sets `conditions[SupervisorConcerned: True]` on first
stuck tick. After 2 consecutive concerned ticks: checks
`Workspace.status.activeWitnessRef` — if nil or expired, dispatches witness
`WorkflowRun` via SSA (`fieldOwner: keese-supervisor-controller`). Writes OpenFGA tuples
at dispatch; deletes on witness completion or 30-min TTL expiry.
RBAC: `get/list/watch` on `WorkflowRun`/`Workspace`; `create/patch` on `WorkflowRun`;
`patch` on `Workspace.spec.forceRevoke`.

## Witness agent (C)

`WorkflowRun.spec.witnessOf: <target-workspace>` marks it as a supervision run.
Recipe `witness-default` constrained toolset: `kubectl describe/logs` on supervised
resources; `goose session dump`; OpenFGA `Check`; patch `Workspace.spec.forceRevoke`.
SA `ksa-witness-<witness-uid>`; audience `keese-egress-supervisor-<tenant>` (05a
locked) — separate from workspace audience, preventing upstream model impersonation.
Token budget: `TokenBudget` CR `keese-supervision-<tenant>` in `keese-system`
(platform cost, separate NATS KV key — NOT counted against supervised workspace).
Witness budget exhaustion: supervisor skips step 3, jumps to step 4, fires
`SupervisionBudgetExhausted` alert.

## Escalation ladder

Default; overridable per `Tenant.spec.supervision.escalationLadder` (ordered):

| Step | Action | Delay | Notes |
|---|---|---|---|
| 1 | `AgentRuntime.Resume(ctx, workspace)` nudge (D25) | 0 s | Cheap; always first; D25 GUPP contract |
| 2 | Re-prompt: inject diagnostic context into session | 60 s after step 1 fails | Witness posts "you appear stuck — what are you doing?" |
| 3 | Dispatch witness `WorkflowRun` | 120 s after step 2 | Heavy diagnosis; dedup 1 per workspace per 10 min |
| 4 | Restart agent pod (controller-driven) | After witness emits `Unstuck: false` | D24 durable identity; resume from SQLite checkpoint |
| 5 | Abort `WorkflowRun` (`SupervisorAborted` reason) | After 2× pod-restart attempts | Witness escalates via status patch |
| 6 | Force-revoke Workspace (`spec.forceRevoke`) | Step 5 + agent still consuming tokens | 04a `can_revoke` authz check at admission |
| 7 | Page human via AlertManager `WorkspaceStuckEscalated` | Any step 5 occurrence | Ensures human visibility on abort |

**D25 Resume invariant:** after step 4 pod restart, controller MUST call `AgentRuntime.Resume(ctx, workspace)`. Timeout → event `AgentUnresponsive`.

## Authorization and tuple shapes

Supervision controller writes at witness dispatch; deletes on witness completion or
30-min TTL:

| Tuple | Written by | When |
|---|---|---|
| `workspace:W#supervised_by@witness:WIT` | Supervisor controller | Witness dispatched |
| `workspace:W#can_revoke@witness:WIT` | Supervisor controller | Witness dispatched |

OpenFGA `Check` on `Workspace.spec.forceRevoke` admission (04a, `HIGHER_CONSISTENCY`,
≤ 15 ms): `Check(workspace:<name>#can_revoke@witness:<uid>)`. Deny → `ForbiddenToRevoke`.

Witness read scope: `get/watch` on supervised Workspace + its WorkflowRuns and K8s
events; `patch` only `spec.forceRevoke.*` on the supervised workspace. Witness cannot
read the supervised workspace's Memory backend or session artifacts — separate SA audience
enforces at the gateway (05a).

## Argo retry interaction

Argo manages per-step retry within a WorkflowRun — timescale seconds to minutes.
Supervision manages per-Workspace across WorkflowRuns — timescale 10+ minutes.
These are orthogonal. The stuck detector reads `WorkflowRun.status.argoRetryInFlight`
(boolean set by the Argo delegation controller) and skips evaluation while true,
preventing double-retry races.

## Trade-offs

| Option | Decision | Rationale |
|---|---|---|
| Sidecar vs. controller detection | Controller (B) | Shared reconciler; no per-pod overhead |
| OR vs. AND signals | OR | Any single signal actionable; AND delays escalation on novel modes |
| Witness scope | Per workspace per 10-min window | Dedup prevents pile-up; window allows recovery |
| Supervision tokens | Separate `TokenBudget` | Platform cost isolation from user quota |
| Witness audience | `keese-egress-supervisor-<tenant>` | Prevents witness impersonating workspace upstream calls |

## Failure modes

| Failure | Mitigation |
|---|---|
| Supervisor controller loop failure | Operator restarts it; no data lost — stuck signals re-evaluate on next reconcile |
| Witness `WorkflowRun` stuck itself | Supervisor controller has a 30-min hard TTL; kills witness, fires `WitnessStuck` alert |
| OpenFGA down at `can_revoke` check | Deny (fail-closed); supervisor falls back to pod restart (step 4) without revoke |
| SIGKILL of agent mid-step | D24: SQLite on PVC is checkpoint; controller detects stale session, re-runs Resume |
| Cascade: all workspaces stuck | Witness budget exhaustion fires `SupervisionBudgetExhausted`; page human |

## Upgrade / rollback

Supervision controller ships in the same operator binary. Feature flag:
`keese-supervision-defaults` ConfigMap `supervision.enabled: false` disables evaluation
without redeploying. Tuple shape changes require 04a model migration (drain-and-rollout).
Witness `Recipe` updates are versioned via `RecipeSource` OCI digest pinning (16).

## Observability

Events (in `internal/controller/workspace/events.go`): `WorkspaceConcerned`,
`WitnessDispatched`, `WitnessCompleted`, `WitnessStuck`, `AgentUnresponsive`,
`SupervisorAborted`, `SupervisionBudgetExhausted`, `WorkspaceStuckEscalated`.

OTEL span: `supervisor.evaluate` (`workspace`, `tenant`, `signals_triggered`,
`action_taken`, `witness_uid`). Metric:
`keese_supervision_escalation_total{tenant,workspace,step}`. Alert:
`WorkspaceStuckEscalated` (step 5+, pages on-call).

## Refs

- [04a-openfga-authz-model.md](04a-openfga-authz-model.md) — `supervised_by`, `can_revoke` tuples locked
- [04c-token-revocation.md](04c-token-revocation.md) — `spec.forceRevoke` admission path; `revocationMode`
- [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md) — `keese-egress-supervisor-<tenant>` audience locked
- [07-agent-runtime-spi.md](07-agent-runtime-spi.md) — `Resume(ctx, workspace)` D25 GUPP contract (stub; flagged)
- [10b-token-accounting.md](10b-token-accounting.md) — separate supervision `TokenBudget`; exhaustion → step skip
- [18-process-lifecycle.md](18-process-lifecycle.md) — drain budgets + SQLite checkpoint bound detection thresholds (stub; flagged)
- [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md) — D24, D25
- [../plans/rubric.md](../plans/rubric.md)
- [Steve Yegge — Welcome to Gas Town](https://steve-yegge.medium.com/welcome-to-gas-town-4f25ee16dd04)

## Iteration log

### Iteration 1 — 2026-04-21 — Score **87.5** — Verdict: SHIP

| # | Category | Wt | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 5 signals + thresholds; 7-step ladder; exit criteria explicit |
| 2 | Architecture fit | 10 | 1.0 | 10 | D24/D25 aligned; OR signals; Argo orthogonality; no rule violations |
| 3 | Security posture | 15 | 1.0 | 15 | Separate witness audience; fail-closed revoke; no kubeconfig in witness; tuples TTL-expire |
| 4 | Automatability | 10 | 0.5 | 5 | Ladder + ConfigMap defaults specified; controller + Recipe named but not yet authored |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Concrete thresholds/tuples/modes; no envtest harness or kuttl scenario yet |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Loop failure, recursive-witness, OpenFGA-down, SIGKILL, cascade — all mitigated |
| 7 | Context efficiency | 10 | 1.0 | 10 | Single file; linked deps; no inline code |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; 6 required deps listed; `status: current` |
| 9 | Observability | 5 | 1.0 | 5 | 8 events; OTEL span + metric + on-call alert |
| 10 | Operational readiness | 10 | 1.0 | 10 | Feature flag; rollback; witness TTL; OCI-pinned Recipe; tuple cleanup |
| | **Total** | 100 | | **87.5** | |

Honest gap note: Cat 4 (−5) and Cat 5 (−7.5) are structural — supervisor controller
and test harness are not yet authored (pre-gate acceptable per P8). All design decisions
are settled; cross-deps locked. Iter-2 closes Cat 4/5 after controller phase opens.

Top gaps: (1) envtest + kuttl for stuck-signal and ladder steps; (2) supervisor
controller reconciler + `witness-default` Recipe; (3) 07 `Resume` signature and 18 drain
budgets are stubs — iter-2 cross-checks when both reach `current`.
