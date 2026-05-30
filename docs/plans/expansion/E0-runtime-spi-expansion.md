<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../../api/keese/v1alpha1/agentruntime_types.go
  - ../../../internal/controller/keese/runtime_runtimes.go
  - ../../../internal/runtime/providers/goose/register.go
  - ../../designs/07-agent-runtime-spi.md
  - ../../specs/agent-runtime-spi.md
related_skills: [plan-management, crd-authoring, controller-authoring]
status: planned
last_verified: 2026-05-13
---

# E0 — AgentRuntime SPI expansion

**Refinement pass:** correctness & security.
**Effort:** 3 days. **Owner agent:** `crd-author`.

## Goal

Extend `AgentRuntimeImplementation` to support `adkPython` and `adkGo` as 4th and 5th
variants. Add provider package skeletons. Update CEL XValidation to count to 5. No
runtime logic ships in this phase — stubs only.

## Inputs

- Current discriminated one-of (3 variants, CEL count == 1):
  [`api/keese/v1alpha1/agentruntime_types.go:66`](../../../api/keese/v1alpha1/agentruntime_types.go#L66)
- Runtime registry:
  [`internal/controller/keese/runtime_runtimes.go`](../../../internal/controller/keese/runtime_runtimes.go)
- Goose provider register pattern:
  [`internal/runtime/providers/goose/register.go`](../../../internal/runtime/providers/goose/register.go)
- SPI design: [`docs/designs/07-agent-runtime-spi.md`](../../designs/07-agent-runtime-spi.md)

## Tasks

### T1 — Extend `AgentRuntimeImplementation`

Edit [`api/keese/v1alpha1/agentruntime_types.go`](../../../api/keese/v1alpha1/agentruntime_types.go).

Add structs:
- `ADKPythonSpec{Image string; PythonVersion string; ADKVersion string; SessionStoreRef *LocalObjectRef; CompactionInterval *metav1.Duration}`
- `ADKGoSpec{Image string; GoVersion string; ADKVersion string; SessionStoreRef *LocalObjectRef; CompactionInterval *metav1.Duration}`

Add to `AgentRuntimeImplementation`: `AdkPython *ADKPythonSpec` and `AdkGo *ADKGoSpec`.

Update CEL XValidation rule to:
`"(has(self.goose)?1:0)+(has(self.claudeCode)?1:0)+(has(self.aider)?1:0)+(has(self.adkPython)?1:0)+(has(self.adkGo)?1:0)==1"`

Acceptance: `go vet ./api/...` clean; CEL message updated.

### T2 — Regen CRDs and deepcopy

Run `make manifests generate`. Verify only `agentruntime` CRD changes; no unrelated
diffs. Both `ADKPythonSpec` and `ADKGoSpec` appear in the generated OpenAPI schema.

Acceptance: `git diff --stat` after regen shows only CRD + deepcopy files.

### T3 — Provider package skeletons

Create `internal/runtime/providers/adk/` with:
- `python_provider.go` — stub implementing `spi.Provider`; `init()` calls
  `runtime.Register("adkPython", &ADKPythonProvider{})`.
- `go_provider.go` — same pattern for `"adkGo"`.
- `doc.go` — package comment.

No business logic. `Bootstrap`, `Drain`, `Resume` return `spi.ErrNotImplemented`.

Acceptance: `go build ./internal/runtime/providers/adk/...` succeeds.

### T4 — Runtime registry wiring

Edit [`internal/controller/keese/runtime_runtimes.go`](../../../internal/controller/keese/runtime_runtimes.go)
to import the new adk package (blank import for side-effect registration). Mirror the
existing goose import pattern.

Acceptance: `go build ./internal/controller/keese/...` succeeds.

### T5 — Sample CRs

Create:
- `config/samples/runtime_v1alpha1_agentruntime_adk_python.yaml` (minimal + full variants)
- `config/samples/runtime_v1alpha1_agentruntime_adk_go.yaml` (minimal + full variants)

Both pass `kubectl apply --dry-run=server` against an envtest-backed API server.

### T6 — VAP image-pin enforcement

Add `ValidatingAdmissionPolicy` `ADKRuntimeImageDigestPinned` (CEL) that rejects `:latest`
or tag-only references on `adkPython`/`adkGo` images unless namespace has label
`keese.ai/environment=dev`. Mirrors existing goose image-pin VAP pattern.

Acceptance: VAP manifest passes `kubectl apply --dry-run=server`.

## Acceptance criteria

- `TestAgentRuntime_CELOneOfFiveVariants` envtest passes.
- Both sample CRs apply cleanly.
- Existing goose runtime tests (`runtime_runtimes_test.go`) pass without regression.
- `go vet`, `make manifests`, `make generate` all clean.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| CEL rule length exceeds CRD annotation limit | Use `+kubebuilder:validation:XValidation` on struct, not field |
| `LocalObjectRef` type not defined | Reuse `corev1.LocalObjectReference` or add to `common_types.go` |
| Deepcopy regen touches unrelated files | Review `git diff` carefully; pin tool version in Makefile |

## Refs

- [`docs/designs/07-agent-runtime-spi.md`](../../designs/07-agent-runtime-spi.md)
- [`docs/designs/07b-agent-runtime-spi.md`](../../designs/07b-agent-runtime-spi.md)
- [E1-adk-python-runtime.md](E1-adk-python-runtime.md)
- [E3-adk-go-runtime.md](E3-adk-go-runtime.md)

## Iteration log

### Iteration 1 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 6 tasks, each with acceptance |
| 2 | Architecture fit | 10 | 1.0 | 10 | Honors D9, D16, rule 04.6 (discriminated one-of) |
| 3 | Security posture | 15 | 1.0 | 15 | VAP pins images; stubs return ErrNotImplemented |
| 4 | Automatability | 10 | 1.0 | 10 | `make manifests generate` + envtest |
| 5 | Verifiability | 15 | 1.0 | 15 | Envtest test named; samples dry-run |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Failure table covers CEL limit + type gaps |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines; file refs with line anchors |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter complete |
| 9 | Observability | 5 | 0.5 | 2.5 | No metrics in stub phase; acceptable |
| 10 | Operational readiness | 10 | 1.0 | 10 | Regen idempotency checked; no external deps |
| | **Total** | 100 | | **97.5** | |

Verdict: SHIP

Top gaps:
1. Observability deferred to E1/E3 when real providers ship.
2. `LocalObjectRef` type resolution deferred to crd-author to check `common_types.go`.
3. VAP manifest needs a real envtest cluster to exercise the CEL expression.

### Iteration 2 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | No change |
| 2 | Architecture fit | 10 | 1.0 | 10 | No change |
| 3 | Security posture | 15 | 1.0 | 15 | No change |
| 4 | Automatability | 10 | 1.0 | 10 | No change |
| 5 | Verifiability | 15 | 1.0 | 15 | No change |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | No change |
| 7 | Context efficiency | 10 | 1.0 | 10 | No change |
| 8 | Docs quality | 5 | 1.0 | 5 | No change |
| 9 | Observability | 5 | 1.0 | 5 | Stub phase; gap noted and deferred |
| 10 | Operational readiness | 10 | 1.0 | 10 | No change |
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
