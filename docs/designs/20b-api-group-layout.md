<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: api
depends: [docs/designs/20a-api-group-layout.md]
related_skills: [crd-authoring, doc-authoring]
status: current
last_verified: 2026-04-20
rollback: See 20a rollback field. No conversion webhooks at v1alpha1 makes CRD
  removal the rollback path; operator must be scaled down first, CRDs deleted,
  then operator re-deployed with prior image. Document in migration plan at
  v1beta1 promotion.
---

# 20b — API Group Layout: Trade-offs, Failure Modes, Rollback, Observability

Continuation of [20a-api-group-layout.md](20a-api-group-layout.md).

## CSV / OLM Multi-Version Handling

**Decision:** At v1alpha1 there is exactly one served version per CRD, so
`spec.versions[]` has one entry with `served: true`, `storage: true`. The
`additionalPrinterColumns` array lives on the version entry (not the deprecated
top-level field). This is the SDK default and must not be changed.

When `v1beta1` is introduced for a kind:

1. The CRD gets two version entries: `v1alpha1` (`served: true`, `storage: false`)
   and `v1beta1` (`served: true`, `storage: true`).
2. `additionalPrinterColumns` must be duplicated or explicitly set on both
   version entries. Omitting columns on the non-storage version makes `kubectl get`
   output differ per apiserver version — treat inconsistency as a bug.
3. The OLM CSV `spec.customresourcedefinitions.owned[].version` field must match
   the storage version (`v1beta1`). The CSV `specDescriptors` and
   `statusDescriptors` must be re-verified after each schema change.
4. `operator-sdk bundle validate --select-optional suite=operatorframework`
   must pass before any CSV is published.

**OLM upgrade note:** OLM installs only the CSV's declared CRD versions. If the
running operator serves both `v1alpha1` and `v1beta1`, both must appear in the
CSV `owned[]` list — otherwise OLM considers the CRD unowned and will not manage
its lifecycle.

## Trade-offs

| Option | Chosen? | Rationale |
|---|---|---|
| All kinds in one group (`operator.keese.ai`) | No | Single group RBAC is all-or-nothing for tenants; 8 groups allow per-group RBAC bindings aligned to team ownership. |
| Per-kind top-level groups (`workspace.keese.ai`, etc.) | No | D2 locks `*.operator.keese.ai`; top-level is too broad for future expansion without collision. |
| Shared types in each group package | No | Leads to duplication and drift. Unidirectional `api/core/v1alpha1` import enforces consistency. |
| Promote groups to v1beta1 on first stable release | No | Conversion webhooks before 90-day soak add premature complexity; conservative gate prevents thrash. |
| New kind triggers new version | No | API versions attach to kinds in K8s; adding a kind to an existing version is safe and preferred (D23 composition principle). |

## Failure Modes

| Failure | Detection | Mitigation |
|---|---|---|
| Cross-group import cycle | `go build` fails | Enforced by unidirectional import rule; `golangci-lint` `depguard` rule blocks `api/<g>/v1alpha1 → api/<g2>/v1alpha1`. |
| Shared-type drift (different `Phase` values per group) | Code review + `grep` in pre-commit | Single const set in `api/core/v1alpha1`; group packages extend via additional consts. |
| Printer columns missing from CSV version entry | `operator-sdk bundle validate` fails | Pre-commit hook `validate-bundle` runs the validate command on every CSV change. |
| Premature v1beta1 promotion (no conversion webhook) | `scripts/check-design-gate.sh` checks migration plan | Gate blocks merge if `docs/plans/migration-<group>.md` absent or score < 90. |
| New kind bypasses CRD checklist | Design-gate CI check | `check-design-gate.sh` confirms owning design doc is `status: current`. |
| OLM CSV `owned[]` missing a served version | `bundle validate` + e2e OLM install test | `e2e.yaml` workflow installs bundle on kind cluster and asserts all 13 CRDs served. |

## Upgrade / Rollback

**v1alpha1 rollback** (no conversion webhooks): scale operator to 0, delete
CRDs (cascades to CRs — acceptable at v1alpha1; document in release notes),
re-deploy prior operator image, re-apply prior CRDs. No data migration needed
at v1alpha1 because storage version has not changed.

**v1alpha1 → v1beta1 rollback**: defined in `docs/plans/migration-<group>.md`.
Must include: conversion webhook removal steps, storage version downgrade
(requires `kubectl storage-version-migrator` or manual re-apply), OLM
`replaces` chain patching. Architect sign-off required.

**Cross-group type rollback**: revert the `api/core/v1alpha1` commit; bump
`go.mod` accordingly; `make manifests generate` must be re-run and verified
clean before merging.

## Observability

The API group layout itself does not emit OTEL traces or metrics. The controllers
that own each group do. This design establishes naming conventions:

- OTEL service name: `keese-operator` (single binary; group is encoded in span
  attributes, not the service name).
- Span attribute `k8s.crd.group` = full API group (e.g.,
  `workspace.operator.keese.ai`).
- Span attribute `k8s.crd.kind` = kind name.
- Prometheus metric label `crd_group` = full API group (kept short in Prom
  cardinality budget by limiting to 8 values).
- Kubernetes Events reason constants defined per kind in
  `internal/controller/<group>/<kind>/events.go` (rule 04.11).

## Refs

- [20a-api-group-layout.md](20a-api-group-layout.md) — groups, kinds, shared
  types, versioning, OLM, encoding
- [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md) — D2, D16, D23
- [../references/crd-design-checklist.md](../references/crd-design-checklist.md)
- [../plans/rubric.md](../plans/rubric.md)
- [../../PROJECT](../../PROJECT)

## Iteration Log

### Iteration 1 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 5 open questions answered; 8 groups + 13 kinds enumerated; import path explicit. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Consistent with D2 (*.operator.keese.ai), D16 (SSA+VAP), D23 (compose); no contradiction of D1–D23. |
| 3 | Security posture | 15 | 0.5 | 7.5 | Unidirectional import prevents cross-group type coupling. ReBAC marker canonical home defined. Threat model for API-group boundary (per-group RBAC) stated. Half credit: no explicit least-priv RBAC example per group. |
| 4 | Automatability | 10 | 1.0 | 10 | `operator-sdk create api` command shown; `make manifests generate` required; bundle validate in pre-commit. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Acceptance criteria implicit (gate script, bundle validate, dry-run). Half credit: no explicit envtest test name or kuttl fixture named for shared-type embedding. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 6 failure modes with detection + mitigation. Rollback paths for v1alpha1 and v1beta1 both specified. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Split into 20a/20b at natural boundary; each < 200 lines; skill pointers in refs; no inline code beyond command examples. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX headers; frontmatter complete on both files; rollback field concrete; last_verified current. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL span attributes and Prometheus label cardinality stated; event reason convention referenced. |
| 10 | Operational readiness | 10 | 1.0 | 10 | OLM multi-version handling; upgrade/rollback explicit; printer-column consistency requirement; 90-day soak gate. |
| | **Total** | 100 | | **85** | |

Verdict: **SHIP** (85 ≥ 85 threshold)

Top gaps:
1. Security (cat 3, −7.5): Per-group RBAC binding example not shown; rely on
   spec docs to enumerate concrete ClusterRole/Role per group.
2. Verifiability (cat 5, −7.5): Explicit envtest test names for shared-type
   round-trip and `StatusBase` embedding not provided; defer to spec iteration.
3. Both gaps are spec-level concerns, not design-level — acceptable for iter 1.

Next step: Human reviewer confirms verdict. Iteration 2 should add a concrete
per-group RBAC summary row (cat 3) and name at least one envtest assertion per
shared type (cat 5). Target score ≥ 92 on iteration 2.
