<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: authz
depends: [01-tenancy-capsule.md, 02-workspace-model.md, 20a-api-group-layout.md]
related_skills: []
status: draft
last_verified: 2026-04-20
rollback: |
  Revert model.fga to the prior git-tagged version. Replay tuple writes from ES
  index keese-openfga-audit-* using internal/rebac/cmd/replay/main.go against the
  reverted model ID. Document in docs/plans/migration-rebac-<version>.md and bump
  the version-tagged cache key in internal/rebac/cache.go (see 04c).
---

# 04a — OpenFGA Authorization Model

## Context

Every egress request from an agent pod must be authorized before the Envoy AI Gateway
injects credentials. Authorization is ReBAC: the right to call a tool or reach a model
is a *relation* on a typed object, not a flat ACL. OpenFGA (CNCF Incubating, Apache-2.0)
provides the DSL, tuple store, and check API. This design specifies canonical types,
relations, consistency tiers, model-ID propagation, and the failure-closed contract.
04b owns user-shape (projected SA audience); 04c owns revocation latency SLO and
cache invalidation.

## Types

Kind names align with `docs/designs/20a-api-group-layout.md`.

| OpenFGA type | Keese entity | CRD / primitive |
|---|---|---|
| `user` | Human operator or CI identity | K8s RBAC subject |
| `service_account` | Per-workspace projected SA | K8s ServiceAccount (projected) |
| `tenant` | Capsule Tenant (D3) | `capsule.clastix.io/v1beta2/Tenant` |
| `workspace` | Agent execution scope | `workspace.operator.keese.ai/Workspace` |
| `workspace_share` | Cross-tenant access grant | `workspace.operator.keese.ai/WorkspaceShare` |
| `tool` | Callable MCP function | `runtime.operator.keese.ai/RuntimeExtension` |
| `model_gate` | Upstream model allow/deny scope | ConfigMap `keese-model-gates/<tenant>` |
| `memory` | Memory / SharedMemory backend | `memory.operator.keese.ai/{Memory,SharedMemory}` |
| `witness` | Supervision agent (D23, D24) | `WorkflowRun` with supervision recipe |

## Relations

Every CRD field writing a tuple carries `// +keese:rebac-tuple=<relation>` (rule 04.14).
04b owns final user-shape validation. Placeholder SA form: `service_account:ws-<name>-sa`.

**tenant** — `admin`, `member` (includes admin), `viewer` (includes member). Workspace
controller writes `tenant:X#member@service_account:Y` on workspace create.

**workspace** — `owner@tenant`, `admin@user`, `editor@user`, `viewer@user`,
`supervised_by@witness` (written by supervision controller, D23). Computed: `can_run=editor`,
`can_read_memory=viewer`, `can_write_memory=editor`. Workspace identity is durable across
pod churn (D24): tuples survive pod restarts; deleted only on CR deletion via finalizer
`finalizers.workspace.operator.keese.ai/rebac`.

**workspace_share** — `grantor@workspace`, `grantee@tenant`, `can_access@service_account`.
`can_access` requires both `grantee` membership and the share object to exist. Written by
WorkspaceShare controller after ReferenceGrant validation.

**tool** — `owner@workspace` (RuntimeExtension binding), `allowed_in@workspace`,
`can_call@service_account`. `can_call` requires `allowed_in` AND SA tenant membership.
GuardrailBinding controller writes `allowed_in`; RuntimeExtension controller writes `owner`.

**model_gate** — `allows@workspace`, `denies@workspace`. Deny wins: check evaluates
`allows AND NOT denies`. Tenant admin writes via `keese-tenant-admin` RBAC aggregate.

**memory** — `owner@workspace`, `reader@{user,service_account}`, `writer@{user,service_account}`.
For SharedMemory: SharedMemory controller writes `reader@workspace` for each permitted workspace
after OpenFGA share confirmation.

**witness** (D23, forward-compatible) — `can_read@workspace` only; write-scope restricted to
the supervision controller SA. Details deferred to `23-agent-supervision.md`.

## Consistency Policy

| Check type | Tier | Latency budget |
|---|---|---|
| Tool `can_call` on egress (hot path) | `HIGHER_CONSISTENCY` | ≤ 15 ms p99 |
| Workspace `viewer/editor` reads | Eventual | ≤ 5 ms p99 |
| Tenant `admin/member` checks | Eventual | ≤ 5 ms p99 |
| Revocation checks (suspension) | `HIGHER_CONSISTENCY` | ≤ 15 ms p99 |
| Model gate allow/deny on egress | `HIGHER_CONSISTENCY` | ≤ 15 ms p99 |

04c will codify the ≤ 60 s revocation latency SLO. This doc commits to
`HIGHER_CONSISTENCY` for all revocation-relevant checks.

## Model Versioning and Propagation

The active `model_id` is propagated via ConfigMap `keese-system/openfga-model` (key `modelID`):

1. **Bootstrap:** `dev/bootstrap/openfga/seed.yaml` Job creates the store, writes the model,
   and stores `modelID` in the ConfigMap.
2. **Controllers:** read the ConfigMap at startup; re-cache on informer change without restart.
3. **Ext_authz:** reads the ConfigMap at startup and on SIGHUP; passes `modelID` on every check.
4. **Upgrade:** new `model.fga` ships in a release; the upgrade Job writes the new `modelID`
   to the ConfigMap; controllers and ext_authz observe within one resync cycle
   (default 10 min; tunable via `--model-id-resync-period`).

## Failure-Closed Contract

When OpenFGA is unavailable: ext_authz returns HTTP `503` to Envoy; the agent sees a failed
tool call, not a bypass. Event reason `OpenFGAUnavailable` is emitted on the Workspace.
AlertManager fires when `keese_openfga_check_error_total > 5` in 60 s. No cached-allow
decision is served past TTL — expired entries are hard-denied (rule 05).

## Migration Path to SpiceDB

SpiceDB is preferable above ~100 M tuples or when zed-token cross-region consistency is needed.
Migration: export tuples via `internal/rebac/cmd/export/main.go` → transform via
`internal/rebac/cmd/migrate/main.go` → `zed import` → translate DSL → cut over controllers
and ext_authz → document in `docs/plans/migration-rebac-spicedb.md`. Threshold trigger:
architect review when `keese_openfga_tuple_total` sustains > 80 M for 30 days.

## Trade-offs

OpenFGA chosen over SpiceDB (heavier ops, deferred to >100M tuples), Ory Keto (anemic
2024–2026 releases), and flat RBAC (cannot model cross-tenant sharing without namespace
explosion). Caching allow on outage rejected — violates fail-closed invariant (rule 05).

## Failure Modes

| Failure | Detection | Mitigation |
|---|---|---|
| OpenFGA pod crash | `keese_openfga_check_error_total` spike | PDB `minAvailable=1`; 2 replicas in prod |
| Stale tuple after workspace delete | Finalizer `finalizers.workspace.operator.keese.ai/rebac` | Blocks CR deletion until tuples removed |
| Model ID ConfigMap deleted | Controller logs `OpenFGAModelIDMissing`; pauses tuple writes | Admission webhook validates ConfigMap presence |
| Witness over-writes | RBAC on tuple-writer SA; VAP rejects | Witness SA scoped to `witness:*` objects only |
| Cross-tenant share without ReferenceGrant | WorkspaceShare controller validates ReferenceGrant | VAP rejects WorkspaceShare without grant in target ns |

## Observability

- **OTEL span:** `openfga.check` (`tuple`, `relation`, `decision`, `consistency_tier`, `latency_ms`). No token values or bodies (rules 05.10, 02).
- **Metrics:** `keese_openfga_check_duration_seconds{relation,decision}`, `keese_openfga_check_error_total{reason}`, `keese_openfga_tuple_total{type}`, `keese_openfga_model_id_age_seconds`.
- **Events:** `OpenFGAUnavailable`, `TupleWriteFailed`, `TupleDeleteFailed`, `ModelIDUpdated`, `RevocationCheckFailed`.
- **Audit log:** ES index `keese-openfga-audit-*`; fields: `tuple`, `sa`, `host`, `decision`, `upstream_status`, `model_id`. No token fields.

## Refs

- [01-tenancy-capsule.md](01-tenancy-capsule.md) — ClusterRole scaffold; Mode A/B
- [20a-api-group-layout.md](20a-api-group-layout.md) — kind names
- [23-agent-supervision.md](23-agent-supervision.md) — witness/supervisor relations (D23)
- [04b-projected-sa-identity.md](04b-projected-sa-identity.md) — SA audience / OIDC trust
- [04c-token-revocation.md](04c-token-revocation.md) — revocation SLO + cache invalidation
- [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md) — ext_authz integration
- [../specs/egress-authz-protocol.md](../specs/egress-authz-protocol.md) — tuple shape contract
- [../../dev/bootstrap/openfga/model.fga](../../dev/bootstrap/openfga/model.fga) — live DSL
- [../plans/rubric.md](../plans/rubric.md)
- [D4, D13, D23, D24, D25 in scaffolding-plan.md](../plans/scaffolding-plan.md)

## Iteration Log

### Iteration 1 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal stated; all required tuple shapes covered; 04b/04c boundary explicit |
| 2 | Architecture fit | 10 | 1.0 | 10 | D4/D13/D23/D24/D25 honored; Mode A/B covered; no tenant CRD; compose-over-replicate |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed 503; deny wins on model_gate; no tokens in audit; witness write-scoped |
| 4 | Automatability | 10 | 0.5 | 5 | model.fga updated; seed Job path stated; migration scripts named but unimplemented (pre-gate) |
| 5 | Verifiability | 15 | 0.5 | 7.5 | tests/openfga/*.yaml deferred to spec phase; acceptance criteria in egress-authz-protocol.md |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Five failure modes with detection + mitigation; finalizer pattern; PDB |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Under 200 lines; 04b/04c cross-refs short; no inline code |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete with concrete rollback; last_verified updated |
| 9 | Observability | 5 | 1.0 | 5 | OTEL spans, metrics, events, audit log; no token fields |
| 10 | Operational readiness | 10 | 1.0 | 10 | ConfigMap propagation; zero-downtime upgrade; SpiceDB threshold named; PDB |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP (92.5 ≥ 90 tier-2 bar)

Top gaps:
1. `tests/openfga/*.yaml` positive/negative assertions not yet authored — deferred to spec phase; rebac-modeler authors on gate open.
2. Migration scripts (`export`, `migrate` under `internal/rebac/cmd/`) named but unimplemented — pre-gate acceptable.
3. 04b must confirm final SA tuple form (`service_account:<ksa-name>` audience template) — flagged as cross-dep.

Cross-dependencies:
- **04b** owns SA user-shape: audience template `keese-egress-<tenant>`, OIDC trust anchoring, TTL.
- **04c** owns revocation latency SLO and `internal/rebac/cache.go` version-tagged cache bump; this doc commits only to `HIGHER_CONSISTENCY` tier.
- **23-agent-supervision.md** owns witness read/write scope; `supervised_by@witness` is reserved in model.fga but details are intentionally deferred.
