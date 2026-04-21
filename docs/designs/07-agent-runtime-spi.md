<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends:
  - 02-workspace-model.md
  - 04b-projected-sa-identity.md
  - 08a-goose-headless-modes.md
  - 20-api-group-layout.md
related_skills: [doc-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
rollback: Remove CleanupSubAgents from SPI and revert AgentRuntime.spec.sidecars;
  providers requiring static registration can be shimmed via thin adapter until migration lands.
---

# 07 — Agent Runtime SPI

**Decision:** The `AgentRuntime` Go interface is statically registered at process-start
via `init()`, gated by a per-provider `CapabilityMatrix`. Goose is the first provider.
Sidecar topology for the ACP bridge is conditional on `Workspace.spec.interactive`.

## Context

Keese needs a pluggable runtime surface so goose, aider, and future agents share one
operator without per-provider controller forks. The SPI defines the Go interface,
capability matrix, and lifecycle the workspace controller drives. Iter-2 replaces
dynamic binary-probe discovery with compile-time `init()` registration, adds
`CleanupSubAgents` for orphan cleanup (08c), and formalises the sidecar contract (08b).

## Interface — `internal/runtime/spi/v1alpha1`

Required (all providers): `Start`, `Stop`, `Status`, `InjectPrompt`.

Optional (gate: `CapabilitySupportsSubAgentCleanup`):
```go
// CleanupSubAgents signals active sub-agents to drain gracefully.
// Called by workspace controller on parent drain before pod-delete-by-label fallback.
// Returns ErrTransient if sub-agents could not drain within budget.
CleanupSubAgents(ctx context.Context, s Session) error
```

Optional: `CredentialRotationExpired` — called when projected SA token nears expiry.
See 04b iter-2 + D28 for authoritative SA identity shape and OIDCProvider CRD.

**SPI versioning:** new required method → major package bump (`spi/v2alpha1`); new
optional method → minor semver bump; apidiff enforced in CI.

## Static capability registration

`internal/runtime/providers/goose/register.go`:
```go
func init() {
    spi.Register("goose", spi.CapabilityMatrix{
        SupportsStreaming: true, SupportsSubAgents: true,
        SupportsSubAgentCleanup: true, SupportsMCP: true,
        SupportsRecipes: true, SupportsInjectPrompt: true,
        ProviderName: "goose", SPIVersion: "1.0.0",
    }, newGooseProvider)
}
```

`cmd/operator/main.go` blank-imports each built-in provider to trigger `init()`:
```go
import _ "github.com/keese-ai/keese/internal/runtime/providers/goose"
```

`internal/runtime/providers/goose/versions.go` declares `SupportedImageVersions`
semver ranges; operator release cadence governs which goose image tags are valid.

No `<binary> capabilities` JSON emission. No runtime pod at admission. No stdout parsing.

## `AgentRuntime` CR (simplified)

```yaml
spec:
  providerRef: goose        # resolved against in-process registry; UnknownProvider if absent
  image: ghcr.io/keese-ai/goose-runtime:1.0.5  # ImageVersionUnsupported if outside ranges
  config: { recipesDir: /var/keese/recipes }
  sidecars:
    acpBridge:
      image: ""             # empty = operator-embedded default
status:
  capabilities: { SupportsStreaming: true, SupportsSubAgentCleanup: true }
  phase: Ready
  observedGeneration: 1
```

## Pod topology — conditional on `Workspace.spec.interactive`

**Interactive** (`spec.interactive: true`): two containers — `goose` (runs
`goose serve --stdio`, exposes ACP on `/var/run/keese/acp/goose.sock`) + `keese-acp-bridge`
(ACP frame multiplexer) sharing an `emptyDir` volume at `/var/run/keese/acp`.

**Non-interactive** (WorkflowRun-driven): single container — `goose` running
`goose run --recipe=<path>`. No bridge, no emptyDir, no shared IPC.

Bridge image is independently versioned. `AgentRuntime.spec.sidecars.acpBridge.image`
allows override. Bridge drains on SIGTERM and exits 0 when goose exits. 08b owns
bridge internals; 07 declares contract only.

## Lifecycle and failure modes

Workspace controller owns pod lifecycle end-to-end. On drain: calls `CleanupSubAgents`
(if `SupportsSubAgentCleanup`), then falls back to pod-delete-by-label within the
120s `terminationGracePeriodSeconds` (rule 06-signal-handling §3). On crash: K8s
restart policy governs; reconciler picks up on next watch event. No `panic` or
`log.Fatal` in controller code (rule 04.8).

| Failure | Recovery |
|---|---|
| Unknown `providerRef` | Admission rejects: `UnknownProvider` |
| Image outside version range | Admission rejects: `ImageVersionUnsupported` |
| `Start` error | Reconciler returns `(Result{}, err)`; retry with backoff |
| `CleanupSubAgents` `ErrTransient` | Fall back to pod-delete-by-label |
| Bridge sidecar crash | Pod restarts; goose state durable on PVC |

## Security

Agent pods carry no kubeconfig, no upstream API keys. Identity = projected SA token
`keese-egress-<tenant>`, TTL ≤ 10m (rule 05.3). Bridge sidecar shares the pod and
same SA token — no new credential surface. `readOnlyRootFilesystem: true` on both
containers; writes to workspace PVC (rule 05.11). NetworkPolicy fail-closed; egress
only to Envoy AI Gateway on 443 (rule 05.4). Images pinned by digest in CSV/production.

## Observability

Events (const table in `internal/controller/runtime/agentruntime/events.go`):
`RuntimeStarted`, `RuntimeStopped`, `SubAgentCleanupTimeout`, `ProviderUnknown`,
`ImageVersionUnsupported`, `CredentialExpired`.

OTEL: span per `Start`/`Stop`/`CleanupSubAgents`; attributes: `provider.name`,
`workspace.name`, `tenant.name`, `session.id`. Printer columns: `Age`, `Ready`,
`Phase`, `Provider`.

## Iteration log

### Iteration 1 — 2026-04-20

| # | Category | Wt | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Interface, matrix, lifecycle, apidiff policy defined. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Aligns D8/D9; SSA fieldOwner; no panic/Fatal. |
| 3 | Security posture | 15 | 1.0 | 15 | No creds in agent pod; SA token; NetworkPolicy fail-closed. |
| 4 | Automatability | 10 | 0.5 | 5 | apidiff gate referenced, not yet CI-wired. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Contract clear; envtest/kuttl pre-gate. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Crash policy, ErrTransient, token expiry covered. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; companion 07b for flow diagrams. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter; cross-links consistent. |
| 9 | Observability | 5 | 1.0 | 5 | Events const table; OTEL spans declared. |
| 10 | Operational readiness | 10 | 0.5 | 5 | Upgrade/rollback sketch present; version ranges declared. |
| | **Total** | 100 | | **92.5** | **SHIP** |

Top gaps: (4) apidiff not CI-wired; (5) no test names pre-gate; (10) migration doc TODO.

### Iteration 2 — 2026-04-21

Changes applied: (1) dynamic probe replaced by `init()` static registration;
(2) `CleanupSubAgents` optional method + `SupportsSubAgentCleanup` cap flag added;
(3) sidecar contract formalised — bridge injected only when `Workspace.spec.interactive`;
(4) `CredentialRotationExpired` references 04b iter-2 + D28 for OIDC SA identity shape.

| # | Category | Wt | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Three mandates integrated; decision header precise. |
| 2 | Architecture fit | 10 | 1.0 | 10 | Static registration simpler; no new rule violations. |
| 3 | Security posture | 15 | 1.0 | 15 | Bridge in same pod = same SA token; no new surface. |
| 4 | Automatability | 10 | 0.5 | 5 | apidiff CI hook still referenced, not yet wired. |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Method sigs precise; SupportedImageVersions testable; envtest pre-gate. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | CleanupSubAgents ErrTransient; bridge crash; all modes tabled. |
| 7 | Context efficiency | 10 | 1.0 | 10 | ≤ 200 lines; companion 07b unchanged. |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX; frontmatter updated; depends includes 04b. |
| 9 | Observability | 5 | 1.0 | 5 | SubAgentCleanupTimeout event added; OTEL span for CleanupSubAgents. |
| 10 | Operational readiness | 10 | 0.5 | 5 | Rollback note added; sidecar versioning stated; upgrade cadence defined. |
| | **Total** | 100 | | **92.5** | **SHIP** |

Top gaps: (4) apidiff not CI-wired — primary blocker to 95+; (5) envtest pre-gate by
design; (10) migration doc for v1beta1 promotion still TODO.

Next step: wire apidiff to `lint.yaml` and author `test/controller/agentruntime_test.go`
stubs — those two close both half-credit categories and should bring iter-3 to ≥ 95.
