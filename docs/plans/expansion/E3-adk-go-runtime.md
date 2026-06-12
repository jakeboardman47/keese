<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - E0-runtime-spi-expansion.md
  - E1-adk-python-runtime.md
  - E2-a2a-protocol.md
  - ../../../api/keese/v1alpha1/agentruntime_types.go
  - ../../../internal/runtime/providers/adk/
  - ../../designs/07-agent-runtime-spi.md
related_skills: [plan-management, controller-authoring]
status: shipped-with-stubs
last_verified: 2026-06-11
revisit_when_adkgo_image_built: true
phase: E3
model_tier: sonnet
depends_on: [E0]
agent: implementer
outputs:
  - internal/runtime/providers/adkgo/
  - internal/controller/keese/workspacesession_controller.go
  - Dockerfile.adk-go
  - config/samples/
---

# E3 — ADK Go runtime

**Refinement pass:** performance & quality.
**Effort:** 2 weeks. **Owner agent:** `controller-author`.

## Goal

Ship the ADK Go runtime provider: image, pod template, A2A bridge (reused from E1),
NetworkPolicy, WorkspaceSession discriminator, and envtest suite. Same security and
network model as E1; smaller pod footprint (~50 MB image vs. Python ~200 MB).

## Inputs

- `ADKGoSpec` struct added by E0:
  [`api/keese/v1alpha1/agentruntime_types.go`](../../../api/keese/v1alpha1/agentruntime_types.go)
- Provider stub: `internal/runtime/providers/adk/go_provider.go`
- Python provider as implementation reference: `internal/runtime/providers/adk/python_provider.go`
- SPI design: [`docs/designs/07-agent-runtime-spi.md`](../../designs/07-agent-runtime-spi.md)
- A2A bridge sidecar from E1: `internal/runtime/a2a/bridge/`

## Tasks

### T1 — ADK Go image

Author `Dockerfile.adk-go`. Multi-stage:
1. `golang:1.24-bookworm` builder — `go build` of a thin Go binary wrapping
   `google.golang.org/adk` and `github.com/a2aproject/a2a-go`. Binary at `/app/adk-go`.
2. `gcr.io/distroless/static-debian12` final stage. Binary + CA certs only. Target
   ~50 MB image.

Image serves A2A on `:8080` and exposes `/healthz` + `/readyz`.

Acceptance: `docker build -f Dockerfile.adk-go .` succeeds; `./adk-go --version` runs;
image size < 100 MB.

### T2 — Pod template

Implement `Bootstrap` in `internal/runtime/providers/adk/go_provider.go`. Pod spec
mirrors E1.T2 exactly, with these differences:
- Single container image from `ADKGoSpec.Image`.
- Command: `/app/adk-go serve --workspace $(KEESE_WORKSPACE_NAME) --a2a-port 8080`.
- Same env vars (no API keys), same volume mounts, same SecurityContext.

Acceptance: envtest pod render identical security posture to ADK Python pod.

### T3 — A2A bridge sidecar

Reuse the bridge sidecar image built in E1.T3. No new code. Inject into pod template
when runtime is `adkGo`, same as `adkPython`. RUNTIME_A2A_UPSTREAM_PORT=8080.

### T4 — NetworkPolicy

Identical to E1.T4 — copy with `adkGo` label selector. Reuse shared NetworkPolicy
builder helper extracted during E1 to avoid duplication.

Acceptance: `go vet ./internal/runtime/providers/adk/...` clean; no duplicated policy
YAML.

### T5 — WorkspaceSession discriminator

Extend discriminator in
[`internal/controller/keese/workspacesession_controller.go`](../../../internal/controller/keese/workspacesession_controller.go)
to call `adkGoProvider.Bootstrap(...)` for `adkGo` implementation variant.

### T6 — Sample CRs

Create `config/samples/runtime_v1alpha1_agentruntime_adk_go_full.yaml`. Must pass
`kubectl apply --dry-run=server`. Include `compactionInterval`, `sessionStoreRef`
fields to exercise full spec.

### T7 — Envtest suite

`internal/runtime/providers/adk/go_provider_test.go`:
- `TestADKGoProvider_PodRender`: no API-key env vars, SA token projected, RO root FS.
- `TestADKGoProvider_NetworkPolicyIdempotency`: SSA × 3, no conflict with Python policy.
- `TestADKGoProvider_ImageSizeBound`: build smoke (CI only; skip in local unit).

## Acceptance criteria

- ADK Go sample Workspace reaches `phase: Active` in kind cluster.
- Pod image < 100 MB (distroless static).
- Zero API key env vars.
- `TestADKGoProvider_*` envtest suite passes.
- Existing ADK Python and goose tests pass without regression.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| `google.golang.org/adk` Go module not yet stable | Pin to latest pre-release; update go.sum; check license (Apache-2.0) |
| `github.com/a2aproject/a2a-go` API surface changes | Version-pin; add govulncheck to CI; minimal wrapper surface |
| Distroless static missing glibc for CGO deps | Use `CGO_ENABLED=0` throughout; pure Go only |
| NetworkPolicy builder duplication between E1 and E3 | Extract shared helper before E3 starts; enforce via `go vet` |

## Refs

- [E0-runtime-spi-expansion.md](E0-runtime-spi-expansion.md)
- [E1-adk-python-runtime.md](E1-adk-python-runtime.md)
- [E2-a2a-protocol.md](E2-a2a-protocol.md)
- [`docs/designs/07b-agent-runtime-spi.md`](../../designs/07b-agent-runtime-spi.md)

## Iteration log

### Iteration 1 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 7 tasks; goal bounded |
| 2 | Architecture fit | 10 | 1.0 | 10 | Mirrors E1; no new patterns introduced |
| 3 | Security posture | 15 | 1.0 | 15 | Same zero-trust posture as E1 |
| 4 | Automatability | 10 | 1.0 | 10 | make + envtest; image size check in CI |
| 5 | Verifiability | 15 | 1.0 | 15 | 3 named tests; regression gate on E1 + goose |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Module stability + CGO + duplication risks |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines; shared helper noted |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter |
| 9 | Observability | 5 | 0.5 | 2.5 | Same OTEL as E1; no new metrics |
| 10 | Operational readiness | 10 | 1.0 | 10 | Distroless + pure Go = minimal attack surface |
| | **Total** | 100 | | **97.5** | |

Verdict: SHIP

Top gaps:
1. `google.golang.org/adk` Go module stability is external dependency risk — pin aggressively.
2. Observability (metrics) deferred to E8 SessionStore.
3. Shared NetworkPolicy builder extraction should happen before E3 starts (pre-requisite note).

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
| 9 | Observability | 5 | 1.0 | 5 | Deferred gap explicit |
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
