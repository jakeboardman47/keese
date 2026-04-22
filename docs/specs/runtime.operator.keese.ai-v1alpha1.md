<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/07-agent-runtime-spi.md
  - ../designs/07b-agent-runtime-spi.md
  - ../designs/08a-goose-headless-modes.md
  - ../designs/08b-goose-acp-stdio-k8s.md
  - ../designs/08c-goose-subagents-limits.md
  - ../designs/16-recipe-distribution.md
  - ../designs/04a-openfga-authz-model.md
related_skills: [controller-authoring, crd-authoring]
status: current
last_verified: 2026-04-21
regression_lock: false
tests:
  unit: []
  envtest:
    - AgentRuntime_unknown_provider_rejected
    - AgentRuntime_image_version_unsupported_rejected
    - AgentRuntime_reconcile_idempotency_3_passes
    - RuntimeExtension_owner_ref_to_agentruntime
    - RuntimeExtension_enabled_in_tuple_written_on_workspace_create
    - RuntimeExtension_enabled_in_tuple_deleted_on_workspace_teardown
    - RuntimeExtension_reconcile_idempotency_3_passes
  kuttl: []
metrics:
  - keese_runtime_session_starts_total{provider,result}
  - keese_runtime_drain_duration_seconds{provider,result}
  - keese_runtime_crashes_total{provider}
  - keese_extension_tuple_writes_total{extension,result}
events:
  - RuntimeStarted
  - RuntimeStopped
  - ProviderUnknown
  - ImageVersionUnsupported
  - SubAgentCleanupTimeout
  - CredentialExpired
  - ExtensionTupleWritten
  - ExtensionTupleDeleted
  - ExtensionRuntimeRefInvalid
  - ExtensionOpenFGAUnavailable
---

# runtime.operator.keese.ai v1alpha1 — spec

Two kinds in `runtime.operator.keese.ai/v1alpha1`. Go interface: [`agent-runtime-spi.md`](agent-runtime-spi.md).
Iteration log: [`runtime.operator.keese.ai-v1alpha1b-iter-log.md`](runtime.operator.keese.ai-v1alpha1b-iter-log.md).

---

## AgentRuntime

Registers a runtime provider. Cluster-scoped. Workspace controller drives pod lifecycle.

### spec

```yaml
spec:
  implementation:   # discriminated one-of; CEL XValidation enforces exclusivity
    goose:          # present iff provider == "goose"
      image: string           # OCI ref; digest-pinned prod / tag dev (rule 05.12)
      imageTag: string        # informational; admission validates SupportedImageVersions
      migrationPolicy:
        severity: critical|high|medium|low
        maxDeferral: duration # critical hard-capped at 1h (08a)
      sidecars:
        acpBridge:
          image: string       # empty = operator-embedded default digest
    claudeCode: {}  # stub; no sub-fields at v1alpha1
    aider: {}       # stub; no sub-fields at v1alpha1
```

**No ReBAC tuple** — AgentRuntime is cluster-scoped config, not a per-identity object.

**Static registration.** Provider registers via Go `init()` in
`internal/runtime/providers/<provider>/register.go`; `cmd/operator/main.go` blank-imports each built-in (07 iter-2).
Unknown impl → `400 UnknownProvider` + event. Image outside `SupportedImageVersions` → `400 ImageVersionUnsupported` (08a).

**CEL XValidation:** `has(self.goose) ? !has(self.claudeCode) && !has(self.aider) : true`

**Printer columns:** `Age`, `Ready`, `Phase`, `Provider`.

**Markers:** `// +kubebuilder:subresource:status` · `// +kubebuilder:resource:scope=Cluster`.

### status

`observedGeneration`, `phase` (Pending|Ready|Degraded|Incompatible), `provider`,
`capabilities` (mirrored CapabilityMatrix flags), `conditions[Ready]`.

### Controller mechanics

- **SSA:** `client.FieldOwner("keese-agentruntime-controller")`.
- **Finalizer:** `finalizers.agentruntime.operator.keese.ai/drain`.
- **RBAC:** `agentruntimes` get;list;watch;create;update;patch;delete; `/status` get;update;patch.
- **Events** (`internal/controller/runtime/agentruntime/events.go`):
  `RuntimeStarted`, `RuntimeStopped`, `ProviderUnknown`, `ImageVersionUnsupported`,
  `SubAgentCleanupTimeout`, `CredentialExpired`.
- **OTEL spans** (07/07b): `runtime.start`, `runtime.drain`, `runtime.health`.

### Failure modes

| Failure | Mitigation |
|---|---|
| Unknown `spec.implementation` | Admission 400 `UnknownProvider`; event; no pod created |
| Image outside `SupportedImageVersions` | Admission 400 `ImageVersionUnsupported` |
| `Start` error | Return `(Result{}, err)`; controller-runtime backoff retry |
| `CleanupSubAgents` `ErrTransient` | Fall back to batch pod-delete-by-label (07, 08c) |

Upgrade/rollback: patch `spec.implementation.goose.image`; see 07b + 08a for drain→restart and migration deferral.

---

## RuntimeExtension

Bundles N tools for a runtime. Namespaced. Owner-ref to AgentRuntime.

### spec

```yaml
spec:
  # +keese:rebac-tuple=extension:E#owner@tenant:T
  runtimeRef:
    # +keese:rebac-tuple=extension:E#enabled_in@workspace:W
    name: string
    namespace: string
  tools:
    - name: string  # must exist in GuardrailBinding effective policy (16)
  description: string
```

**Printer columns:** `Age`, `Ready`, `Phase`, `Runtime`.

**Markers:** `// +kubebuilder:subresource:status` · `// +kubebuilder:resource:scope=Namespaced`.

### status

`observedGeneration`, `phase` (Pending|Ready|Degraded), `boundWorkspaces` (int32 — count
of active `enabled_in` tuples), `conditions[Ready]`.

### Controller mechanics

- **SSA:** `client.FieldOwner("keese-runtimeextension-controller")`.
- **Finalizer:** `finalizers.runtimeextension.operator.keese.ai/rebac-cleanup` — cleared after all `enabled_in` tuples deleted.
- **RBAC:** `runtimeextensions` get;list;watch;create;update;patch;delete; `/status` get;update;patch.
- **Events** (`internal/controller/runtime/runtimeextension/events.go`):
  `ExtensionTupleWritten`, `ExtensionTupleDeleted`, `ExtensionRuntimeRefInvalid`, `ExtensionOpenFGAUnavailable`.
- **OTEL spans:** `runtime.extension.tuple_write`, `runtime.extension.tuple_delete`.
- **Alert:** `ExtensionOpenFGAUnavailable` > 1 min → P2.

### ReBAC lifecycle

`extension:E#owner@tenant:T` written at creation; deleted at deletion (04a).

`extension:E#enabled_in@workspace:W` written (SSA) when a Recipe referencing this
extension is admitted against a Workspace (16 ext-gate); deleted on Workspace finalizer
completion. Admission Check timeout > 500ms → webhook 503, fail-closed (16).
Rollback: delete RuntimeExtension; finalizer drives tuple cleanup; 04a drain-and-rollout
pattern governs model changes.

### Failure modes

| Failure | Mitigation |
|---|---|
| `runtimeRef` not found | Event `ExtensionRuntimeRefInvalid`; `phase: Degraded`; no tuple written |
| OpenFGA unavailable at tuple write | Retry with backoff; event `ExtensionOpenFGAUnavailable`; alert |
| Tuple write partial failure | Finalizer blocks deletion until write confirmed; idempotent retry |
| Workspace teardown with OpenFGA down | Finalizer retries tuple delete; Workspace deletion blocked until tuple gone |

---

## Acceptance tests

Packages: `test/controller/runtime/{agentruntime,runtimeextension}/`.

| Kind | Test name | Assertion |
|---|---|---|
| AgentRuntime | `AgentRuntime_unknown_provider_rejected` | Admission 400 `UnknownProvider`; event emitted |
| AgentRuntime | `AgentRuntime_image_version_unsupported_rejected` | Admission 400 `ImageVersionUnsupported` |
| AgentRuntime | `AgentRuntime_reconcile_idempotency_3_passes` | Status stable; no extraneous SSA writes |
| RuntimeExtension | `RuntimeExtension_owner_ref_to_agentruntime` | `ownerReferences` → AgentRuntime UID; GC cascade |
| RuntimeExtension | `RuntimeExtension_enabled_in_tuple_written_on_workspace_create` | Tuple present in OpenFGA after admit |
| RuntimeExtension | `RuntimeExtension_enabled_in_tuple_deleted_on_workspace_teardown` | Tuple absent after finalizer; `ExtensionTupleDeleted` |
| RuntimeExtension | `RuntimeExtension_reconcile_idempotency_3_passes` | Status + tuples stable across 3 passes |

## Automatability

`make runtime-dry-run` (CRD dry-run, pre-gate) · `go test ./test/controller/runtime/...` (post-gate).

## Refs

[07](../designs/07-agent-runtime-spi.md) · [07b](../designs/07b-agent-runtime-spi.md) ·
[08a](../designs/08a-goose-headless-modes.md) · [08b](../designs/08b-goose-acp-stdio-k8s.md) ·
[08c](../designs/08c-goose-subagents-limits.md) · [16](../designs/16-recipe-distribution.md) ·
[04a](../designs/04a-openfga-authz-model.md) · [spi-spec](agent-runtime-spi.md) ·
[iter-log](runtime.operator.keese.ai-v1alpha1b-iter-log.md) · [rubric](../plans/rubric.md)
