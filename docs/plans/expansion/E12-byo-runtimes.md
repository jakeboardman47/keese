<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - E1-adk-python-runtime.md
  - E2-a2a-protocol.md
  - ../../../api/keese/v1alpha1/agentruntime_types.go
  - ../../designs/07-agent-runtime-spi.md
related_skills: [plan-management, crd-authoring, controller-authoring]
status: planned
last_verified: 2026-05-13
phase: E12
model_tier: sonnet
depends_on: [E1]
agent: implementer
outputs:
  - api/keese/v1alpha1/agentruntime_types.go
  - internal/runtime/providers/byo/
  - config/samples/
---

# E12 — BYO runtimes

**Refinement pass:** correctness & security.
**Effort:** 1 week. **Owner agent:** `crd-author`.

## Goal

Add a `byo` variant to `AgentRuntimeImplementation` for user-supplied container images.
BYO containers must satisfy a minimal A2A contract. Provide sample images for
LangGraph and CrewAI as reference BYO implementations. This is the long-tail escape
hatch for runtimes keese does not ship natively.

## Inputs

- AgentRuntime types (add `BYOSpec` as 7th variant, or 6th if E11 adds Sandbox as
  6th — coordinate with E11 CEL count):
  [`api/keese/v1alpha1/agentruntime_types.go`](../../../api/keese/v1alpha1/agentruntime_types.go)
- A2A bridge sidecar from E1 (reused):
  `internal/runtime/a2a/bridge/`
- A2A protocol contract from E2: [E2-a2a-protocol.md](E2-a2a-protocol.md)

## Tasks

### T1 — `BYOSpec` struct

Add to `AgentRuntimeImplementation` as a new variant (count: E11 adds Sandbox as 6th,
BYO becomes 7th). Update CEL XValidation count accordingly.

`BYOSpec`:
- `Image string` — OCI reference. Must be digest-pinned outside `dev` namespaces
  (VAP `BYORuntimeImageDigestPinned`).
- `Command []string` — optional override.
- `Args []string` — optional.
- `HealthcheckPath string` (default `/healthz`).
- `ReadinessProbePath string` (default `/readyz`).
- `A2APort *int32` (default 8080) — port the container serves A2A on.

No arbitrary env vars allowed — BYO containers receive the same projected SA token,
same ConfigMap refs, same NetworkPolicy as first-party runtimes. Operators cannot
inject custom env vars via `BYOSpec`; they must bake config into the image or use a
projected ConfigMap. This upholds rule 05.7 and rule 05.2.

### T2 — BYO pod template

`internal/runtime/providers/byo/byo_provider.go`. Bootstrap:
1. Build pod template from `BYOSpec.Image` + `Command` + `Args`.
2. Inject standard keese volumes: session PVC, projected SA token, CA bundle, MCP
   ConfigMap (read-only).
3. Inject A2A bridge sidecar from E1.T3 (forwards A2A inbound on bridge port to
   `BYOSpec.A2APort`).
4. Liveness probe: HTTP GET `BYOSpec.HealthcheckPath:A2APort`.
5. Readiness probe: HTTP GET `BYOSpec.ReadinessProbePath:A2APort`.
6. Same SecurityContext as first-party runtimes (non-root, RO root FS, drop ALL).

Acceptance: envtest `TestBYOProvider_PodRender`: image set; SA token projected; no
custom env vars beyond standard keese set.

### T3 — VAPs

- `BYORuntimeImageDigestPinned`: rejects tag-only image outside `dev` namespaces.
- `BYORuntimePortRange`: `A2APort` must be 1024–65535.
- `BYORuntimeNoArbitraryEnv`: rejects any `env` or `envFrom` stanza on `BYOSpec`
  (env is not a field — prevent future field drift).

### T4 — Sample images

`dev/samples/byo/langgraph/Dockerfile`:
- Python 3.12 + `langgraph>=0.2` + `a2a-sdk>=0.3.23`.
- Entrypoint: `python -m langgraph_a2a_wrapper --a2a-port 8080`.
- Listens on `/healthz` + `/readyz`.
- Serves A2A on 8080.

`dev/samples/byo/crewai/Dockerfile`:
- Python 3.12 + `crewai>=0.28` + `a2a-sdk>=0.3.23`.
- Same structure.

Both are reference images only — not built or published by keese CI. Users build and
push to their own registry, then reference by digest in `BYOSpec.Image`.

### T5 — Documentation

`docs/references/byo-runtime-contract.md` — A2A contract requirements for BYO images:
serve A2A on declared port, expose `/healthz` + `/readyz`, read MCP config from
ConfigMap at `/var/run/keese/mcp-config/config.json`, accept SA token via projected
volume. One-page spec.

### T6 — Envtest suite

- `TestBYOProvider_PodRender`: BYO image rendered; SA token projected; RO root FS.
- `TestBYOProvider_VAP_DigestRequired`: tag-only image rejected in prod namespace.
- `TestBYOProvider_Idempotency`: SSA × 3.

## Acceptance criteria

- `BYOSpec` with a valid digest-pinned image creates a pod satisfying the A2A contract.
- LangGraph + CrewAI sample images build locally.
- VAP rejects non-digest images in prod namespaces.
- Envtest suite passes.
- Existing first-party runtime tests pass without regression.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| BYO image doesn't implement `/healthz` correctly | Probe failure → pod `NotReady`; emit event `BYORuntimeUnhealthy` |
| A2A SDK version mismatch between BYO and keese bridge | Bridge speaks a stable A2A subset; version negotiation at HTTP content-type |
| CEL count grows unwieldy as variants accumulate | Consider switching to `+kubebuilder:validation:MaxProperties=1` on the struct |
| LangGraph/CrewAI license (MIT/Apache) | Both Apache-2.0 compatible; confirm in `dev/samples/byo/*/LICENSE` |

## Refs

- [E1-adk-python-runtime.md](E1-adk-python-runtime.md)
- [E2-a2a-protocol.md](E2-a2a-protocol.md)
- [E11-sandbox-runtime.md](E11-sandbox-runtime.md)
- [`docs/designs/07-agent-runtime-spi.md`](../../designs/07-agent-runtime-spi.md)

## Iteration log

### Iteration 1 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 6 tasks; contract document + VAPs + samples |
| 2 | Architecture fit | 10 | 1.0 | 10 | Same pod shape as first-party; no new patterns |
| 3 | Security posture | 15 | 1.0 | 15 | Digest pin VAP; no custom env; same SA token rails |
| 4 | Automatability | 10 | 1.0 | 10 | make manifests + envtest; sample builds are manual |
| 5 | Verifiability | 15 | 1.0 | 15 | 3 named envtest tests |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Probe failure event; A2A version mismatch; CEL growth |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter; contract doc referenced |
| 9 | Observability | 5 | 1.0 | 5 | `BYORuntimeUnhealthy` event |
| 10 | Operational readiness | 10 | 1.0 | 10 | Sample images are reference-only; no CI build needed |
| | **Total** | 100 | | **100** | |

Verdict: SHIP

Top gaps: none blocking. CEL count growth is a future maintainability concern.

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
