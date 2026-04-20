<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Kubernetes conventions (always loaded)

Non-negotiables for any code under `api/`, `internal/controller/`, `config/`,
`bundle/`, or anything that produces Kubernetes manifests. Violating rules
here blocks merge; exceptions require an ADR in `docs/designs/`.

## API surface

1. **Group domain.** Every API group is `<domain>.operator.keese.ai`. Current
   domains: `workspace`, `workflow`, `runtime`, `memory`, `recipe`,
   `guardrail`, `observability`, `transport`. No new top-level group without
   an ADR referenced in `docs/designs/20-api-group-layout.md`.
2. **Versioning.** All new kinds land as `v1alpha1`. Promotion to `v1beta1`
   requires a conversion webhook and a `docs/plans/migration-<kind>.md`
   migration plan scored ≥ 90.
3. **Status subresource** is mandatory:
   `// +kubebuilder:subresource:status`.
4. **`observedGeneration`** on every status. Status never inputs into the
   next reconcile decision of the same controller — break spec/status
   coupling.
5. **Printer columns** required on every CRD: `Age`, `Ready` (from
   conditions), `Phase`, plus at least one domain-specific column.
6. **Discriminated one-of** (via `oneOf` + `XValidation`) preferred over
   flat enums when extending per-provider/per-type configs — see
   `Memory.spec.provider` and `Transport.spec.type` in
   `docs/designs/15-memory-management.md` and
   `docs/designs/09-transport-crd.md`.

## Reconciler imports

7. Controllers import only `sigs.k8s.io/controller-runtime`,
   `k8s.io/api`, `k8s.io/apimachinery`, and
   `github.com/keese-ai/keese/api/...`. No `client-go` direct use. All
   writes via **Server-Side Apply** (`client.Apply`) with
   `client.FieldOwner("keese-<kind>-controller")`.
8. **No `panic`, `log.Fatal`, or `os.Exit`** under `internal/controller/`.
   Return `(ctrl.Result{}, err)` and let the manager decide.

## RBAC + finalizers + events

9. RBAC markers on every reconciler; `make manifests` must not drift.
   Never `resources: ["*"]` or `verbs: ["*"]` without an ADR reference in
   the marker comment.
10. **Finalizers** required on any reconciler that allocates external
    resources. ID format:
    `finalizers.<kind>.operator.keese.ai/<purpose>`.
11. **Events** use `recorder.Eventf` with `reason` from a finite const
    table in `internal/controller/<group>/<kind>/events.go`. No free-text
    reasons.

## Webhooks

12. **VAP first (CEL, K8s 1.30 GA), webhook second.** Prefer
    `ValidatingAdmissionPolicy` for static invariants; use admission
    webhooks only where CEL is insufficient (cross-resource lookups,
    dynamic external checks). Webhooks do only validation/defaulting —
    no business logic, no side effects.
13. **No conversion webhooks at v1alpha1.** Added at the first
    `v1alpha1 → v1beta1` promotion (rule 2).

## ReBAC markers

14. Every CRD field that affects authorization carries a
    `// +keese:rebac-tuple=<relation>` marker naming the OpenFGA tuple
    the reconciler writes. Absence blocks merge — enforced by
    `scripts/check-rebac-markers.sh` (P3 pre-commit). Tuples are
    documented in `docs/specs/egress-authz-protocol.md`.

## Samples + testing

15. **Samples under `config/samples/**`** must pass `kubectl
    apply --dry-run=server` against an envtest-backed API server. Every
    CRD ships ≥ 2 samples (minimal + fully populated). Enforced by
    `scripts/check-crd-validation.sh`.
16. **Envtest-first.** Every reconciler has `suite_test.go` that loads
    CRDs from `config/crd/bases/` and asserts idempotency over ≥ 3
    reconciles with no spec change.

## Network isolation

17. Every workspace namespace gets a **fail-closed default-deny
    NetworkPolicy** (allow only egress to the Envoy AI Gateway service
    on 443). Wildcards forbidden. See
    `.claude/rules/05-security-zero-trust.md` for the zero-trust
    invariants this serves.
