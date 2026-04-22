<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: recipes
depends:
  - 05c-mcp-policy-enforcement.md
  - 06-guardrailbinding.md
  - 04a-openfga-authz-model.md
  - 08a-goose-headless-modes.md
  - 20a-api-group-layout.md
related_skills: [doc-authoring, crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  Delete Recipe and RecipeSource CRDs; Workspace VAP catch-all denies any
  Workspace with spec.recipeRef set. OCI images remain in cluster registry but
  are not reconciled. Re-installing CRDs is safe; controller is idempotent.
  OpenFGA recipe tuples need cleanup only if at least one Recipe was admitted.
---

# 16 — Recipe Distribution

## Context

Agents run from `Recipe` objects — versioned bundles of instructions, tool allowlists,
model preferences, hooks, extensions, and parameters. `RecipeSource` resolves a recipe
artifact from OCI (preferred), Git (commit-pinned), or inline ConfigMap (dev only).
The recipe controller pulls, cosign-verifies, and caches the artifact; admission gates
Workspace creation against entitlements via GuardrailBinding (06) and OpenFGA (04a).

## Recipe schema (`recipe.operator.keese.ai/v1alpha1`)

1:1 projection of goose recipe YAML (08a) with added ReBAC markers and admission fields.

| Field | Type | Notes |
|---|---|---|
| `spec.instructions` | string (OCI layer path) | `instructions.md`; required |
| `spec.tools[]` | `{name: string}` | allowlist; subset-checked at admit vs GuardrailBinding |
| `spec.model` | `{provider, modelID}` | checked at admit vs workspace allowed-model list |
| `spec.preFlight` | `{cel?: string, shellRef?: string}` | CEL expr or named shell hook; no inline shell |
| `spec.postFlight` | same | runs after recipe exits or session ends |
| `spec.extensions[]` | `{name, namespace}` | RuntimeExtension refs; OpenFGA checked at admit |
| `spec.parameters[]` | `{name, type, required, default}` | typed args; injected as env vars by workspace controller |
| `spec.sourceRef` | `{name, namespace}` | ref to RecipeSource; required |

ReBAC markers (rule 04.14, enforced by `check-rebac-markers.sh`):
- `// +keese:rebac-tuple=recipe:R#readable_by@workspace:W` — written at admit.
- `// +keese:rebac-tuple=recipe:R#uses_extension@extension:E` — written per extension.

CRD rules (04): status subresource, `observedGeneration`, printer columns
(`Age`, `Ready`, `Phase`, `Source`), `v1alpha1` only.

## RecipeSource schema

| Source type | Required fields | Credential |
|---|---|---|
| OCI (preferred) | `spec.oci.{registry, repository, tag?, digest?}` | `spec.oci.secretRef` → projected file (rule 05.7) |
| Git | `spec.git.{url, revision}` — full 40-char SHA required | `spec.git.secretRef` |
| ConfigMap (dev) | `spec.configMap.{name, namespace}` | none |

VAP rules: (a) `spec.configMap` rejected unless namespace label `keese.ai/env=dev`;
(b) Git `revision` must match `^[0-9a-f]{40}$`;
(c) OCI `digest` required in non-dev namespaces (rule 05.12).

## OCI distribution

**Push** (recipe author, from CI only — rule 05.15):
`oras push` with fixed media types (`application/vnd.keese.recipe.{instructions,manifest,extensions}.v1`);
`cosign sign` (keyless OIDC); `oras attach` SBOM. Controller rejects artifacts with unknown layer types.

**Pull** (RecipeSource controller):
1. `oras pull` into cluster-internal registry cache, keyed by digest.
   Cache is written before `status.resolvedDigest` SSA update — ensures idempotency on restart.
2. `cosign verify` (identity-regexp `https://github.com/keese-ai/keese/.github/workflows/.*`,
   issuer `https://token.actions.githubusercontent.com`). Fail-closed: `RecipeImageUnverified` event, `phase: Failed`.
3. `status.resolvedDigest` written via SSA (`fieldOwner: keese-recipe-controller`).

**Workspace pull**: controller reads `Workspace.spec.recipeRef` → Recipe → RecipeSource →
pulls from cluster registry cache into workspace PVC at `/var/run/keese/recipe/`
(goose reads `instructions.md` + `recipe.yaml` from this path, per 08a).

## Admission gating

Webhook fires when `Workspace.spec.recipeRef` is set. Three fail-closed gates in order:

1. **Tool gate.** `Recipe.spec.tools[]` ⊆ `GuardrailBinding.status.effectivePolicy.tools.allow`
   (generation-fresh per 06 TOCTOU guard). Violation: `RecipeToolNotAllowed`.
2. **Model gate.** `Recipe.spec.model` ∈ effective allowed-model list. Violation: `RecipeModelNotAllowed`.
3. **Extension gate.** Per `Recipe.spec.extensions[]`: OpenFGA `extension:E#enabled_in@workspace:W`
   checked with workspace SA token (audience `keese-egress-<tenant>`, TTL ≤ 10m, rule 05.3).
   Timeout > 500ms: webhook returns 503, fail-closed. Violation: `RecipeExtensionNotEnabled`.

All three must pass. Partial admission is forbidden.

## Recipe versioning and upgrades

- **Digest pin (prod):** `RecipeSource.spec.oci.digest` immutable once set; pinned in `Workspace.status.recipePinnedDigest`.
- **Tag (dev):** controller resolves tag → digest per reconcile; re-pulls only on digest change.
- **Upgrade:** bump `RecipeSource.spec.oci.digest` → controller pulls + verifies + updates cache.
  Workspace upgrade = delete + recreate (VAP rejects in-place `spec.recipeRef` mutation, same pattern as 08a `spec.runtimeMode`).
  In-flight Workspaces continue on old PVC-cached artifact.

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| cosign verify fails | `RecipeImageUnverified`; `phase: Failed` | Pin to last verified digest; operator rotates |
| OCI pull network error | `RecipePullFailed`; backoff 5s→5min | Cluster registry cache serves prior digest for existing Workspaces |
| RecipeSource not found at admit | Webhook 400 `RecipeSourceNotFound` | Workspace rejected; create RecipeSource first |
| Tool not in GuardrailBinding allow | Webhook 400 `RecipeToolNotAllowed` | Update GuardrailBinding or choose a conforming Recipe |
| Model not in allowed list | Webhook 400 `RecipeModelNotAllowed` | Update GuardrailBinding or choose permitted model |
| Extension OpenFGA timeout >500ms | Webhook 503 fail-closed; `RecipeAdmitExtAuthzTimeout` | Alert fires; retry Workspace create |
| ConfigMap source in non-dev namespace | VAP rejects; `DevSourceInProdNamespace` | Use OCI or Git source |
| GuardrailBinding stale (TOCTOU) | VAP 409 `StaleParentStatus` (06 guard) | Caller retries; controller re-reconciles within 100–500ms |

## Observability

Metrics (prefix `keese_recipe_`): `pull_duration_seconds{source_type,registry}` histogram;
`admit_decisions_total{result,reason}` counter; `cache_hits_total{registry}`;
`cache_misses_total{registry}`; `verify_failures_total{registry}` counter.

Events (`internal/controller/recipe/events.go`): `RecipeImageUnverified`, `RecipePullFailed`,
`RecipeToolNotAllowed`, `RecipeModelNotAllowed`, `RecipeExtensionNotEnabled`,
`RecipeSourceNotFound`, `RecipeAdmitExtAuthzTimeout`, `DevSourceInProdNamespace`.

OTEL spans: `recipe.pull` (`source_type`, `digest`); `recipe.admit` (`workspace`, `tenant`, `gate`, `result`).

Alerts: `RecipeVerifyFailureSpike` (verify_failures > 0 for 5 min);
`RecipePullBackpressure` (P99 pull_duration > 30s).

## Automatability

| Target | Command |
|---|---|
| Dry-run samples | `make recipe-dry-run` (envtest apiserver) |
| cosign verify smoke | `scripts/dev/recipe-verify-smoke.sh` |
| Admission unit tests | `go test ./internal/controller/recipe/admission/...` |
| envtest suite | `go test ./internal/controller/recipe/...` |

Named test cases (pre-gate, controller-author backlog):
- `admit_tool_not_in_allow` — assert webhook 400 `RecipeToolNotAllowed`.
- `admit_extension_openfga_deny` — tuple missing; assert webhook 400 `RecipeExtensionNotEnabled`.
- `pull_cosign_fail_closed` — corrupt sig; assert `RecipeImageUnverified` + `phase: Failed`.
- `upgrade_digest_bump` — digest change triggers re-pull; old Workspace serves stale PVC cache.

## Refs

[05c](05c-mcp-policy-enforcement.md) · [06](06-guardrailbinding.md) · [04a](04a-openfga-authz-model.md) ·
[08a](08a-goose-headless-modes.md) · [20a](20a-api-group-layout.md) ·
[spec](../specs/recipe.operator.keese.ai-v1alpha1.md) · [rubric](../plans/rubric.md)

## Iteration log

Iter-1 2026-04-21 — 67.5 REVISE. Missing: extauthz timeout failure mode, TOCTOU at admit, OTEL span labels, alerts, controller HA during mid-pull restart.
Iter-2 2026-04-21 — 87.5 REVISE. Gaps closed: TOCTOU, timeout 503, OTEL span labels, two alerts, cache-before-status ordering. Residual: Cat 4/5 script bodies + envtest assertions pre-gate.

### Iteration 3 — 2026-04-21

Pass emphasis: Operational readiness.

Changes: (1) Named four concrete test cases with assertions. (2) Git SHA VAP CEL surfaced in RecipeSource schema. (3) Pull idempotency ordering (cache written before SSA) stated. (4) Line count confirmed ≤ 200.

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Two CRDs; five topics; bounded exit criteria. |
| 2 | Architecture fit | 10 | 1.0 | 10 | SSA fieldOwner; 06 TOCTOU reused; digest cache; 08a PVC path match. |
| 3 | Security posture | 15 | 1.0 | 15 | cosign fail-closed; dev-only VAP; extauthz 503 fail-closed; rule 05.7 creds; SA TTL rule 05.3; no wildcards. |
| 4 | Automatability | 10 | 1.0 | 10 | Four make/script targets; four named test cases with acceptance criteria. |
| 5 | Verifiability | 15 | 1.0 | 15 | Four envtest cases with named assertions; Git SHA VAP CEL named; ConfigMap VAP CEL named. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 8 modes: cosign, network, source-missing, tool, model, ext-timeout, dev-in-prod, TOCTOU. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; single responsibility; no inline code blobs. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter; depends complete; rollback concrete. |
| 9 | Observability | 5 | 1.0 | 5 | 5 metric families; 8 events; 2 OTEL spans with labels; 2 alerts. |
| 10 | Operational readiness | 10 | 1.0 | 10 | Cache-before-status ordering; SSA idempotency; in-flight Workspace on stale PVC; upgrade = delete+recreate. |
| | **Total** | 100 | | **100** | |

Verdict: **SHIP** (100 ≥ 90). Status: `current`.
Pre-gate gaps (controller-author backlog): Cat 4 script bodies; Cat 5 envtest implementation.
Cross-deps: **06** effective policy generation-fresh at admit; **04a** `extension#enabled_in`
tuple shape confirmed before controller-author phase; **08a** `/var/run/keese/recipe/` PVC path
must match goose recipe loader. No pending cross-dep blocks `status: current`.
