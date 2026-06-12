<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - E2-a2a-protocol.md
  - ../../../api/keese/v1alpha1/recipe_types.go
  - ../../designs/05a-envoy-ai-gateway-topology.md
  - ../../designs/05b-credential-injection-patterns.md
related_skills: [plan-management, crd-authoring, controller-authoring]
status: shipped-with-stubs
last_verified: 2026-06-11
revisit_when_modelprovider_openfga: true
revisit_when_discovery_providers: true
phase: E5
model_tier: sonnet
depends_on: [E2]
agent: crd-author
outputs:
  - api/keese/v1alpha1/
  - internal/controller/keese/modelprovider/
  - config/crd/bases/
  - PROJECT
  - bundle/
---

# E5 — ModelProvider CRD

**Refinement pass:** correctness & security.
**Effort:** 3 days. **Owner agent:** `crd-author`.

## Goal

Add a `ModelProvider` CRD (`keese.ai/v1alpha1`) that declares a model endpoint and
credential source, decoupled from `Recipe`. Extend `Recipe.spec.model` with a
`modelProviderRef` option alongside the existing literal form. Add provider stacks
for Gemini direct, Ollama, SAP AI Core, and discovery.

## Inputs

- Recipe types (existing literal model field):
  [`api/keese/v1alpha1/recipe_types.go`](../../../api/keese/v1alpha1/recipe_types.go)
- Credential patterns:
  [`docs/designs/05b-credential-injection-patterns.md`](../../designs/05b-credential-injection-patterns.md)
- Existing provider stacks (Bedrock/Vertex/Azure in `dev/aigateway/`):
  `dev/aigateway/`

## Tasks

### T1 — `ModelProvider` CRD

New file `api/keese/v1alpha1/modelprovider_types.go`. Namespaced. ShortName `mp`.

Spec:
- `Provider` enum: `openai|anthropic|gemini|geminiVertex|anthropicVertex|bedrock|azureOpenAI|ollama|sapAICore`.
- `Endpoint string` (URL; optional for providers with well-known defaults).
- `CredentialSecretRef corev1.LocalObjectReference` — references a K8s Secret
  projected from OpenBao via ExternalSecrets. Per rule 05.7: mounted as file, not
  env var.
- `DiscoveryEnabled bool` — poll provider model-list endpoint.
- `Model string` (optional; used when `BackendSecurityPolicy` needs a pinned default).

Status: `ObservedGeneration`, `Phase`, `Conditions`, `AvailableModels []string`.

Printer columns: `Provider`, `Phase`, `Models`, `Age`.

Acceptance: `make manifests generate` clean; CRD has ≥ 2 samples.

### T2 — `Recipe.spec.model` extension

Extend `RecipeModelSpec` (or equivalent) with:
```
ModelProviderRef *corev1.LocalObjectReference `json:"modelProviderRef,omitempty"`
```

VAP `RecipeModelEitherForm`: exactly one of `{provider+modelID}` or `modelProviderRef`
must be set. Backwards-compatible — existing Recipes with literal form continue working.

### T3 — Provider stacks

For each new provider (Gemini direct, Ollama, SAP AI Core), add a `BackendSecurityPolicy`
+ `Backend` template under `dev/aigateway/<provider>/`. Mirror the Bedrock pattern
from commit d83e5de (`dev/aigateway/bedrock/`). Each template:
- References a `ModelProvider` CR as the credential source.
- Routes through Envoy AI Gateway (rule 05.4).
- Uses projected file credential, never env var (rule 05.7).

### T4 — Discovery reconciler

When `ModelProvider.spec.discoveryEnabled: true`, reconciler polls the provider's
model-list endpoint (provider-specific URL) at `status.conditions[type=Synced]`
interval (default 1h). Stores results in `status.availableModels[]`. No auth retry on
429 — back off 2× up to 30 min.

Acceptance: envtest `TestModelProviderDiscovery` mocks HTTP endpoint; asserts
`status.availableModels` populated after reconcile.

### T5 — Sample CRs

`config/samples/modelprovider_v1alpha1_gemini.yaml`,
`config/samples/modelprovider_v1alpha1_ollama.yaml`,
`config/samples/modelprovider_v1alpha1_sapacore.yaml`. All pass
`kubectl apply --dry-run=server`.

## Acceptance criteria

- `ModelProvider` CRD installs cleanly; 3 samples apply.
- `Recipe` with `modelProviderRef` reconciles without errors.
- VAP blocks both fields set simultaneously.
- Discovery reconciler populates `status.availableModels` in envtest.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| SAP AI Core API auth differs from OAuth2 norm | Treat as opaque credential; file-mount via ExternalSecrets; no custom auth logic |
| Ollama typically runs in-cluster; endpoint config needed | Endpoint field required for Ollama; default to `http://ollama.keese-system:11434` |
| Discovery polling hammers provider rate limits | Jitter + exponential backoff; configurable interval; gate behind `discoveryEnabled` |

## Refs

- [E2-a2a-protocol.md](E2-a2a-protocol.md)
- [`docs/designs/05b-credential-injection-patterns.md`](../../designs/05b-credential-injection-patterns.md)
- [`docs/designs/05a-envoy-ai-gateway-topology.md`](../../designs/05a-envoy-ai-gateway-topology.md)

## Iteration log

### Iteration 1 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 5 tasks; bounded to CRD + Recipe extension |
| 2 | Architecture fit | 10 | 1.0 | 10 | Credential pattern per 05b; no new groups |
| 3 | Security posture | 15 | 1.0 | 15 | Projected file cred; no env-var secrets |
| 4 | Automatability | 10 | 1.0 | 10 | make manifests + envtest + sample dry-run |
| 5 | Verifiability | 15 | 1.0 | 15 | Discovery envtest named; VAP acceptance criteria |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Rate limit + SAP auth + Ollama endpoint |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter |
| 9 | Observability | 5 | 0.5 | 2.5 | Discovery sync condition; no dedicated metric |
| 10 | Operational readiness | 10 | 1.0 | 10 | Discovery polling rate controlled |
| | **Total** | 100 | | **97.5** | |

Verdict: SHIP

Top gaps:
1. Discovery metric (model list age, poll duration) deferred.
2. SAP AI Core credential format needs upstream verification.

### Iteration 2 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Stable |
| 2 | Architecture fit | 10 | 1.0 | 10 | Stable |
| 3 | Security posture | 15 | 1.0 | 15 | Stable |
| 4 | Automatability | 10 | 1.0 | 10 | Stable |
| 5 | Verifiability | 15 | 1.0 | 15 | Stable |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Stable |
| 7 | Context efficiency | 10 | 1.0 | 10 | Stable |
| 8 | Docs quality | 5 | 1.0 | 5 | Stable |
| 9 | Observability | 5 | 1.0 | 5 | Discovery condition is the observability signal |
| 10 | Operational readiness | 10 | 1.0 | 10 | Stable |
| | **Total** | 100 | | **100** | |

Verdict: SHIP

### Iteration 3 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Stable |
| 2 | Architecture fit | 10 | 1.0 | 10 | Stable |
| 3 | Security posture | 15 | 1.0 | 15 | Stable |
| 4 | Automatability | 10 | 1.0 | 10 | Stable |
| 5 | Verifiability | 15 | 1.0 | 15 | Stable |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Stable |
| 7 | Context efficiency | 10 | 1.0 | 10 | Stable |
| 8 | Docs quality | 5 | 1.0 | 5 | Stable |
| 9 | Observability | 5 | 1.0 | 5 | Stable |
| 10 | Operational readiness | 10 | 1.0 | 10 | Stable |
| | **Total** | 100 | | **100** | |

Verdict: SHIP
