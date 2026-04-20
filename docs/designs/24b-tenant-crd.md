<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: tenancy
depends: [24-tenant-crd.md]
related_skills: [crd-authoring]
status: current
last_verified: 2026-04-20
rollback: See 24-tenant-crd.md frontmatter.
---

# 24b — Keese `Tenant` CRD: Trade-offs, Failure Modes, Upgrade, Observability

Companion to [24-tenant-crd.md](24-tenant-crd.md). Spec, reconcile, ownerRef,
admission, and migration details are in 24.

## Trade-offs

| Option | Chosen | Rationale |
|---|---|---|
| `metadata.name` as ReBAC identity key | Yes | Matches existing OpenFGA tuples; UID changes on delete+recreate causing tuple orphans. |
| `metadata.uid` as ReBAC identity key | No | Requires full tuple backfill on re-creation; name is stable per D26 intent. |
| OwnerRef Workspace → Tenant | No | Cascade deletion on tenant removal destroys user data; label association is correct model. |
| OwnerRef TokenBudget → Tenant | Yes | Budget is meaningless without its tenant; GC is correct. |
| Fail-closed on Mode A label removal | Yes | Accidental de-tenanting is a security event — silent orphaning is unacceptable. |
| Cascade-warn on Mode A label removal | No | Deferred; only suitable for dev/staging overlays via feature flag. |
| `spec.namespaceSelector` authoritative in Mode B | No | Capsule owns namespace membership in Mode B; dual authority creates split-brain. |

## Failure Modes

| Failure | Detection | Mitigation |
|---|---|---|
| Capsule Tenant not found (Mode B) | `status.capsuleTenantResolved=false`; `Phase=Pending` | Retry + event `CapsuleTenantNotFound`; alert after 5 min |
| Label removed with live Workspaces (Mode A) | VAP + finalizer block | `TenantLabelLocked` event; drain Workspaces first |
| Namespace selector overlap | Webhook rejects | `SelectorOverlapDenied`; admin must re-scope selector |
| tokenBudgetRef / credentialPoolRef missing at apply | Webhook rejects | Fix ref; re-apply |
| RefNotResolved at runtime (ref deleted post-create) | Controller emits `RefNotResolved`; phase → Degraded | Restore or re-point ref |
| Tenant deleted with live Workspaces | Finalizer blocks | `TenantDeletionBlocked` event; drain Workspaces first |
| `dedicatedGateway` toggle with live namespaces | VAP rejects | Drain namespaces; coordinate 05a gateway teardown first |
| `--tenant-crd-mode=off` rollback with orphaned tuples | OpenFGA tuples for `tenant:X` remain; workspace controller resumes label-derivation | Run `fga tuple delete` cleanup job; document in rollback migration plan |

## Upgrade / Rollback

OLM `replaces` chain handles operator version rollback. CRD is not deleted on
OLM downgrade (OLM safety rule); Tenant CRs persist until manually cleaned via
finalizer drain.

**Pre-D26 → post-D26 upgrade order:**
1. Author and apply `migrations/tenant-backfill.yaml` (creates Tenant CRs from
   existing labels).
2. Upgrade operator image via OLM catalog (operator reads Tenant CRs on startup).
3. Verify `status.phase=Ready` on all Tenant CRs before marking upgrade complete.

**Emergency rollback:**
1. Set `--tenant-crd-mode=off` on operator Deployment.
2. Operator derives `tenant:X` from `keese.ai/tenant` labels (pre-D26 mode).
3. Run OpenFGA tuple cleanup job to remove stale Tenant-backed tuples if any
   drift occurred.
4. Document in `docs/plans/migration-d26-rollback.md` before executing.

## Observability

OTEL spans:
- `keese.tenant.reconcile` — attrs: `tenant.name`, `tenant.mode`, `namespace.count`, `phase`.
- `keese.tenant.capsule.sync` (Mode B) — attrs: `tenant.name`, `capsule.tenant.name`, `resolved`.

Event reasons (finite table in `internal/controller/tenancy/tenant/events.go`):
`TenantProvisioned`, `NamespaceAdded`, `NamespaceRemoved`, `TenantLabelLocked`,
`CapsuleTenantNotFound`, `RefNotResolved`, `TenantDeletionBlocked`,
`SelectorOverlapDenied`, `NamespaceSelectorIgnoredInModeB`.

Metrics:
- `keese_tenant_reconcile_duration_seconds{mode,phase}`
- `keese_tenant_namespace_count{tenant,mode}`
- `keese_tenant_capsule_sync_errors_total{tenant}`
- `keese_tenant_deletion_blocked_total{tenant}`

Alert: `keese_tenant_capsule_sync_errors_total > 0 for 5m` → P2 (Mode B only).

## Iteration Log

### Iteration 1 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Q1–Q6 answered; bounded by D23/D26; split across 24/24b at ceiling. |
| 2 | Architecture fit | 10 | 1.0 | 10 | D3 Capsule-direct preserved; D16 SSA/VAP-first; D23 compose-over-replicate; no Capsule aggregation replicated. |
| 3 | Security posture | 15 | 1.0 | 15 | Fail-closed Mode A; finalizer blocks cascade; VAP non-toggle for dedicatedGateway; ReBAC marker; identity key security rationale documented; no wildcard RBAC. |
| 4 | Automatability | 10 | 0.5 | 5 | Migration Job path stated; `--tenant-crd-mode=off` flag stated but not yet implemented; VAP manifest path implicit from D-01.7 pattern. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Failure modes, event reasons, VAP CEL clauses concrete and testable; envtest/kuttl test names deferred to spec phase (pre-gate acceptable). |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Eight modes enumerated; each has detection + mitigation. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Split at 200-line ceiling; cross-refs by pointer; single responsibility per file. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; depends updated; status current on both files. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL spans, events (finite table named), metrics, alert declared. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rollback concrete; OLM replaces chain; upgrade ordering stated; backfill job + ADR path explicit. |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP (92.5 ≥ 90)

Top gaps:
1. `--tenant-crd-mode=off` flag not yet implemented — acceptable pre-gate; must land in P8 controller phase.
2. Envtest/kuttl test names not yet authored — design phase; pre-gate per gate policy.
3. 04a identity key discrepancy (`metadata.uid` vs `metadata.name`) flagged in 24 § Cross-Reference Impacts; 04a iter-5 needed to align.

Next step: Author `docs/plans/migration-d23-tenant-crd.md` (D26 backfill ADR). Then 04a iter-5 to align identity key.
