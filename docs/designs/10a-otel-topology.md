<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: observability
depends:
  - 01-tenancy-capsule.md
  - 04a-openfga-authz-model.md
  - 04c-token-revocation.md
  - 05a-envoy-ai-gateway-topology.md
  - 05b-credential-injection-patterns.md
  - 05c-mcp-policy-enforcement.md
  - 10b-token-accounting.md
  - 20a-api-group-layout.md
  - 24-tenant-crd.md
related_skills: []
status: current
last_verified: 2026-04-21
rollback: |
  Revert helmfile.lock pin of otel-collector chart; run `make bootstrap-infra`.
  Patch ConfigMap `otel-collector-config` to prior pipeline YAML via SSA
  (`fieldOwner: keese-otel-controller`); rolling restart picks it up within one
  reconcile. Annotation `keese.ai/force-exporter=apm|jaeger|debug` overrides
  auto-switch. Document in `docs/plans/migration-otel-<version>.md`.
---

# 10a — OTEL Topology

Iteration log: [10a-ii-iter-log.md](10a-ii-iter-log.md).

## Context

Every keese component (operator controllers, agent pods, Envoy AI Gateway
ext_authz/ext_proc) emits OTLP telemetry. This design specifies the collector
deployment topology, hybrid sampling strategy, APM fallback, fan-out to Elastic
APM / ECK / Prometheus / Loki, per-tenant trace isolation, mandatory resource
attributes, and the `keese-argument-redactor` processor. Settles open
questions flagged by 04a, 04c, 05a, 05b, 05c, and 24.

## Sampling strategy — Hybrid

Per-request Envoy AI Gateway spans make pure head-based sampling unsafe (rare
errors silently dropped). Tail-based alone requires unbounded buffering.

| Traffic class | Mechanism | Dev rate | Prod rate |
|---|---|---|---|
| Normal (non-error, within SLO) | Head probabilistic | 10% | 1% |
| Error spans (`status_code=ERROR`) | Tail 100% keep | 100% | 100% |
| Slow spans (p95 > 2 × SLO) | Tail 100% keep | 100% | 100% |
| Budget-exhausted / force-revoke | Tail 100% keep | 100% | 100% |

Tail-sampling: `decision_wait: 10s`; `num_traces: 50 000` (gateway tier),
`10 000` (controller tier). Buffer evicts oldest on overflow before accepting
new traces.

## Collector deployment topology

### Tier 1 — Gateway DaemonSet (`keese-otel-gateway`)

One pod per node. Envoy and `keese-ext-authz` pods emit OTLP to
`localhost:4317` (DaemonSet hostPort). Co-location is required for
tail-sampling correctness — cross-node span assembly produces split traces.
Resources: 1 vCPU / 2 Gi request; 2 vCPU / 4 Gi limit. Pipelines: traces
(tail-sample) → forward OTLP to Tier 2; metrics → Prometheus; logs → ES + Loki.

### Tier 2 — Controller Deployment (`keese-otel-controller`)

3 replicas; HPA on memory (80%); PDB `minAvailable: 2`. Receives OTLP from:
Tier 1 forwarders, agent pods (direct), operator controllers (direct). Applies
resource-attribute injection, argument redactor, tenant-isolation filter.
Service: `keese-otel-controller.keese-system.svc.cluster.local:4317`. Pipelines:
traces → Elastic APM (+ fallback); metrics → Prometheus; logs → ES + Loki.

### Agent pods (goose runtime)

Send OTLP directly to the Tier 2 Service. SIGTERM drain: SDK `ForceFlush` +
`Shutdown` within the 120 s agent grace period (D21/rule 06).

## APM fallback (D17)

Primary: `otlp/apm` exporter to
`keese-apm-apm-http.elastic-system.svc.cluster.local:8200`. Bearer token from
OpenBao path `kv/keese/system/apm-token` via ExternalSecret (D11 cross-ref).

Trigger: `otelcol_exporter_send_failed_spans_total{exporter="apm"}` > 0
sustained 30 s, OR APM liveness `/` returns non-200 for 3 checks.

Fallback chain: (1) Jaeger `jaeger-collector.elastic-system.svc:14268` if
deployed; (2) debug exporter (dev only); (3) drop + counter
`keese_otel_traces_dropped_total{reason="no_exporter"}` + event
`APMExportDegraded`. Recovery: 5 min of clean APM exports → restore primary.
Override: annotation `keese.ai/force-exporter=apm|jaeger|debug`.

## Per-tenant trace isolation

Tier 2 processor injects `keese.tenant=<T>` from `x-keese-tenant` header
(stamped by ext_authz per 05a). Spans still missing `keese.tenant` after
injection → routed to `keese-discard-*`; warning `MissingTenantAttribute`;
counter `keese_otel_discard_total{reason="missing_tenant"}`. Fail-closed.

Kibana: one Space per tenant; ES role `keese-tenant-<T>-viewer` grants `read`
on `keese-*-audit-*` with DLS `{"term":{"keese.tenant":"<T>"}}`. Role written
by Tenant controller via SSA (`fieldOwner: keese-tenant-controller`). Audit:
`scripts/check-tenant-isolation.sh` (post-gate).

## Mandatory resource attributes

| Attribute | Source |
|---|---|
| `service.name` | Binary constant (`keese-operator`, `keese-ext-authz`, `goose-runtime`) |
| `service.version` | Semver via `ldflags -X` |
| `service.instance.id` / `k8s.pod.uid` | Pod UID (downward API) |
| `k8s.namespace.name` / `k8s.pod.name` | Downward API |
| `keese.tenant` | Tier 2 processor from `x-keese-tenant` |
| `keese.workspace` | Tier 2 processor from `x-keese-workspace` when present |
| `keese.sa` | `ksa-<workspace-uid>` from SA name (workspace pods only) |
| `deployment.environment` | ConfigMap `keese-otel-env` key `env` |

**05b settled:** `keese.rebac.check` span carries `bsp`, `upstream_role`,
`exchange_result` (ok | sts_error | timeout | no_match). 05b iter-2 may cite
this as locked.

## Fan-out destinations

| Destination | Pipeline | Index / stream | Retention |
|---|---|---|---|
| Elastic APM | traces Tier 2 | — | 30-day hot, 90-day warm |
| Jaeger | traces fallback | — | 7-day |
| ES `keese-openfga-audit-*` | logs Tier 1+2 | ILM | 30-day |
| ES `keese-revocation-audit-*` | logs Tier 1+2 | ILM | 30-day |
| ES `keese-mcp-audit-*` | logs Tier 2 | ILM | 30-day |
| ES `keese-workspace-audit-*` | logs Tier 2 | ILM | 30-day |
| ES `keese-discard-*` | logs Tier 2 | ILM | 7-day |
| Loki `{job=keese-*}` | logs Tier 1+2 | object storage | ≥ 1 year |
| Prometheus | metrics Tier 1+2 | — | 30-day |

10b cross-ref: Prometheus is the authoritative source for token consumption
(`keese_token_budget_consumed_total{tenant, workspace, model, direction}`). The
`TokenBudget` reconciler queries Prometheus at its reconcile interval to compare
consumed vs. limit; NATS KV is used only as the boolean budget-exceeded signal
(not as a counter store). 10a owns the Prometheus pipeline; 10b owns the
TokenBudget reconcile logic. `keese_envoy_jwks_cache_fail_open_seconds_remaining{tenant}`
(24b) is a Prometheus gauge emitted by `keese-ext-authz`, scraped by Tier 1.

## `keese-argument-redactor` processor

Source: `cmd/otel-argument-redactor/` (Go). Loaded by Tier 2 only. Activates
when span name is `keese.mcp.tool_call` AND `Tenant.spec.auditArgumentsRedacted
== true` (read from ConfigMap `keese-tenant-audit-config`, refreshed via
informer). Flow: extract `mcp.arguments` → Presidio sidecar → set
`mcp.arguments.redacted` → drop original. If Presidio unavailable: drop
attribute entirely; emit `AuditRedactionUnavailable` event; increment
`keese_otel_redactor_unavailable_total{tenant}`. Rule 05.10: no tokens or
bodies ever reach logs.

## Trade-offs

| Option | Chosen | Rationale |
|---|---|---|
| DaemonSet gateway tier | Yes | Co-locates sampler with span sources; no cross-node fragmentation |
| Hybrid sampling | Yes | Head-only misses errors; tail-only requires 10× memory |
| Presidio for redaction | Yes | Auditable NER; fail-drop if unavailable (rule 05) |
| Single Tier 2 Service | Yes | Simplifies NetworkPolicy; agent pods avoid DaemonSet lifecycle dependency |

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| APM exporter down | `send_failed_spans_total > 0` sustained 30 s | Fallback chain; `APMExportDegraded` event |
| Tier 1 pod crash | Node trace gap | DaemonSet restarts; brief gap acceptable |
| Tier 2 pod crash | Span drop | PDB + HPA ≥ 2; OTLP SDK retry |
| Presidio crash | HTTP 5xx | Drop argument; `AuditRedactionUnavailable`; spans still exported |
| Tail-buffer overflow | `tail_sampling_cache_size` metric | Evict oldest; alert `OTELTailBufferFull`; scale Tier 1 |
| Missing `keese.tenant` | Discard counter | Route to `keese-discard-*`; ops review |
| SIGKILL before flush | Spans lost | Audit spans use `ForceFlush` on SIGTERM; SIGKILL loss documented |

## Upgrade and rollback

Collector chart pinned in `helmfile.lock`. Pipeline config in ConfigMap
`otel-collector-config`; patched via SSA. Processor upgrade: bump image in
DaemonSet/Deployment PodTemplateSpec via SSA rolling restart. Rollback: git
revert pin + config → `make bootstrap-infra`. 14b gap: CSV pinning of
otel-collector CRD — flag for 14b iter-1.

## Refs

- [04a](04a-openfga-authz-model.md) · [04c](04c-token-revocation.md)
- [05a](05a-envoy-ai-gateway-topology.md) · [05b](05b-credential-injection-patterns.md) · [05c](05c-mcp-policy-enforcement.md)
- [10b](10b-token-accounting.md) · [24](24-tenant-crd.md) · [24b](24b-tenant-crd.md)
- [10a-ii-iter-log.md](10a-ii-iter-log.md)
- [dev/bootstrap/values/otel-collector.yaml](../../dev/bootstrap/values/otel-collector.yaml)
- [../plans/rubric.md](../plans/rubric.md)
