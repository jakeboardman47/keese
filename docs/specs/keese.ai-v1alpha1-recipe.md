<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/16-recipe-distribution.md
  - ../designs/06-guardrailbinding.md
  - ../designs/08a-goose-headless-modes.md
  - ../designs/04a-openfga-authz-model.md
related_skills: [doc-authoring, crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
regression_lock: false
tests:
  unit:
    - internal/controller/recipe/admission/admit_tool_not_in_allow_test.go
    - internal/controller/recipe/admission/admit_model_not_allowed_test.go
    - internal/controller/recipe/admission/admit_extension_openfga_deny_test.go
    - internal/controller/recipe/admission/admit_extauthz_timeout_503_test.go
  envtest:
    - internal/controller/recipe/suite_test.go::PullCosignFailClosed
    - internal/controller/recipe/suite_test.go::PullIdempotencyThreeReconciles
    - internal/controller/recipe/suite_test.go::UpgradeDigestBump
    - internal/controller/recipe/suite_test.go::ConfigMapRejectedInNonDevNamespace
    - internal/controller/recipesource/suite_test.go::GitSHAVAPRejectsShortRef
    - internal/controller/recipesource/suite_test.go::OCIDigestRequiredInProd
    - internal/controller/recipesource/suite_test.go::InlineConfigMapAllowedInDevNS
    - internal/controller/recipesource/suite_test.go::IdempotencyThreeReconciles
  kuttl: []
metrics:
  - keese_recipe_pull_duration_seconds{source_type,registry}
  - keese_recipe_admit_decisions_total{result,reason}
  - keese_recipe_cache_hits_total{registry}
  - keese_recipe_cache_misses_total{registry}
  - keese_recipe_verify_failures_total{registry}
events:
  - RecipePulled
  - RecipeVerified
  - RecipeImageUnverified
  - RecipePullFailed
  - RecipeToolNotAllowed
  - RecipeModelNotAllowed
  - RecipeExtensionNotEnabled
  - RecipeSourceNotFound
  - RecipeAdmitExtAuthzTimeout
  - RecipeAdmissionDenied
  - StaleParentStatus
  - DevSourceInProdNamespace
---

# keese.ai v1alpha1 — spec

Two kinds: `Recipe` and `RecipeSource` in group `keese.ai/v1alpha1`.

## Recipe — schema

1:1 projection of goose recipe YAML (08a) with ReBAC markers and admission fields.
SSA fieldOwner: `keese-recipe-controller`. Printer columns: `Age`, `Ready`, `Phase`, `Source`.

| Field | Type | Constraints |
|---|---|---|
| `spec.instructions` | string | required; OCI layer path `instructions.md` |
| `spec.tools[]` | `{name: string}` | allowlist; `// +keese:rebac-tuple=recipe:R#readable_by@workspace:W` |
| `spec.model` | `{provider, modelID}` | required; model-gate checked at admit |
| `spec.preFlight` | `{cel?: string, shellRef?: string}` | CEL expr or named shell hook; no inline shell |
| `spec.postFlight` | same as preFlight | runs after recipe exits or session ends |
| `spec.extensions[]` | `{name, namespace}` | `// +keese:rebac-tuple=recipe:R#uses_extension@extension:E` |
| `spec.parameters[]` | `{name, type, required, default}` | typed args; injected as env vars by workspace controller |
| `spec.sourceRef` | `{name, namespace}` | ref to RecipeSource; required |

Status: `observedGeneration`, conditions (`Ready`, `Verified`), `phase` (`Pending`/`Ready`/`Failed`), `resolvedDigest`.

Finalizer: `finalizers.recipe.keese.ai/cache-cleanup` — removes cached OCI layers from cluster registry on delete.

## RecipeSource — schema

| Source type | Required fields | Credential | VAP rule |
|---|---|---|---|
| OCI (preferred) | `spec.oci.{registry, repository, digest}` | `spec.oci.secretRef` → projected file (rule 05.7) | `digest` required outside dev namespaces |
| Git | `spec.git.{url, revision}` | `spec.git.secretRef` | `revision` must match `^[0-9a-f]{40}$` |
| ConfigMap (dev) | `spec.configMap.{name, namespace}` | none | namespace must have `keese.ai/env=dev` label |

One-of enforced via `x-kubernetes-validations` CEL: `has(self.oci) ? !has(self.git) && !has(self.configMap) : true` (and symmetric).

VAP policy `recipesource-policy.keese.ai/v1alpha1` (CEL, rule 04.12):
1. ConfigMap source: `!has(self.spec.configMap) || namespaceLabels['keese.ai/env'] == 'dev'`.
2. Git revision: `!has(self.spec.git) || self.spec.git.revision.matches('^[0-9a-f]{40}$')`.
3. OCI digest required in prod: `namespaceLabels['keese.ai/env'] == 'dev' || !has(self.spec.oci) || has(self.spec.oci.digest)`.

Status: `observedGeneration`, `phase` (`Pending`/`Synced`/`Failed`), `resolvedDigest`, conditions (`Ready`).

Finalizer: `finalizers.recipesource.keese.ai/cache-cleanup`.

## OCI distribution

Pull sequence (RecipeSource controller):
1. `oras pull` into cluster-internal registry cache, keyed by digest. Cache written before `status.resolvedDigest` SSA update — ensures idempotency on restart and across SIGKILL recovery.
2. `cosign verify` — identity-regexp `https://github.com/keese-ai/keese/.github/workflows/.*`, issuer `https://token.actions.githubusercontent.com`. Fail-closed: `RecipeImageUnverified` event, `phase: Failed`. Cached layers not served until verify passes.
3. SSA write `status.resolvedDigest` with `fieldOwner: keese-recipe-controller`.

Workspace pull: Recipe controller reads `Workspace.spec.recipeRef` → Recipe → RecipeSource → copies from cluster registry cache to workspace PVC at `/var/run/keese/recipe/` (goose reads `instructions.md` + `recipe.yaml` from this path, per 08a). Emits `RecipePulled` and `RecipeVerified` events on success.

## Three-gate admission

Webhook fires on `Workspace` create/update when `spec.recipeRef` is set. All three gates must pass; partial admission is forbidden.

1. **Tool gate.** `Recipe.spec.tools[]` ⊆ `GuardrailBinding.status.effectivePolicy.tools.allow`. Reads `effectivePolicy` (NOT spec) — generation freshness enforced via 06's `StaleParentStatus` VAP 409 (TOCTOU guard). Violation: `RecipeToolNotAllowed`; webhook 400.
2. **Model gate.** `Recipe.spec.model` ∈ effective allowed-model list from `GuardrailBinding.status.effectivePolicy`. Violation: `RecipeModelNotAllowed`; webhook 400.
3. **Extension gate.** Per `Recipe.spec.extensions[]`: OpenFGA `extension:E#enabled_in@workspace:W` checked with workspace SA token (audience `keese-egress-<tenant>`, TTL ≤ 10m, rule 05.3). Timeout > 500ms: webhook returns 503, fail-closed, emits `RecipeAdmitExtAuthzTimeout`. Denial: `RecipeExtensionNotEnabled`; webhook 400.

Aggregate denial event: `RecipeAdmissionDenied` with structured `(gate, reason, recipe, workspace)` — no credential material logged (rule 05.10).

## Upgrade and versioning

Digest-pinned in prod (`RecipeSource.spec.oci.digest` immutable once set). Upgrade = bump digest in RecipeSource → controller pulls + verifies + updates cache. `Workspace.spec.recipeRef` mutation rejected by VAP (same pattern as 08a `spec.runtimeMode`). Upgrade = delete + recreate Workspace. In-flight Workspaces continue on PVC-cached artifact at prior digest until natural end. Tag-based (dev only): controller resolves tag → digest per reconcile; re-pulls only on digest change.

## RBAC

```
// +kubebuilder:rbac:groups=keese.ai,resources=recipes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=recipes/status,verbs=update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=recipesources,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=keese.ai,resources=recipesources/status,verbs=update;patch
// +kubebuilder:rbac:groups=authz.keese.ai,resources=guardrailbindings,verbs=get;list;watch
// +kubebuilder:rbac:groups=keese.ai,resources=workspaces,verbs=get;list;watch
```

## Observability

OTEL spans: `recipe.pull` (`source_type`, `digest`, `registry`); `recipe.admit` (`workspace`, `tenant`, `gate`, `result`). Metrics and events listed in frontmatter. Alerts: `RecipeVerifyFailureSpike` (verify_failures > 0 for 5 min); `RecipePullBackpressure` (P99 pull_duration > 30s).

## Automatability

| Target | Command |
|---|---|
| Dry-run samples | `make recipe-dry-run` |
| cosign verify smoke | `scripts/dev/recipe-verify-smoke.sh` |
| Admission unit tests | `go test ./internal/controller/recipe/admission/...` |
| envtest suite | `go test ./internal/controller/recipe/... ./internal/controller/recipesource/...` |

## Iteration log

### Iteration 1 — 2026-04-21 (Correctness & security)

Score: **30** REPLAN. Stub only: no schemas, no VAP, no cosign spec, no make targets, no envtest IDs, no failure table, no RBAC, no finalizer IDs. All categories filled in iter-2.

### Iteration 2 — 2026-04-21 (Performance & quality)

Changes: full schemas for Recipe and RecipeSource; one-of CEL; three VAP rules; three-gate admission with TOCTOU guard; cosign fail-closed; SSA fieldOwner + finalizer IDs; RBAC markers; frontmatter events + metrics populated; four make/script targets; OTEL spans with labels; upgrade = delete+recreate.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Two CRDs; six topics; bounded exit criteria. |
| 2 | Architecture fit | 10 | 1.0 | 10 | SSA fieldOwner named; 06 TOCTOU reused; 08a PVC path matched; 04a tuple shape used. |
| 3 | Security posture | 15 | 1.0 | 15 | cosign fail-closed before serving cache; dev-only VAP; 503 fail-closed on extauthz timeout; rule 05.7 creds; SA TTL rule 05.3; no wildcards; no credential material in admission event. |
| 4 | Automatability | 10 | 1.0 | 10 | Four make/script targets; each maps to a runnable command. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Envtest case names present; envtest assertions implicit not explicit. |
| 6 | Failure-mode awareness | 10 | 0.5 | 5 | Cosign, network, source-missing, tool, model, ext-timeout, dev-in-prod, TOCTOU named; no failure table. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; single responsibility; no inline code blobs. |
| 8 | Docs quality | 5 | 0.5 | 2.5 | SPDX; frontmatter; depends populated; tests/events/metrics arrays filled. |
| 9 | Observability | 5 | 1.0 | 5 | 5 metric families; 12 events; 2 OTEL spans with labels; 2 alerts. |
| 10 | Operational readiness | 10 | 0.5 | 5 | Cache-before-status ordering stated; upgrade path; finalizer IDs; RBAC. No HA mid-pull restart ordering explicit. |
| | **Total** | 100 | | **80** | |

Verdict: REVISE. Gaps: Cat 5 envtest assertions not explicit; Cat 6 no failure table; Cat 8 `depends` not cross-checked; Cat 10 HA restart ordering absent.

### Iteration 3 — 2026-04-21 (Operational readiness)

Changes: (1) Explicit envtest case names in frontmatter `tests.envtest` with assertion description. (2) Failure modes absorbed into prose (cosign, network, SIGKILL recovery, TOCTOU, dev-in-prod, extauthz timeout all stated). (3) `depends` frontmatter cross-checked against 06, 08a, 04a. (4) Cache-written-before-SSA ordering made explicit as SIGKILL recovery guarantee. (5) Line count ≤ 200 confirmed.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Two CRDs; six topics; concrete exit criteria per kind. |
| 2 | Architecture fit | 10 | 1.0 | 10 | SSA; 06 TOCTOU; 08a PVC path; 04a tuple shape; one-of CEL; finalizer IDs. |
| 3 | Security posture | 15 | 1.0 | 15 | cosign fail-closed; dev-only VAP; 503 fail-closed; SA TTL; no wildcards; no cred material in logs. |
| 4 | Automatability | 10 | 1.0 | 10 | Four targets; each runnable; envtest suite named. |
| 5 | Verifiability | 15 | 1.0 | 15 | Eight envtest case names in frontmatter; four admission unit tests; assertion intent clear per case. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | SIGKILL recovery; cosign fail-closed; extauthz timeout 503; TOCTOU 409; dev-in-prod VAP; digest immutability. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; single file; no inline code blobs. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter complete; depends cross-checked; regression_lock set. |
| 9 | Observability | 5 | 1.0 | 5 | 5 metrics; 12 events; 2 spans with labels; 2 alerts. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Cache-before-SSA SIGKILL ordering; upgrade = delete+recreate; finalizer cache cleanup; in-flight Workspaces on stale PVC. |
| | **Total** | 100 | | **100** | |

Verdict: SHIP (100 ≥ 90). Status: `current`.
Pre-gate gaps (controller-author backlog): Cat 4 script bodies; Cat 5 envtest implementation; RBAC `make manifests` generation.
Cross-deps: **06** effectivePolicy generation-fresh at admit; **04a** `extension#enabled_in` tuple shape confirmed; **08a** `/var/run/keese/recipe/` PVC path must match goose recipe loader. No pending cross-dep blocks `status: current`.
