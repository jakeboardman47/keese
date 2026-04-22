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
  - 10b-token-accounting.md
  - 17-credential-broker.md
  - 23-agent-supervision.md
  - 24-tenant-crd.md
related_skills: []
status: current
last_verified: 2026-04-21
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
| `tenant` | Keese Tenant (D24) | `tenancy.operator.keese.ai/v1alpha1/Tenant`; identity key `Tenant.metadata.name` (not `.uid` — names are stable across delete+recreate, avoiding full tuple backfill on typo-fix cycles) |
| `workspace` | Workspace CR | `workspace.operator.keese.ai/v1alpha1/Workspace` |
| `tool` | Callable function | ConfigMap-backed `ToolAllowList` via GuardrailBinding |
| `extension` | RuntimeExtension provider | `runtime.operator.keese.ai/v1alpha1/RuntimeExtension` |
| `credential` | BackendSecurityPolicy-referenced secret | OpenBao/KMS via ExternalSecrets |
| `memory` | Memory/SharedMemory backend | `memory.operator.keese.ai/v1alpha1/{Memory,SharedMemory}` |
| `service_account` | Per-workspace projected SA | K8s `ServiceAccount` |
| `user` | Human operator or CI identity | OIDC identity |
| `witness` | Supervision agent (D23) | Agent-supervision controller |
| `oidc_provider` | OIDCProvider CR (D28) | `authz.operator.keese.ai/v1alpha1/OIDCProvider`; identity key `OIDCProvider.metadata.name` (cluster-scoped) |

**Per-tenant OIDCProvider gating (D28 / iter-6).** `tenant.uses_oidc_provider: [oidc_provider]` — written by the Tenant controller per `Tenant.spec.oidc.allowedProviders[]`; ext_authz denies tokens from issuers not in the allow-list. Cross-cuts: `authz.operator.keese.ai-v1alpha1.md` §1.6; D26/D24.

**Cross-tenant messaging relations (D29 / iter-5).** Two relations gate
**cross-tenant a2a** at the workspace granularity. Intra-tenant a2a is
**implicit** via Workflow definition (NATS topic existence within
`keese.tenant.<tenant-uid>.wf.<workflow-run-uid>.*` IS authz; topic
provisioning happens in the Workflow controller per design 03 iter-3,
constrained by the workflow's owning tenant).

- `tenant.allows_messaging: [tenant]` — directional grant from
  `T_to#allows_messaging@tenant:T_from`. Written exclusively by the
  **CrossTenantAgreement controller (D29 / design 25)** after both-side
  approval; manual writes via `fga write` are tolerated (out-of-band
  third-party authz workflows) and the controller no-ops when a tuple
  exists, emitting `OutOfBandTupleObserved`.
- `workspace.messageable_from: [workspace]` — workspace-pair grant.
  Written per (W_from, W_to) cartesian product expanded from each
  approved CrossTenantAgreement's `from.workspaceSelector` ×
  `to.workspaceSelector`. ext_authz at NATS / a2a transport (09)
  enforces via `Check(workspace:W_to#messageable_from@workspace:W_from)`
  before subscribe + first publish.

**`extension` — justification.** A RuntimeExtension CR (D7) bundles N tools. `tool.owner: [extension]` (not `[tenant]`) preserves the D7 SPI boundary; sole writer is the RuntimeExtension controller. One ConfigMap update enables/disables an entire extension. Impact on D7: extension-to-tool registration protocol flagged.

**`credential` — justification.** `Check(credential:C#can_use@SA)` gives the credential broker (D17) a single authz call. Every `credential.can_use` decision emits a token-accounting event consumed by `TokenBudget` per D10b. Impact on D5a, D5b, D10b, D17: each must cross-reference this tuple shape.

**`tenant_member` computed.** `workspace.tenant_member = member from owner`. `can_call: tenant_member from allowed_in` walks `tool → allowed_in → workspace → owner → tenant → member` in one RTT.

**`can_revoke`.** `workspace.can_revoke: [witness, service_account]` — automation-only. Written by operator install Job and supervision controller. `user:*` never receives `can_revoke`; humans use `Workspace.spec.suspended`.

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
| `tenant:T_to#allows_messaging@tenant:T_from` | CrossTenantAgreement controller (D29) | CRA reaches phase Approved |
| `workspace:W_to#messageable_from@workspace:W_from` | CrossTenantAgreement controller (D29) | Per (from × to) workspace pair on Approved |
| `tenant:T#uses_oidc_provider@oidc_provider:P` | Tenant controller (D24) | Per entry in `Tenant.spec.oidc.allowedProviders[]` |

## Check semantics and latency tiers

Default: `Check(tool:<name>#can_call@<subject>)` — one RTT. Fallback: `--openfga-check-mode=dual` (incident only).

| Check type | Hops | Consistency | p99 budget |
|---|---|---|---|
| Direct (`workspace#viewer@user`) | 1 | `HIGHER_CONSISTENCY` | ≤ 15 ms |
| 2-hop computed (`workspace#admin`) | 2 | `HIGHER_CONSISTENCY` | ≤ 25 ms |
| 4–5 hop (`tool#can_call@SA`) | 4–5 | `HIGHER_CONSISTENCY` | ≤ 50 ms |
| Audit-only | — | eventual | ≤ 1 s |

**04c SLO cross-check:** `can_revoke` is 1-hop (≤ 15 ms); p95 ≤ 60 s revocation SLO is NOT at risk.

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
2. **Enter MODEL_MIGRATION.** Cluster-wide flag set (operator Deployment annotation or `ClusterModelVersion` CR). Webhook blocks new `WorkflowRun` creation (`ModelMigrationInProgress`) and Trigger scheduling.
3. **Drain.** Existing WorkflowRuns continue on old model ID. Operator polls until in-flight = 0 or drain-timeout (default 10 min). On timeout: emit `DrainTimeout`; abort (recommended) or force-revoke stuck runs via 04c.
4. **Atomic swap.** PATCH ConfigMap `keese-rebac-config` with new model ID. Controllers and ext_authz sidecars observe via informer; re-cache ≤ 1 s.
5. **Readiness gate.** Operator polls all controller and ext_authz pods for `status.observedModelID`. Block exit until 100% report new ID. Controllers must expose `status.observedModelID` — flagged for controller phase.
6. **Exit MODEL_MIGRATION.** Clear flag; webhook resumes.

Rollback: reverse flow (enter → drain new-model runs → swap to prior ID → gate → exit).
SLO: typical window 0–10 min (drain-dominated). Schedule off-peak.
Ops runbook: `docs/plans/runbook-model-migration.md`.
Controller entry point (post-gate): `internal/controller/rebac/modelmigration_controller.go`.

## CI automation matrix

| Automation | Script | Runs in | Fail condition | Anchor |
|---|---|---|---|---|
| Model DSL syntax | `scripts/check-openfga-model.sh` (`fga model validate`) | pre-commit + `lint.yaml` | exits 2 if model.fga invalid | `.pre-commit-config.yaml` rebac section |
| Tuple assertions | `scripts/check-openfga-assertions.sh` (`fga model test`) | pre-commit + `test.yaml` | exits 2 if any YAML assertion fails | `tests/openfga/*.yaml` |
| ReBAC marker presence | `scripts/check-rebac-markers.sh` (existing) | pre-commit | exits non-zero if `+keese:rebac-tuple` absent on authz field | `.pre-commit-config.yaml` `rebac-markers` hook |
| MODEL_MIGRATION e2e | `make test-model-migration` | `test.yaml` (e2e matrix) | fails if drain does not reach in-flight=0 within 10 min or force-abort does not clear flag | `test/e2e/model_migration_drain_test.go` |

Test assertions and named test locations: see companion doc `04a-ii-testplan.md`.

## Observability

ES `keese-openfga-audit-*` (30-day ILM): `{tuple, sa, host, decision, upstream_status,
latency_ms, model_id}` per `ext_authz` decision. No tokens or bodies (rule 05.10).
Loki `{job="keese-ext-authz", tenant="<T>"}` ≥ 1-year (D10a). OTEL collector fans out;
no keese-side dual-write. Metrics: `keese_rebac_check_duration_seconds{check_type,
consistency, result}`, `keese_rebac_check_errors_total{check_type}`,
`keese_rebac_tuple_writes_total{type, relation}`. OTEL span `keese.rebac.check`.

## Refs

- [model.fga](../../dev/bootstrap/openfga/model.fga)
- [04a-ii-testplan.md](04a-ii-testplan.md) — named test assertions + CI automation detail
- [04b-projected-sa-identity.md](04b-projected-sa-identity.md) · [04c-token-revocation.md](04c-token-revocation.md)
- [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md) · [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md)
- [07-agent-runtime-spi.md](07-agent-runtime-spi.md) · [10b-token-accounting.md](10b-token-accounting.md) · [17-credential-broker.md](17-credential-broker.md)
- [10a-otel-topology.md](10a-otel-topology.md) · [23-agent-supervision.md](23-agent-supervision.md) · [24-tenant-crd.md](24-tenant-crd.md)
- [../specs/egress-authz-protocol.md](../specs/egress-authz-protocol.md) · [../plans/rubric.md](../plans/rubric.md)
- [../plans/runbook-model-migration.md](../plans/runbook-model-migration.md)

## Iteration log

### Iter-1 2026-04-19 — 92.5 SHIP held. Gaps: dual-Check, no `can_revoke`, no Loki.

### Iter-2 2026-04-20 — 100 self-reported; rejected (inflated). Added computed relation, `can_revoke`, Loki, types; migration was rolling, not atomic.

### Iter-3 2026-04-20 — 94 SHIP held at `draft`. Gaps: no `tests/openfga/*.yaml`, no CI for `fga model test`, MODEL_MIGRATION controller not scaffolded, runbook absent.

### Iter-4 2026-04-20 — 97 SHIP (reviewer-authorized cap override). Closed Cat 4/5/10 gaps; `status: current`. Detail + score table: [04a-iii-iter-log.md](04a-iii-iter-log.md).

### Iter-5 2026-04-21 — 97 SHIP (D29 spot-fix). Added `tenant.allows_messaging` + `workspace.messageable_from` relations + 2 tuple shapes for cross-tenant messaging. `status: current` retained. Detail + score table: [04a-iii-iter-log.md](04a-iii-iter-log.md).

### Iter-6 2026-04-21 — 97 SHIP (D28 spot-fix). Added `oidc_provider` type + `tenant.uses_oidc_provider` relation + 1 tuple shape for per-tenant OIDC allow-list gating. `status: current` retained. Detail + score table: [04a-iii-iter-log.md](04a-iii-iter-log.md).
