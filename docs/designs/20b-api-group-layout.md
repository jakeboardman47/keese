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

Each CRD additionally ships ≥ 2 samples (minimal + fully-populated, rule 04.15)
that pass `kubectl apply --dry-run=server` against an envtest API server,
exercising the printer columns end to end.

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
| Phase enum marker drifts from its consts | Owning-spec review + envtest | A kind's `+kubebuilder:validation:Enum` marker and its `<Kind>Phase` consts live in the same `_types.go`, reviewed together. |
| Printer columns missing or `<unknown>` | `make manifests` + sample `--dry-run=server` | controller-gen rejects malformed JSONPath; each CRD's samples exercise the columns. |
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
