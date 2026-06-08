<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: api
depends: [docs/designs/20a-api-group-layout.md]
related_skills: [crd-authoring, doc-authoring]
status: current
last_verified: 2026-06-08
rollback: See 20a rollback field. No conversion webhooks at v1alpha1 makes CRD
  removal the rollback path; operator must be scaled down first, CRDs deleted,
  then operator re-deployed with prior image. Document in migration plan at
  v1beta1 promotion.
---

# 20b — API Group Layout: Trade-offs, Failure Modes, Rollback, Observability

Continuation of [20a-api-group-layout.md](20a-api-group-layout.md).

## Printer Column Validation

`make manifests generate` runs controller-gen with `crd:maxDescLen=0`.
Controller-gen validates that every `+kubebuilder:printcolumn` JSONPath
expression resolves against the generated OpenAPI schema — a malformed
JSONPath causes `make manifests` to fail before any artifact is written.

A proposed pre-commit hook `scripts/check-printer-columns.sh` (P3, **not yet
implemented**) would perform a second-pass check after `make manifests generate`
completes clean:

1. Runs `kubectl create --validate=strict --dry-run=server` against each CRD's
   two required samples (minimal + fully-populated, per rule 04.15).
2. Parses `kubectl get <kind>` tabular output and asserts no column renders as
   `<unknown>`.
3. Fails the pre-commit hook if any column is absent or unknown.

This hook is intentionally gated on a clean `make manifests generate` step to
avoid false failures from stale CRD artifacts.

## CSV / OLM Multi-Version Handling

At `v1alpha1` there is exactly one served version per CRD: `served: true`,
`storage: true`. The `additionalPrinterColumns` array lives on the version entry
(not the deprecated top-level field). This is the SDK default and must not be
changed.

When `v1beta1` is introduced for a kind:

1. Two version entries: `v1alpha1` (`served: true`, `storage: false`) and
   `v1beta1` (`served: true`, `storage: true`).
2. `additionalPrinterColumns` must be set on **both** version entries.
   Inconsistency across versions is treated as a bug.
3. The OLM CSV `spec.customresourcedefinitions.owned[].version` must match
   the storage version. `specDescriptors` and `statusDescriptors` must be
   re-verified after each schema change.
4. `operator-sdk bundle validate --select-optional suite=operatorframework`
   must pass before any CSV is published.

**OLM upgrade note:** Both `v1alpha1` and `v1beta1` must appear in the CSV
`owned[]` list if both are served — otherwise OLM considers the CRD unowned.

## Trade-offs

| Option | Chosen? | Rationale |
|---|---|---|
| All kinds in one group | No | Single-group RBAC is all-or-nothing for tenants; the 3 groups (`keese.ai` / `authz.keese.ai` / `policy.keese.ai`) let access-control and policy kinds carry RBAC bindings distinct from core workload kinds. |
| Per-kind top-level groups (`workspace.keese.ai`, etc.) | No | Too broad for future expansion without collision; concerns are separated by the 3 fixed groups instead. |
| A shared `api/core` types package | No | Would force every group package to import `core`. Sharing is instead minimal — `LocalObjectReference` + `ConcurrencyPolicy` in `api/keese/v1alpha1`; each kind declares its own status fields. |
| Promote groups to v1beta1 on first stable release | No | Conversion webhooks before 90-day customer soak add premature complexity; conservative gate prevents thrash. |
| New kind triggers new version | No | API versions attach to kinds in K8s; adding a kind to an existing version is safe and preferred (D23). |
| Phase as bare string (no enum marker) | No | Admission cannot reject unknown phase values; each kind declares a typed `<Kind>Phase` with a `+kubebuilder:validation:Enum` marker. |
| A single shared `Phase` enum for all kinds | No | Lifecycles differ per kind (`WorkspacePhase` has `Idle`/`Evicted`; `TokenBudgetPhase` does not); per-kind enums avoid a lowest-common-denominator vocabulary. |

## Failure Modes

| Failure | Detection | Mitigation |
|---|---|---|
| Cross-group import coupling | `go build` / review | Group packages are self-contained and import no other `api/<g>/v1alpha1`; cross-group coordination stays in the controller layer. |
| Phase enum marker drifts from its consts | Owning-spec review + envtest | A kind's `+kubebuilder:validation:Enum` marker and its `<Kind>Phase` consts live in the same `_types.go`; the proposed `check-phase-enum-drift.sh` hook was not built. |
| Printer columns missing or `<unknown>` | `scripts/check-printer-columns.sh` (P3) | Hook runs dry-run server-validate after `make manifests generate`. |
| Premature v1beta1 promotion (no conversion webhook) | `scripts/check-design-gate.sh` checks migration plan | Gate blocks merge if `docs/plans/migration-<group>.md` absent or score < 90. |
| New kind bypasses CRD checklist | Design-gate CI check | `check-design-gate.sh` confirms owning design doc is `status: current`. |
| OLM CSV `owned[]` missing a served version | `bundle validate` + e2e OLM install test | `e2e.yaml` installs bundle on kind cluster and asserts all 20 CRDs served. |

## Upgrade / Rollback

**v1alpha1 rollback** (no conversion webhooks): scale operator to 0, delete
CRDs (cascades to CRs — acceptable at v1alpha1; document in release notes),
re-deploy prior operator image, re-apply prior CRDs. No data migration needed
at v1alpha1 because storage version has not changed.

**v1alpha1 → v1beta1 rollback**: defined in `docs/plans/migration-<group>.md`.
Must include: conversion webhook removal steps, storage version downgrade
(requires `kubectl storage-version-migrator` or manual re-apply), OLM
`replaces` chain patching. Architect sign-off required.

**Shared-helper rollback**: revert the `api/keese/v1alpha1/common_types.go`
change; `make manifests generate` must be re-run and verified clean before
merging.

## Observability

The API group layout itself does not emit OTEL traces or metrics. The controllers
that own each group do. This design establishes naming conventions:

- OTEL service name: `keese-operator` (single binary; group encoded in span attrs).
- Span attribute `k8s.crd.group` = full API group (e.g. `keese.ai`).
- Span attribute `k8s.crd.kind` = kind name.
- Prometheus metric label `crd_group` = full API group (3 values; within budget).
- Kubernetes Events reason constants defined per kind in
  `internal/controller/<group>/<kind>/events.go` (rule 04.11).

## Refs

- [20a-api-group-layout.md](20a-api-group-layout.md) — groups, kinds, shared
  types, versioning, RBAC summary, encoding
- [../plans/scaffolding-plan.md](../plans/scaffolding-plan.md) — D2, D16, D23
- [../designs/01-tenancy-capsule.md](../designs/01-tenancy-capsule.md) — RBAC scaffold
- [../references/crd-design-checklist.md](../references/crd-design-checklist.md)
- [../plans/rubric.md](../plans/rubric.md)
- [../../PROJECT](../../PROJECT)

## Iteration Log

### Iteration 1 — 2026-04-20

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 5 open questions answered; 9 groups + 14 kinds enumerated (D26 update); import path explicit. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Consistent with D2, D16, D23; no contradiction of D1–D23. |
| 3 | Security posture | 15 | 0.5 | 7.5 | Unidirectional import prevents cross-group type coupling. ReBAC marker canonical home defined. Per-group RBAC boundary stated. Half credit: no explicit per-group ClusterRole mapping. |
| 4 | Automatability | 10 | 1.0 | 10 | `operator-sdk create api` shown; `make manifests generate` required; bundle validate in pre-commit. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Acceptance criteria implicit. Half credit: no envtest test names for shared-type embedding. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 6 failure modes with detection + mitigation. Rollback paths for v1alpha1 and v1beta1 specified. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Split into 20a/20b; each < 200 lines; skill pointers in refs. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX headers; frontmatter complete; rollback field concrete. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL span attributes and Prometheus label cardinality stated. |
| 10 | Operational readiness | 10 | 1.0 | 10 | OLM multi-version handling; upgrade/rollback explicit; 90-day soak gate. |
| | **Total** | 100 | | **85** | |

Verdict: **SHIP** (85 ≥ 85 threshold)

Top gaps:
1. Security (cat 3, −7.5): Per-group RBAC binding example not shown.
2. Verifiability (cat 5, −7.5): Explicit envtest test names for shared-type round-trip not provided.
3. Both gaps are design-adjacent concerns; acceptable for iter 1.

### Iteration 2 — 2026-04-20

Changes from iter 1:
- Added Phase enum Option C (hybrid): per-kind `+kubebuilder:validation:Enum` marker
  on `StatusBase.Phase` field + `check-phase-enum-drift.sh` hook (security, correctness).
- Added printer column validation: `check-printer-columns.sh` hook + controller-gen
  JSONPath validation requirement moved to 20b (automatability, verifiability).
- Revised soak gate: timer starts at first external customer production deployment
  (not GA tag); migration plan must cite Elastic APM trace ID or release-notes entry.
- Added `core` package alias note (`keesecore`) clarifying no collision with `k8s.io/api/core/v1`.
- Added per-group RBAC summary table (pointer to design 01 as authoritative source).
- Added 4 named envtest assertions for shared types (`TestCoreCondition_RoundTrip`,
  `TestCorePhase_EnumValidation`, `TestCoreResourceRef_ValidateCrossGroup`,
  `TestCoreStatusBase_ObservedGenerationMonotonic`).
- Updated `depends:` in 20a to include `docs/designs/01-tenancy-capsule.md`.
- Added phase-enum and printer-column failure modes in 20b failure modes table.
- Added 2 trade-off rows for Phase enum options C vs. alternatives.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | No change; bounds remain clear. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Option C hybrid consistent with controller-gen constraints; D2/D16/D23 preserved. |
| 3 | Security posture | 15 | 1.0 | 15 | Per-group RBAC table added (pointer to design 01 scaffold); enum admission enforcement closes injection surface; import rule unchanged. |
| 4 | Automatability | 10 | 1.0 | 10 | Two new pre-commit hooks enumerated with gating conditions; `make manifests generate` validation confirmed. |
| 5 | Verifiability | 15 | 1.0 | 15 | 4 named envtest assertions cover all 4 shared types; enum rejection and round-trip both specified. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Phase enum drift and printer column failures added; all 8 failure modes have detection + mitigation. |
| 7 | Context efficiency for Claude | 10 | 1.0 | 10 | Both files ≤ 200 lines; printer column validation moved to 20b to preserve limit. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter; `depends:` updated; `last_verified` current. |
| 9 | Observability | 5 | 1.0 | 5 | No change needed; conventions remain correct. |
| 10 | Operational readiness | 10 | 0.5 | 5 | Soak gate now customer-production-anchored. Half credit: hook implementations deferred to gate-open (acceptable; design-level only). |
| | **Total** | 100 | | **95** | |

Verdict: **SHIP** (95 ≥ 92 target)

Top gaps:
1. Operational readiness (cat 10, −5): `check-phase-enum-drift.sh` and
   `check-printer-columns.sh` are enumerated but not implemented (blocked by design
   gate). Full credit deferred to iter 3 or first spec iteration.
2. Per-group ClusterRole definitions in design 01 are iter-1 only; if design 01
   iter-2 introduces changes, this table must be re-synced.
3. `TestCorePhase_EnumValidation` requires VAP/admission webhook to be in place —
   currently stub controllers; test will fail until P8 gate opens.

Next step: Design 01 iter-2 sign-off confirms ClusterRole names. Hooks implemented
in P3. Envtest assertions implemented when design gate opens (P8+).
