<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: feature
category: feature
depends:
  - docs/designs/06-guardrailbinding.md
  - docs/designs/06-ii-spec-schema.md
implements_specs:
  - docs/specs/authz.keese.ai-v1alpha1-guardrail.md
implements_plans:
  - docs/plans/demo/tech-debt.md
source_refs:
  - api/authz/v1alpha1/guardrailbinding_types.go:1-315
  - internal/controller/authz/guardrailbinding_controller.go:1-341
  - internal/controller/authz/guardrail_merge.go:1-243
  - internal/controller/authz/guardrail_envoy.go:1-341
  - internal/controller/authz/guardrail_kyverno.go:1-51
  - internal/controller/authz/guardrail_events.go:1-34
related_skills: [controller-authoring]
status: implemented
implemented_in_phase: demo-TD-P2
last_verified: 2026-05-29
---

# GuardrailBinding

## Summary

GuardrailBinding (`authz.keese.ai/v1alpha1`) declares tool allow/deny lists,
token budgets, rate limits, Kyverno policy references, and Envoy SecurityPolicy
configuration at one of three scope tiers: Cluster, Tenant, or Workspace. The
controller walks the scope chain (resolving `spec.inherit` parent references),
applies a strictest-wins merge lattice, publishes the result in
`status.effectivePolicy`, and SSA-projects an Envoy Gateway SecurityPolicy plus
any referenced Kyverno ClusterPolicies. Downstream consumers — Recipe admission,
WorkspaceSession provisioning, and the ext_authz sidecar — read only
`status.effectivePolicy`; they do not re-evaluate raw specs.

## Behavior

- **Label-gated.** The controller's `SetupWithManager` predicate admits only
  objects carrying `keese.ai/managed=true`
  (internal/controller/authz/guardrailbinding_controller.go:332-336). Objects
  without this label are silently ignored.
- **Scope chain.** `spec.inherit` lists parent GuardrailBindings (any namespace).
  Parents are resolved ordered broadest-to-narrowest; the current binding is
  appended last. Resolution failure → `ParentReadable=False` condition +
  `Degraded` phase with a `DefaultBindingReadForbidden` event.
- **Merge lattice** (`guardrail_merge.go:37-107`):
  - `tools.allow` — intersection; a narrower binding may only narrow, never
    widen (returns `MergeError` on violation).
  - `tools.deny` — union; any binding may add denials.
  - `tokenBudget.*` — `min()` per field; `0` means "no limit" and is not treated
    as a bound (`minPositive` at guardrail_merge.go:141-155).
  - `tools.rateLimit.requests` — `min()` with narrower scope winning
    (`sa < workspace < tenant`).
  - `recipeHooks` / `kyverno` — union; all hooks and policy refs compose.
- **EffectivePolicy.** Written to `status.effectivePolicy` along with
  `observedGeneration` (used by the TOCTOU CRD `XValidation` rule to reject stale reads).
- **Envoy SecurityPolicy projection** (`guardrail_envoy.go:88-133`). Per binding
  the controller builds `tool_name in [<allow>] && !(tool_name in [<deny>])`,
  compiles and type-checks it with `cel-go` (must be `bool`), then SSA-applies a
  `gateway.envoyproxy.io/v1alpha1.SecurityPolicy` with deny rules first and allow
  rules second; `defaultAction=Deny` when an allow list is set, `Allow` when the
  list is empty (allow-all). Rules match on the `x-mcp-tool-name` request header.
  CEL compilation failure → `CELCompilationFailed=True` condition + `Degraded`
  phase. Projection is skipped when `r.Envoy == nil` or the effective policy has
  no tool rules.
- **Kyverno ClusterPolicy projection** (`guardrailbinding_controller.go:132-139`).
  Each `spec.kyverno[].policyRef` is SSA-applied with field owner
  `keese-guardrailbinding-controller`. Failures transition to `Degraded` and
  requeue after 5 s.
- **OpenFGA tuples** (`guardrailbinding_controller.go:257-285`). The controller
  syncs `guardrail.inherits` tuples for each `spec.inherit` entry and a
  `guardrail.binds_to_workspace` tuple when `spec.scope.type=Workspace`.
  `status.rebacTupleCount` records the number synced.
- **Finalizer** `finalizers.guardrailbinding.keese.ai/cleanup` ensures Envoy
  SecurityPolicy, Kyverno ClusterPolicy projections, and OpenFGA tuples are
  removed before the object is deleted.
- **Missing cluster-default binding.** The controller warns via a
  `DefaultBindingMissing` event when the binding `keese.ai-default` in
  `keese-system` is absent; this is non-fatal for Workspace- or Tenant-scoped
  bindings.

## Configuration surface

Key spec fields (see api/authz/v1alpha1/guardrailbinding_types.go for full
schema):

| Field | Type | Merge rule |
|---|---|---|
| `spec.scope.type` | `Cluster\|Tenant\|Workspace` | immutable (CRD `XValidation`-enforced) |
| `spec.scope.tenantRef` | `NamespacedRef` | required when `type=Tenant` |
| `spec.scope.workspaceRef` | `NamespacedRef` | required when `type=Workspace` |
| `spec.tools.allow` | `[]string` | intersection |
| `spec.tools.deny` | `[]string` | union |
| `spec.tools.rateLimit` | `RateLimit` | min(requests), narrower scope |
| `spec.tokenBudget` | `TokenBudget` | min() per field |
| `spec.kyverno[].policyRef` | `string` | union |
| `spec.inherit[]` | `[]InheritRef` | resolved ordered broad→narrow |
| `spec.envoy.securityPolicyRef` | `NamespacedRef` | optional target override |
| `spec.recipeHooks[]` | `[]RecipeHook` | union; `serviceRef` required (URL form rejected by CRD `XValidation` CEL rule) |

## Observability

**Status conditions** (types defined at guardrailbinding_types.go:249-253):

- `Ready` — `True` when `status.effectivePolicy` is computed and all projections
  succeed.
- `ParentReadable` — `True` when all `spec.inherit` parents are fetchable.
- `CELCompilationFailed` — `True` when Envoy CEL expression fails to compile
  (guardrail_envoy.go:24).

**Status fields:** `status.phase` (`Ready|Degraded|Pending`),
`status.lastMergeTime`, `status.observedGeneration`, `status.rebacTupleCount`.

**Printer columns:** `Age`, `Ready`, `Scope` (from `keese.ai/binding-scope`
label), `Phase`, `ObservedGen`.

**Event reasons** (guardrail_events.go:9-34):

| Reason | Type | When |
|---|---|---|
| `BindingMerged` | Normal | Merge across scope chain complete |
| `EffectivePolicyComputed` | Normal | `status.effectivePolicy` written |
| `DefaultBindingMissing` | Warning | `keese.ai-default` absent in `keese-system` |
| `MergeConflict` | Warning | Allow-list widening attempt detected |
| `CELCompileError` | Warning | CEL expression parse/type-check failed |
| `KyvernoProjectFailed` | Warning | Kyverno ClusterPolicy SSA patch failed |
| `TupleWriteFailed` | Warning | OpenFGA tuple sync failed |
| `DefaultBindingReadForbidden` | Warning | Parent binding not readable |

## Known limitations

- **Requires `keese.ai/managed=true` label.** Objects without this label are
  silently skipped by the controller predicate. Apply the label or the
  reconciler will never process the binding.
- **No wildcard tool names.** Allow/deny lists require exact MCP tool name
  strings; glob patterns are not supported.
- **Envoy projection skipped when gateway integration is absent.** When the
  controller is started without an `EnvoySecurityPolicyProjector` (`r.Envoy ==
  nil`), SecurityPolicy projection is a no-op; tool policy enforcement is then
  only at the ext_authz layer.
- **Kyverno ClusterPolicy body not generated.** `spec.kyverno[].policyRef` is a
  reference to an externally-managed ClusterPolicy; the controller SSA-applies an
  owner annotation but does not create the policy body.

## Change history

- TD-P2-04 (closed 2026-05-07): replaced Envoy CEL stub with
  `ClientSecurityPolicyProjector`; wired `FakeEnvoyProjector` into envtest suite
  (tests 6+7 added). See docs/plans/demo/tech-debt.md.
- Initial controller wiring in demo phase D1; merge lattice, ReBAC tuple sync,
  and Kyverno projector implemented alongside.

## References

- Design: docs/designs/06-guardrailbinding.md
- Design: docs/designs/06-ii-spec-schema.md
- Spec: docs/specs/authz.keese.ai-v1alpha1-guardrail.md
- Plan: docs/plans/demo/tech-debt.md
- Source: api/authz/v1alpha1/guardrailbinding_types.go
- Source: internal/controller/authz/guardrailbinding_controller.go
- Source: internal/controller/authz/guardrail_merge.go
- Source: internal/controller/authz/guardrail_envoy.go
- Source: internal/controller/authz/guardrail_kyverno.go
- Source: internal/controller/authz/guardrail_events.go
