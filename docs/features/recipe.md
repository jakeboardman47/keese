<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: feature
category: feature
depends:
  - ../designs/16-recipe-distribution.md
  - ../designs/06-guardrailbinding.md
  - ../designs/04a-openfga-authz-model.md
  - ../designs/08a-goose-headless-modes.md
implements_specs:
  - ../specs/keese.ai-v1alpha1-recipe.md
implements_plans:
  - demo/D1-controller-wiring.md
  - demo/tech-debt.md
source_refs:
  - api/keese/v1alpha1/recipe_types.go:1-201
  - api/keese/v1alpha1/recipesource_types.go:1-172
  - internal/controller/keese/recipe_controller.go:1-282
  - internal/controller/keese/recipe_admission.go:1-204
  - internal/controller/keese/recipe_oci.go:1-97
  - internal/controller/keese/recipesource_git.go:1-264
  - internal/controller/keese/recipe_events.go:1-41
  - internal/controller/keese/recipe_webhook.go:1-60
related_skills: [controller-authoring]
status: implemented
implemented_in_phase: demo-D1
last_verified: 2026-05-29
---

# Recipes & RecipeSources

## Summary

A `Recipe` is a reproducible, cosign-verified agent task definition that bundles
instructions, model selection, tool allowlist, RuntimeExtension dependencies, typed
parameters, and optional pre/post-flight hooks into a single namespaced Kubernetes
object. A `RecipeSource` resolves the artifact content from one of three backends —
OCI registry (production), Git commit (CI-pinned), or inline ConfigMap (dev only) —
and makes it available to the Recipe controller via a cluster-internal cache. Admission
enforces a three-gate check (tools, model, extensions) against the effective
`GuardrailBinding` policy before a Recipe may be associated with a Workspace.

## Behavior

- **Create a RecipeSource** with exactly one of `spec.oci`, `spec.git`, or
  `spec.configMap` set. The controller resolves the source, writes
  `status.resolvedDigest`, and transitions to phase `Synced`.
  - OCI: pulls via `oras`, then calls `cosign verify` with identity regexp
    `https://github.com/keese-ai/keese/.github/workflows/.*` and OIDC issuer
    `https://token.actions.githubusercontent.com`. Unverified images stay in phase
    `Failed` with event `CosignVerifyFailed`.
  - Git: performs an in-memory clone (no disk writes) using go-git; `spec.git.revision`
    must be a full 40-character SHA (`recipesource_types.go:63`). Tree digest is a
    deterministic SHA-256 over a sorted canonical tar stream (`recipesource_git.go:133`).
  - ConfigMap: rejected by a CRD CEL `XValidation` rule outside namespaces with label
    `keese.ai/env=dev` (event `ConfigMapSourceInNonDev`; no separate VAP).

- **Create a Recipe** referencing the RecipeSource via `spec.sourceRef`. The controller:
  1. Adds finalizer `finalizers.recipe.keese.ai/cache-cleanup`.
  2. Waits for the RecipeSource to reach phase `Synced` and `status.cached=true`.
  3. Adopts `status.resolvedDigest` from the RecipeSource.
  4. Syncs OpenFGA tuples (`readable_by`, `uses_extension`) via `RecipeRebacWriter`.
  5. Advances phase to `Ready` and sets conditions `Ready=True`, `Verified=True`.

- **Admission webhook** (`/validate-keese-ai-v1alpha1-recipe`) runs the three-gate
  check on every `create` and `update`:
  1. **Stale-policy gate** — `GuardrailBinding.status.effectivePolicy.observedGeneration`
     must match `Workspace.status.guardrailGeneration` (TOCTOU guard).
  2. **Tool gate** — every `spec.tools[].name` must appear in
     `effectivePolicy.tools.allow`.
  3. **Model gate** — `spec.model.provider + "/" + spec.model.modelID` must appear in
     the effective model allowlist.
  - **Extension gate** — each `spec.extensions[]` is checked via OpenFGA
    (`extension:E#enabled_in@workspace:W`); times out fail-closed at 500 ms
    (event `RecipeAdmitExtAuthzTimeout`) (`recipe_admission.go:86`).

- On deletion, the finalizer removes OpenFGA tuples before allowing the object to
  be garbage-collected. The Recipe-owned ConfigMap (mounted into session pods as a
  recipe volume) is cleaned up via Kubernetes owner-reference GC.

## Configuration surface

Key fields are defined in `api/keese/v1alpha1/recipe_types.go` and
`api/keese/v1alpha1/recipesource_types.go`; see `docs/specs/keese.ai-v1alpha1-recipe.md`
for the full contract.

| Field | Notes |
|---|---|
| `spec.instructions` | OCI layer path to `instructions.md`; required |
| `spec.model.{provider,modelID}` | Checked at admission against GuardrailBinding |
| `spec.tools[]` | Admission allowlist; subset of GuardrailBinding effective policy |
| `spec.extensions[]` | OpenFGA checked at admission per extension |
| `spec.parameters[]` | Typed (`string`/`int`/`bool`); injected as env vars at runtime |
| `spec.preFlight` / `spec.postFlight` | CEL expression xor registered `shellRef` |
| `spec.sourceRef.{name,namespace}` | Points to a `RecipeSource` in same or named NS |
| `spec.oci.digest` | Required in non-dev namespaces (CRD CEL XValidation; not a VAP) |
| `spec.git.revision` | Must be a 40-char SHA (pattern-validated) |

## Observability

**Status conditions** (all on `Recipe`): `Ready`, `Verified`, `Progressing`
(`recipe_types.go:163-170`).

**Status phases** — `Recipe`: `Pending → Pulling → Verified → Ready | Failed | Terminating`;
`RecipeSource`: `Pending → Synced | Failed`.

**Printer columns** — `Recipe`: `Age`, `Ready`, `Phase`, `Model`, `Source`;
`RecipeSource`: `Age`, `Ready`, `Type`.

**Event reasons** (from `recipe_events.go`):

| Reason | Severity | When |
|---|---|---|
| `RecipePulled` | Normal | RecipeSource resolved; digest adopted |
| `RecipeVerified` | Normal | cosign verify passed |
| `RecipeReady` | Normal | phase transitions to Ready |
| `RecipeCacheCleanup` | Normal | finalizer cleanup on deletion |
| `CosignVerifyFailed` | Warning | cosign verification failed |
| `OCIPullFailed` / `RecipePullFailed` | Warning | OCI pull error |
| `GitCloneFailed` / `GitRefNotFound` | Warning | git source errors |
| `RecipeToolNotAllowed` | Warning | tool gate denied |
| `RecipeModelNotAllowed` | Warning | model gate denied |
| `RecipeExtensionNotEnabled` | Warning | extension gate denied |
| `RecipeAdmitExtAuthzTimeout` | Warning | OpenFGA timeout; fail-closed |
| `ConfigMapSourceInNonDev` | Warning | ConfigMap source outside dev namespace |

`status.rebacTupleCount` records the number of OpenFGA tuples last synced for
debuggability.

## Known limitations

- **ConfigMap-backed `RecipeSource` is rejected outside dev namespaces.** Only
  namespaces with label `keese.ai/env=dev` may use `spec.configMap`. This is
  enforced by a CRD CEL `XValidation` rule (not a separate VAP) and is by design.
- **Recipe requires the `keese.ai/managed=true` label.** The controller predicate
  silently ignores objects without this label (`recipe_controller.go:274`); no event
  or condition is emitted for unlabelled objects.
- OCI digest is required in non-dev namespaces; tag-only references are rejected by
  a CRD CEL `XValidation` rule (`recipesource_types.go:48`; not a separate VAP).
- Git sources perform an in-memory clone — large monorepos may exhaust operator pod
  memory. Shallow-clone depth is not configurable at `v1alpha1`.
- No per-recipe cache eviction policy; artifacts persist until the RecipeSource is
  deleted and its finalizer runs.
- Extension gate timeout is hard-coded at 500 ms with no per-Recipe override.

## Change history

- `demo-D1` — initial Recipe, RecipeSource, three-gate admission, OCI + Git + ConfigMap
  sources, OpenFGA ReBAC tuple sync, and finalizer-based cleanup implemented
  (plan: `docs/plans/demo/D1-controller-wiring.md`).

## References

- Design: `docs/designs/16-recipe-distribution.md`
- Spec: `docs/specs/keese.ai-v1alpha1-recipe.md`
- Plan: `docs/plans/demo/D1-controller-wiring.md`, `docs/plans/demo/tech-debt.md`
- Source: `api/keese/v1alpha1/recipe_types.go`, `api/keese/v1alpha1/recipesource_types.go`,
  `internal/controller/keese/recipe_controller.go`,
  `internal/controller/keese/recipe_admission.go`,
  `internal/controller/keese/recipe_oci.go`,
  `internal/controller/keese/recipesource_git.go`,
  `internal/controller/keese/recipe_events.go`,
  `internal/controller/keese/recipe_webhook.go`
