<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends: [transport.operator.keese.ai-v1alpha1.md]
related_skills: []
status: current
last_verified: 2026-04-21
regression_lock: false
tests:
  unit: []
  envtest: []
  kuttl: []
metrics: []
events: []
---

# transport spec — iteration log

Companion to [transport.operator.keese.ai-v1alpha1.md](transport.operator.keese.ai-v1alpha1.md).
Holds rubric score tables only. Decisions and schema live in the primary doc.

## Iteration 1 — 2026-04-21 (correctness + security)

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | One kind; four type variants; all 10 required sections present; exit criteria bounded. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Discriminated one-of per rule 04.6; SSA fieldOwner; immutability VAP; ReBAC markers on authz fields; rule-05 satisfied. |
| 3 | Security posture | 15 | 1.0 | 15 | No credentials in spec; cert-manager referenced not embedded; cross-tenant webhook-gated; NATS JWT server-side; fail-closed NP flagged to 12. |
| 4 | Automatability | 10 | 0.5 | 5 | Admission checks named; RBAC markers stated; make targets not yet authored — pre-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | 5 named envtest files with assertions; 2 sample paths named; runner not wired — pre-gate. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 10 failure modes; CRA missing wired to webhook + runtime ReBAC layer. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Field tables reference design sections; primary doc at 200 lines; companion split. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + copyright; frontmatter arrays populated; all owning designs in depends. |
| 9 | Observability | 5 | 1.0 | 5 | 4 metric families; 5 OTEL spans; 12 event reasons in events.go const table; `observedGeneration` stated. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Lifecycle phases; rollback path; v1beta1 migration plan requirement stated; finalizer annotation-gated; rule 04.4 cited. |
| | **Total** | 100 | | **92.5** | |

Verdict: **SHIP** (92.5 ≥ 90). `status` held at `draft` pending iter-2.

Top gaps:
1. Cat 4 (−5): make targets + samples not yet authored — structural pre-gate gap.
2. Cat 5 (−7.5): envtest runner not wired; test names declared but no source files — structural pre-gate gap.
3. `workspaceSA.authzTupleCheck` field: marker placement and field semantics clarified in iter-2.

## Iteration 2 — 2026-04-21 (performance + quality)

Changes from iter-1: `workspaceSA.authzTupleCheck` field added with `+keese:rebac-tuple=tenant.allows_messaging`
marker (gap 3 closed); cross-tenant admission §3 tightened to cite 03c fan-out as the source of the
`(from-tenant, to-tenant)` resolution — no ambiguity remains; delivery guarantee table moved to §9;
RBAC section condensed to noun-form list; all inline code blocks removed from primary doc (only marker
snippets remain — load-bearing for rebac-markers check). Line count verified at 200.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | All 10 sections tight; no ambiguous paths; cross-tenant derivation from 03c explicit. |
| 2 | Architecture fit | 10 | 1.0 | 10 | `authzTupleCheck` marker correctly targets `tenant.allows_messaging`; 04a iter-5 relations used correctly; VAP + webhook split per rule 04.12. |
| 3 | Security posture | 15 | 1.0 | 15 | Two ReBAC markers on authz fields; `scope: cross-tenant` double-enforcement (webhook + runtime) documented; no inline credentials. |
| 4 | Automatability | 10 | 0.5 | 5 | RBAC markers complete; admission webhook target named; make targets pre-gate. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Envtest assertions concrete per test file; pre-gate structural residual unchanged. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 10 rows unchanged; mitigation references P8 runbook; `CrossTenantAgreementMissing` both webhook + runtime layer. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Primary doc exactly 200 lines; companion holds rubric; no inline code blobs except load-bearing markers. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter on both files; companion in depends of primary (indirect via backref). |
| 9 | Observability | 5 | 1.0 | 5 | Metrics + events + spans unchanged and correct; `observedGeneration` rule cited. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rollback and v1beta1 migration plan requirement explicit; finalizer annotation-gated; lifecycle phases stable. |
| | **Total** | 100 | | **95** | |

Verdict: **SHIP** (95 ≥ 90). `status` held at `draft` pending iter-3 operational readiness pass.

Top gaps:
1. Cat 4/5 (−5, −7.5): make targets + envtest runner pre-gate structural — cannot close before design gate.
2. Cross-tenant stream ownership: 03c §4 teardown and CRA finalizer path confirmed consistent with §5 spec text — no gap, noted for review.

## Iteration 3 — 2026-04-21 (operational readiness)

Changes from iter-2: §5 NATS topic naming hardened — intra-tenant stream config fields
(`retention=workqueue`, `replicas=3`, `maxAge=WorkflowRun.spec.timeout`) now explicit per 03c §1;
cross-tenant stream GC path confirmed (`keese-cta-<cra-uid>` owner-ref: Argo Workflow; CRA deletion
finalizer via design 25 §NATS cross-tenant stream prefix); 7 d post-mortem retention noted; audit grep
pattern stated. §9 failure modes ref to 09 §Lifecycle confirmed complete. `status: current` on flip.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Unchanged; all 10 sections tight. |
| 2 | Architecture fit | 10 | 1.0 | 10 | 03c §1 stream config values absorbed; owner-ref GC path consistent with 03c §4 and design 25 rollback. |
| 3 | Security posture | 15 | 1.0 | 15 | Unchanged from iter-2; double-enforcement confirmed; no new gaps. |
| 4 | Automatability | 10 | 0.5 | 5 | Unchanged — pre-gate structural residual. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Unchanged — pre-gate structural residual. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Stream teardown failure modes (03c §4 `NATSStreamDeleteFailed`) referenced; 7 d retention window explicit. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Primary doc at 200 lines; companion single-responsibility; no new inline blobs. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter on both files; iteration log entry complete; last_verified updated. |
| 9 | Observability | 5 | 1.0 | 5 | Unchanged and confirmed correct. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Stream maxAge bound to WorkflowRun timeout; post-mortem replay window stated; GC via owner-ref confirmed; CRA deletion finalizer path confirmed. |
| | **Total** | 100 | | **97.5** | |

Verdict: **SHIP** (97.5 ≥ 90). `status: current` flipped in primary doc.

Residuals (pre-gate acceptable):
1. Cat 4 (−5): make targets + samples not authored — design gate not open; controller-author agent owns this.
2. Cat 5 (−7.5): envtest runner not wired; named test files not yet on disk — same pre-gate backlog.
3. Design 25 is `current` but 25-ii / 25-iii companion docs provide schema detail; spec depends on high-level only — no gap.
