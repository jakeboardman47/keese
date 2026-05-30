<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: feature
category: feature
depends:
  - docs/designs/06-guardrailbinding.md
  - docs/designs/15-memory-management.md
  - docs/designs/07-agent-runtime-spi.md
  - docs/designs/05b-credential-injection-patterns.md
implements_specs: []
implements_plans:
  - docs/plans/demo/tech-debt.md
source_refs:
  - config/vap/break-glass-annotation.yaml:1-57
  - config/vap/embedding-dim-immutable.yaml:1-48
  - config/vap/adk-runtime-image-digest-pinned.yaml:1-68
  - config/vap/regional-sensitive.yaml:1-77
  - config/vap/sqlite-single-consumer.yaml:1-73
  - config/vap/kustomization.yaml:1-12
related_skills: [controller-authoring]
status: implemented
implemented_in_phase: expansion-E0
last_verified: 2026-05-29
---

# ValidatingAdmissionPolicies

## Summary

Five `ValidatingAdmissionPolicy` objects (CEL, Kubernetes 1.30 GA) enforce static
invariants on keese API resources at admission time with `failurePolicy: Fail`. They
implement the "VAP first, webhook second" mandate of rule 04.12 and the zero-trust
invariants of rule 05. Each policy ships with a paired `ValidatingAdmissionPolicyBinding`
with `validationActions: [Deny]` and is composed into the default overlay via
`config/vap/kustomization.yaml`. All five policies are purely CEL-based; no admission
webhook process is involved.

## Behavior

- **break-glass-annotation** (`config/vap/break-glass-annotation.yaml:14-57`): rejects
  CREATE or UPDATE of any `keese.ai/*` or `authz.keese.ai/*` resource that carries an
  annotation matching `keese.ai/unsafe-*` unless the containing namespace has label
  `keese.ai/break-glass=true`. Implements rule 05.13. Break-glass events must be
  recorded by the reconciler with reason `UnsafeAnnotationAllowed`; the VAP itself
  produces a `Forbidden` API error on violation.

- **embedding-dim-immutable** (`config/vap/embedding-dim-immutable.yaml:13-48`):
  rejects UPDATE of `memories` or `sharedmemories` where `spec.embeddingDim` differs
  from the previously stored value. A zero-value guard (`oldObject.spec.embeddingDim == 0`)
  allows initial population. Users must create a new `Memory` resource to change
  embedding dimensions.

- **adk-runtime-image-digest-pinned** (`config/vap/adk-runtime-image-digest-pinned.yaml:13-68`):
  rejects CREATE or UPDATE of `agentruntimes` with an `adkPython` or `adkGo`
  implementation whose `image` field does not contain `@sha256:`, unless the resource
  namespace carries label `keese.ai/environment=dev`. Only fires when the resource has
  an `adkPython` or `adkGo` implementation (guarded by `matchConditions`).

- **regional-sensitive** (`config/vap/regional-sensitive.yaml:25-77`): rejects CREATE
  or UPDATE of `guardrailbindings` with `spec.scope.type=Tenant` when the namespace
  label `keese.ai/region` does not match `keese.ai/cluster-region`. If either label
  is absent the policy permits (conservative bootstrap fallback per design 05b).

- **sqlite-single-consumer** (`config/vap/sqlite-single-consumer.yaml:23-73`): rejects
  CREATE or UPDATE of `memories` where `spec.provider.type=sqlite` AND
  (`spec.provider.redis.replicas > 1` OR `spec.provider.qdrant.replicas > 1`). The
  presence of a `redis` or `qdrant` sub-struct alone is not forbidden — only setting
  its `replicas` field above 1 triggers rejection. Closes the admission-time follow-on
  deferred from TD-P1-09.

## Coverage at a glance

| Policy | API group | Resource(s) | Operations |
|---|---|---|---|
| `break-glass-annotation` | `keese.ai`, `authz.keese.ai` | all (`*`) | CREATE, UPDATE |
| `embedding-dim-immutable` | `keese.ai` | `memories`, `sharedmemories` | UPDATE |
| `adk-runtime-image-digest-pinned` | `keese.ai` | `agentruntimes` (matchCondition: has adkPython or adkGo) | CREATE, UPDATE |
| `regional-sensitive` | `authz.keese.ai` | `guardrailbindings` | CREATE, UPDATE |
| `sqlite-single-consumer` | `keese.ai` | `memories` | CREATE, UPDATE |

## Configuration surface

No operator-level flags or env vars govern these policies; they are cluster-wide
structural invariants applied at install time.

- **Break-glass opt-out**: label a namespace `keese.ai/break-glass=true` (privileged
  out-of-band action; see `break-glass-annotation.yaml:36-38`).
- **Dev image-pin opt-out**: label a namespace `keese.ai/environment=dev` (see
  `adk-runtime-image-digest-pinned.yaml:36-40`).
- **Regional cluster identity**: set label `keese.ai/cluster-region=<region>` on the
  binding namespace (see `regional-sensitive.yaml:52-54`); absent means permit-all.
- All five VAPs carry label `keese.ai/policy-tier: static` for tooling queries.

## Observability

- VAP violations surface immediately as API server admission errors (`Forbidden` or
  `Invalid` reason) in `kubectl` output and client error responses.
- Break-glass admission events must additionally be recorded by the reconciler with
  event reason `UnsafeAnnotationAllowed` (referenced in `break-glass-annotation.yaml:10-11`).
- No dedicated controller metrics; audit logging for `admissionregistration.k8s.io`
  resources captures all policy evaluations through the cluster's standard audit log.

## Known limitations

- These policies complement, but do not replace, admission webhooks. CEL cannot
  perform cross-resource lookups, so invariants requiring data from a second object
  (e.g. reading `kube-system` labels to determine cluster region) require an admission
  webhook or controller-side enforcement instead. The `regional-sensitive` VAP works
  around this constraint by requiring the `keese.ai/cluster-region` label on the
  binding's own namespace rather than on a cluster-scoped object.
- The `adk-runtime-image-digest-pinned` VAP requires an envtest cluster with the
  `ValidatingAdmissionPolicy` feature gate enabled to exercise its CEL in integration
  tests; `kubectl --dry-run=client` does not evaluate CEL.
- TD-P2-08 landed 4 of the 5 VAPs; the fifth (`adk-runtime-image-digest-pinned`) was
  added in E0-T6. Earlier than `keese.ai/environment=dev` namespace labeling, the
  ADK image-pin policy will reject all ADK runtimes in dev overlays without the label.

## Change history

- **TD-P2-08** (2026-05-07): initial set of 4 VAPs shipped in `config/vap/`
  (`break-glass-annotation`, `embedding-dim-immutable`, `regional-sensitive`,
  `sqlite-single-consumer`); kustomize overlay wired into `config/default/`.
- **E0-T6** (expansion-E0): added `adk-runtime-image-digest-pinned` for ADK Python
  and Go runtime image-pin enforcement (see `docs/plans/expansion/E0-runtime-spi-expansion.md §T6`).

## References

- Design: `docs/designs/06-guardrailbinding.md`
- Design: `docs/designs/15-memory-management.md`
- Design: `docs/designs/07-agent-runtime-spi.md`
- Design: `docs/designs/05b-credential-injection-patterns.md`
- Plan: `docs/plans/demo/tech-debt.md` (TD-P2-08, TD-P1-09)
- Plan: `docs/plans/expansion/E0-runtime-spi-expansion.md` (T6)
- Source: `config/vap/`
