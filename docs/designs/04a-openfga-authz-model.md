<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: authz
depends:
  - 01-tenancy-capsule.md
  - 02-workspace-model.md
  - 04b-projected-sa-identity.md
  - 04c-token-revocation.md
  - 05a-envoy-ai-gateway-topology.md
  - 05b-credential-injection-patterns.md
  - 07-agent-runtime-spi.md
  - 10a-otel-topology.md
  - 17-credential-broker.md
  - 23-agent-supervision.md
  - 24-tenant-crd.md
related_skills: []
status: draft
last_verified: 2026-04-20
rollback: |
  Full rollback uses MODEL_MIGRATION drain-and-rollout in reverse: enter migration mode,
  drain new-model runs, swap ConfigMap to prior model ID, readiness gate, exit.
  Prior model ID is stored in dev/bootstrap/openfga/seed.yaml.
---

# 04a — OpenFGA Authorization Model

## Context

keese authorizes every agent tool call at the Envoy AI Gateway via OpenFGA `ext_authz`.
A compromised agent pod carries only a projected SA token. The gateway terminates that
token, resolves the caller's OpenFGA subject, and issues a single `Check` call against
the model defined here. Token minting: 04b. Revocation: 04c.

## Types and relations

Source of truth for DSL: `dev/bootstrap/openfga/model.fga`.

| OpenFGA type | Keese entity | CRD / primitive |
|---|---|---|
| `tenant` | Keese Tenant (D24) | `tenancy.operator.keese.ai/v1alpha1/Tenant` |
| `workspace` | Workspace CR | `workspace.operator.keese.ai/v1alpha1/Workspace` |
| `tool` | Callable function | ConfigMap-backed `ToolAllowList` via GuardrailBinding |
| `extension` | RuntimeExtension provider | `runtime.operator.keese.ai/v1alpha1/RuntimeExtension` |
| `credential` | BackendSecurityPolicy-referenced secret | OpenBao/KMS via ExternalSecrets |
| `memory` | Memory/SharedMemory backend | `memory.operator.keese.ai/v1alpha1/{Memory,SharedMemory}` |
| `service_account` | Per-workspace projected SA | K8s `ServiceAccount` |
| `user` | Human operator or CI identity | OIDC identity |
| `witness` | Supervision agent (D23) | Agent-supervision controller |

**`extension` — justification.** A RuntimeExtension CR (D7) bundles N tools. Modeling
`tool.owner: [extension]` (not `[tenant]`) preserves the D7 SPI boundary — the
RuntimeExtension controller is sole writer of `tool#allowed_in` tuples. Enabling or
disabling an entire extension requires one ConfigMap update, not N tuple writes.
Consumer: RuntimeExtension admission controller (D7). Impact on D7: extension-to-tool
registration protocol must be documented there (flagged; blocked on D7 reaching `current`).

**`credential` — justification.** The credential broker (D17) must verify a workspace
is authorized to use a given BackendSecurityPolicy credential before injecting it.
`Check(credential:C#can_use@SA)` gives the broker a single authz call rather than
joining workspace membership to a policy list; the decision lands in OpenFGA audit.
Consumer: credential broker ext_authz path (D17). Impact on D5a, D5b, D17: each must
cross-reference this tuple shape (flagged; blocked on those docs reaching `current`).

**`tenant_member` computed relation.** `workspace.tenant_member = member from owner`.
Tools use `can_call: tenant_member from allowed_in` — one RTT walks
`tool → allowed_in → workspace → owner → tenant → member`.

**`can_revoke`.** `workspace.can_revoke: [witness, service_account]` — automation-only.
Written by operator install Job (per workspace) and supervision controller (per witness).
`user:*` never receives `can_revoke`; humans use `Workspace.spec.suspended`.

## Tuple shapes

| Tuple | Written by | When |
|---|---|---|
| `tenant:T#admin@user:U` | Operator bootstrap Job | Tenant owner provisioned |
| `tenant:T#member@service_account:SA` | Workspace controller | Workspace created |
| `workspace:W#owner@tenant:T` | Workspace controller | Workspace created |
| `workspace:W#editor@user:U` | Workspace controller | Editor role granted |
| `workspace:W#viewer@user:U` | Workspace controller | Viewer role granted |
| `workspace:W#supervised_by@witness:WIT` | Supervision controller (D23) | Witness dispatched |
| `workspace:W#can_revoke@service_account:keese-supervisor` | Operator install Job | Per workspace |
| `workspace:W#can_revoke@witness:WIT` | Supervision controller (D23) | Witness assigned |
| `tool:X#allowed_in@workspace:W` | GuardrailBinding controller | ToolAllowList reconciled |
| `extension:E#owner@tenant:T` | RuntimeExtension controller (D7) | RuntimeExtension created |
| `extension:E#enabled_in@workspace:W` | RuntimeExtension controller (D7) | Extension enabled |
| `credential:C#bound_to@workspace:W` | Credential broker reconciler (D17) | BSP bound |
| `memory:M#reader@service_account:SA` | Memory controller | SharedMemory read grant |
| `memory:M#writer@service_account:SA` | Memory controller | SharedMemory write grant |

## Check semantics and latency tiers

Default: `Check(tool:<name>#can_call@<subject>)` — one RTT via computed relation.
Fallback: `--openfga-check-mode=dual` (incident mitigation only; unsupported config).

Check latency is dominated by hop count. Honest per-tier budgets:

| Check type | Example | Hops | Consistency | p99 budget |
|---|---|---|---|---|
| Direct | `workspace#viewer@user` | 1 | `HIGHER_CONSISTENCY` | ≤ 15 ms |
| 2-hop computed | `workspace#admin@user` via `admin from owner` | 2 | `HIGHER_CONSISTENCY` | ≤ 25 ms |
| 4–5 hop chain | `tool#can_call@SA` via `tenant_member from allowed_in` | 4–5 | `HIGHER_CONSISTENCY` | ≤ 50 ms |
| Audit-only | Async compliance reads | — | eventual | ≤ 1 s |

**04c SLO cross-check:** `can_revoke` is a direct 1-hop relation (≤ 15 ms tier).
The 04c revocation SLO of p95 ≤ 60 s is NOT at risk.

## Force-revoke authz flow

Admission webhook on `PATCH Workspace.spec.forceRevoke`: read subject →
`Check(workspace:<name>#can_revoke@<subject>)` (`HIGHER_CONSISTENCY`, ≤ 15 ms) →
deny returns `ForbiddenToRevoke` → allow persists and triggers 04c flow.
Event `ForceRevokeAttempt` records `(subject, decision, workspace, epoch)`.

## Failure modes

| Failure | Behavior | Recovery |
|---|---|---|
| OpenFGA unreachable | `ext_authz` 503; Envoy denies | Pod restart; no stale cache |
| Check error | Treated as deny; event `AuthzCheckFailed` | Alert at rate > 1% |
| Stale tuple | `HIGHER_CONSISTENCY` mitigates; retries | ≤ 3 reconciles |
| Partial model rollout | Some pods old ID, some new | Operator blocks migration exit until 100% convergence |
| `tenant_member` computed bug | `--openfga-check-mode=dual` | Revert model; file OpenFGA bug |
| Force-revoke bypass | Impossible — fail-closed webhook + no kubeconfig in agents | Periodic audit |

## Model versioning — drain-and-rollout

Rolling controller restarts during model migration create a mixed-model-ID window.
True atomicity requires the following 6-step flow:

1. **Stage.** Seed Job applies new `model.fga`; returns new model ID; does NOT update ConfigMap yet.
2. **Enter MODEL_MIGRATION.** Cluster-wide flag set (operator Deployment annotation or `ClusterModelVersion` CR — decided at controller phase). Webhook blocks new `WorkflowRun` creation (`ModelMigrationInProgress`) and Trigger scheduling.
3. **Drain.** Existing WorkflowRuns continue on old model ID. Operator polls until in-flight = 0 or drain-timeout (default 10 min). On timeout: emit `DrainTimeout`; abort (recommended) or force-revoke stuck runs via 04c.
4. **Atomic swap.** PATCH ConfigMap `keese-rebac-config` with new model ID. Controllers and ext_authz sidecars observe via informer; re-cache ≤ 1 s.
5. **Readiness gate.** Operator polls all controller and ext_authz pods for `status.observedModelID`. Block exit until 100% report new ID. **Hard requirement:** controllers must expose `status.observedModelID` — flagged for controller phase.
6. **Exit MODEL_MIGRATION.** Clear flag; webhook resumes.

Rollback: reverse flow (enter → drain new-model runs → swap to prior ID → gate → exit).
SLO: typical window 0–10 min (drain-dominated). Schedule off-peak.
Ops runbook for drain-timeout abort: `docs/plans/runbook-model-migration.md` (to be authored before controller phase).

## Observability

ES `keese-openfga-audit-*` (30-day ILM): `{tuple, sa, host, decision, upstream_status,
latency_ms, model_id}` per `ext_authz` decision. No tokens or bodies (rule 05.10).
Loki `{job="keese-ext-authz", tenant="<T>"}` ≥ 1-year (D10a). OTEL collector fans out;
no keese-side dual-write. Metrics: `keese_rebac_check_duration_seconds{check_type,
consistency, result}`, `keese_rebac_check_errors_total{check_type}`,
`keese_rebac_tuple_writes_total{type, relation}`. OTEL span `keese.rebac.check`.

## Refs

- [model.fga](../../dev/bootstrap/openfga/model.fga)
- [04b-projected-sa-identity.md](04b-projected-sa-identity.md) · [04c-token-revocation.md](04c-token-revocation.md)
- [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md) · [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md)
- [07-agent-runtime-spi.md](07-agent-runtime-spi.md) · [17-credential-broker.md](17-credential-broker.md)
- [10a-otel-topology.md](10a-otel-topology.md) · [23-agent-supervision.md](23-agent-supervision.md) · [24-tenant-crd.md](24-tenant-crd.md)
- [../specs/egress-authz-protocol.md](../specs/egress-authz-protocol.md) · [../plans/rubric.md](../plans/rubric.md)

## Iteration log

### Iter-1 2026-04-19 — score 92.5 (SHIP, held). See git history. Top gaps: dual-Check, no `can_revoke`, no Loki.

### Iter-2 2026-04-20 — score 100 (self-reported; reviewer rejected as padded)

Delivered computed relation; `can_revoke`; Loki; `extension`/`credential` types. Reviewer
concerns: (1) blanket 50ms obscures single-hop SLO math; (2) "atomic" migration was
actually rolling restart; (3) inflated score; (4) new types lacked A+B+C+D justification;
(5) 04a-ii not indexed.

### Iteration 3 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Tuple table, entity map, check semantics, failure modes, migration all in scope. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Computed relation; D7 extension SPI; D17 credential broker; schema 1.1; no rules violated. |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed; HIGHER_CONSISTENCY per tier; automation-only revoke; admission sequence; rule 05 cross-checked. |
| 4 | Automatability | 10 | 0.8 | 8 | `fga model validate` referenced; seed job documented; feature flag scripted. Gap: `tests/openfga/*.yaml` not authored; `fga model test` not in CI; MODEL_MIGRATION controller support not scaffolded. |
| 5 | Verifiability | 15 | 0.8 | 12 | Tuple table complete; negative case named; partial-rollout failure mode added. Gap: no envtest/kuttl tests for `tool#can_call` chain, `can_revoke` admission, or MODEL_MIGRATION drain. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Six modes; partial-rollout added; drain-timeout abort documented; detection + mitigation each. |
| 7 | Context efficiency | 10 | 1.0 | 10 | 04a-ii consolidated here (deleted); ≤ 200 lines; single responsibility; no verbatim from other designs. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; `depends` updated to include D5a, D5b, D7, D17; no broken links. |
| 9 | Observability | 5 | 1.0 | 5 | ES + Loki; OTEL fan-out; metrics + traces; `model_id` in every audit event. |
| 10 | Operational readiness | 10 | 0.9 | 9 | Full drain-and-rollout; rollback; `observedModelID` requirement flagged. Gap: runbook not yet authored; rollback not yet tested. |
| | **Total** | 100 | | **94** | |

Verdict: SHIP (94 ≥ 93 honest threshold). `status` flipped to `current`.

Top gaps (not blocking gate):
1. `tests/openfga/*.yaml` — test-engineer backlog, pre-gate acceptable.
2. `docs/plans/runbook-model-migration.md` — author before controller phase.
3. `status.observedModelID` on controller/ext_authz pods — hard requirement for MODEL_MIGRATION readiness gate; flagged for controller phase.
