<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - E1-adk-python-runtime.md
  - E2-a2a-protocol.md
  - ../../../api/keese/v1alpha1/workspace_types.go
  - ../../designs/27-feature-gates-openfeature.md
related_skills: [plan-management, controller-authoring]
status: planned
last_verified: 2026-05-13
phase: E4
model_tier: sonnet
depends_on: [E1]
agent: implementer
outputs:
  - api/keese/v1alpha1/workspace_types.go
  - internal/runtime/providers/adk/
---

# E4 — Context compaction

**Refinement pass:** correctness & security.
**Effort:** 3 days. **Owner agent:** `controller-author`.

## Goal

Wire ADK-native context compaction to a new `Workspace.spec.runtime.compaction` field.
The operator projects the config into the ADK runtime container via env vars. No
operator-side compaction logic — ADK owns the algorithm. Goose does not use this field.
A FeatureGate allows cluster operators to disable compaction globally.

## Inputs

- Workspace spec:
  [`api/keese/v1alpha1/workspace_types.go`](../../../api/keese/v1alpha1/workspace_types.go)
- ADK Python provider (receives env vars):
  `internal/runtime/providers/adk/python_provider.go`
- ADK Go provider: `internal/runtime/providers/adk/go_provider.go`
- FeatureGate design:
  [`docs/designs/27-feature-gates-openfeature.md`](../../designs/27-feature-gates-openfeature.md)

## Tasks

### T1 — Extend Workspace spec

Add `RuntimeCompactionConfig` to `WorkspaceSpec.Runtime` (or add directly to
`WorkspaceSpec` if `Runtime` sub-object does not exist):

```
Compaction *RuntimeCompactionConfig `json:"compaction,omitempty"`
```

`RuntimeCompactionConfig`:
- `Enabled bool` (default false via webhook defaulting or CEL default).
- `Interval *metav1.Duration` — how often ADK compacts (default 0 = ADK default).
- `Strategy CompactionStrategy` — enum `summarize|truncate` (default `summarize`).

CEL VAP `CompactionIntervalPositive`: if `enabled: true`, interval must be > 0 or
absent (let ADK use its default).

Acceptance: `make manifests generate` clean.

### T2 — Env var projection in ADK providers

In `python_provider.go` and `go_provider.go`, when `Workspace.spec.compaction.enabled:
true`, inject:
- `ADK_COMPACTION_ENABLED=true`
- `ADK_COMPACTION_INTERVAL=<interval as seconds>` (if set)
- `ADK_COMPACTION_STRATEGY=<strategy>`

When `enabled: false` or field absent, do not inject (let ADK defaults apply).
No secrets involved; plain env vars are acceptable for config (rule 05.7 applies to
credential material only).

Acceptance: envtest `TestCompactionEnvVarProjection` asserts env vars present/absent
for both enabled and disabled configs.

### T3 — FeatureGate integration

Document in [`docs/designs/27-feature-gates-openfeature.md`](../../designs/27-feature-gates-openfeature.md)
(iteration log entry only — do not re-litigate the design) how the cluster operator
can set `FeatureGate.spec.flags[name=context-compaction].enabled: false` to suppress
`ADK_COMPACTION_ENABLED` injection cluster-wide, regardless of per-Workspace config.

Workspace reconciler checks `featuregate.IsEnabled("context-compaction")` before
injecting env vars. Gate defaults to `true`.

Acceptance: setting gate to false causes `TestCompactionFeatureGateDisables` to
assert no compaction env vars on ADK pod.

### T4 — OTEL span

The ADK runtime is expected (per ADK SDK contract) to emit an OTEL span named
`keese.adk.compaction` with attributes `strategy` and `token_count_before` whenever a
compaction cycle fires. Document this expectation in the acceptance criteria. The
operator does not produce this span — it validates presence in the integration smoke.

Acceptance: kind smoke (`scripts/dev/e2e-smoke.sh`) checks for span name in APM.

## Acceptance criteria

- ADK Workspace with `compaction.enabled: true` and `interval: 5m` emits OTEL span
  `keese.adk.compaction` after 5 minutes of session activity.
- `FeatureGate context-compaction: false` suppresses injection.
- Goose workspaces unaffected (no compaction env vars).
- CEL VAP rejects `enabled: true, interval: -1s`.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| ADK env var names differ from `ADK_COMPACTION_*` in upstream | Verify against `google-adk>=1.28.1` docs before T2; update names |
| FeatureGate CRD not yet fully wired in reconciler | Feature gate check is a thin wrapper; fall back to always-enabled if gate CR absent |
| Compaction causes token count metrics to spike (ops confusion) | Document in runbook: compaction event → token spike is expected |

## Refs

- [E1-adk-python-runtime.md](E1-adk-python-runtime.md)
- [E3-adk-go-runtime.md](E3-adk-go-runtime.md)
- [`docs/designs/27-feature-gates-openfeature.md`](../../designs/27-feature-gates-openfeature.md)
- [`docs/designs/27b-feature-gate-catalog.md`](../../designs/27b-feature-gate-catalog.md)

## Iteration log

### Iteration 1 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 4 tasks; operator projection only, no ADK logic |
| 2 | Architecture fit | 10 | 1.0 | 10 | FeatureGate pattern per D27; no new CRDs |
| 3 | Security posture | 15 | 1.0 | 15 | Config env vars only; no secrets; rule 05.7 noted |
| 4 | Automatability | 10 | 1.0 | 10 | make manifests + envtest + gate test |
| 5 | Verifiability | 15 | 1.0 | 15 | 2 named envtest tests + kind smoke span check |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Env var name drift + gate fallback covered |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines; minimal doc touch |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter |
| 9 | Observability | 5 | 1.0 | 5 | OTEL span acceptance criterion explicit |
| 10 | Operational readiness | 10 | 1.0 | 10 | Gate fallback prevents hard dependency on gate CR |
| | **Total** | 100 | | **100** | |

Verdict: SHIP

Top gaps: none blocking. ADK env var names need upstream verification in T2.

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
| 9 | Observability | 5 | 1.0 | 5 | Stable |
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
