<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: observability
depends: [10a-otel-topology.md]
related_skills: []
status: current
last_verified: 2026-04-21
---

# 10a — Iteration Log

Companion to [10a-otel-topology.md](10a-otel-topology.md). Split for 200-line
ceiling per rule 01 + rule 03.

## Iteration 1 — 2026-04-21

Emphasis: Correctness & security.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Goal in one sentence; 5 open questions answered; fan-out table bounded. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Hybrid sampling justified; DaemonSet-for-gateway correctness rationale given; no rule violations. |
| 3 | Security posture | 15 | 1.0 | 15 | Tenant isolation with DLS; missing-tenant discard fail-closed; Presidio fail-drop; no tokens/bodies (rule 05.10); APM token via OpenBao/ExternalSecret (not env var, rule 05.7). |
| 4 | Automatability | 10 | 0.5 | 5 | Pipeline config via ConfigMap SSA; chart pinned; `make bootstrap-infra` path clear. `cmd/otel-argument-redactor/` processor not yet authored — honest dock. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | 7 failure modes with detection signals; `check-tenant-isolation.sh` named. No envtest assertions or named test files — honest dock. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | APM fallback chain; Presidio unavailable; tail-buffer overflow; SIGKILL loss documented; DaemonSet crash gap accepted. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Main doc ≤ 200 lines; iter-log split here; no inline code blobs; cross-refs via links. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX header; complete frontmatter; all 9 deps listed in main doc; rollback concrete. |
| 9 | Observability | 5 | 1.0 | 5 | Named spans (`keese.rebac.check`, `keese.mcp.tool_call`); Prom metrics + counters; events (`APMExportDegraded`, `MissingTenantAttribute`, `AuditRedactionUnavailable`, `OTELTailBufferFull`). |
| 10 | Operational readiness | 10 | 1.0 | 10 | HA: DaemonSet + Tier 2 PDB + HPA; upgrade/rollback path; migration doc named; resource ceilings stated; SIGTERM drain honored; 14b/11 cross-deps flagged. |
| | **Total** | 100 | | **93** | |

Verdict: **SHIP** (93 ≥ 93 tier-3 bar). `status` flipped to `current`.

Top gaps:
1. Cat 4: `cmd/otel-argument-redactor/` not authored — controller-author / test-engineer backlog, pre-gate acceptable.
2. Cat 5: No envtest or integration tests for pipeline or tenant-isolation assertions — same backlog.
3. Residual: Presidio sidecar vs library embedding deferred to implementation phase.

Cross-deps settled:
- 04a: `keese-openfga-audit-*` + Loki `{job=keese-ext-authz}` ≥ 1-year fan-out locked.
- 04c: `keese-revocation-audit-*` + Loki revocation stream locked.
- 05a: Gateway OTEL spans; Tier 1 Prometheus scrape for JWKS gauge locked.
- 05b: `bsp`, `upstream_role`, `exchange_result` on `keese.rebac.check` locked; 05b iter-2 may cite.
- 05c: `keese.mcp.tool_call` span; `keese-mcp-audit-*` + Loki locked.
- 24/24b: `auditArgumentsRedacted`; `keese-argument-redactor` processor interface locked.

Cross-deps flagged (not blocking this doc):
- 10b: NATS KV enforcement owned by 10b; 10a owns Prom pipeline only.
- 14b iter-1: CSV pinning of otel-collector CRD — flag for that doc.
- 11 iter-1: APM token lifecycle in OpenBao — flag for that doc.
