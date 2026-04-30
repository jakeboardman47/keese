<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../../internal/controller/workspace/workspacesession_controller.go
  - ../../../internal/controller/memory/backend.go
  - ../../designs/07-agent-runtime-spi.md
  - ../../specs/agent-runtime-spi.md
related_skills: [plan-management, controller-authoring]
status: planned
last_verified: 2026-04-25
---

# D2 — Agent runtime minimum SPI (real goose pod + sqlite memory)

**Refinement pass:** correctness & security.
**Effort:** 5–7 h. **Owner agent:** `controller-author`.

## Goal

Replace two stubs with the smallest real implementation that lets a Workspace
pod (a) run the upstream `ghcr.io/block/goose` binary, (b) reach the Envoy AI
Gateway with a projected SA token, and (c) persist memory to a sqlite file on
a controller-provisioned PVC.

## Inputs (already in repo)

- WorkspaceSession reconciler with the pod-build stub.
  [internal/controller/workspace/workspacesession_controller.go:438](../../../internal/controller/workspace/workspacesession_controller.go#L438)
- BackendProvisioner interface + FakeBackendProvisioner.
  [internal/controller/memory/backend.go](../../../internal/controller/memory/backend.go)
- Workspace controller PVC + SA + NetworkPolicy SSA already correct.
- Sample `AgentRuntime` CR with goose image: [config/samples/runtime_v1alpha1_agentruntime.yaml](../../../config/samples/runtime_v1alpha1_agentruntime.yaml).

## Tasks

### T1 — Replace the WorkspaceSession pod stub

Edit [internal/controller/workspace/workspacesession_controller.go](../../../internal/controller/workspace/workspacesession_controller.go).
Build the pod from the resolved AgentRuntime + Workspace, not from a
hardcoded distroless reference.

Container shape (single container, demo-grade): image from
`AgentRuntime.spec.implementation.goose.image`; command
`/usr/local/bin/goose session --name <session> --resume`; env
`GOOSE_PROVIDER=anthropic`, `GOOSE_MODEL=claude-opus-4-7`,
`ANTHROPIC_BASE_URL=https://envoy-ai-gateway.keese-system.svc:443`,
`KEESE_SESSION_ID` from fieldRef `metadata.name`. Volume mounts: session
PVC at `/var/run/keese/session`; memory PVC at `/var/run/keese/memory`;
projected SA token at `/var/run/keese/tokens`; CA bundle at
`/var/run/keese/ca` (read-only). SecurityContext: `runAsNonRoot: true`,
`readOnlyRootFilesystem: true`, `allowPrivilegeEscalation: false`,
`capabilities.drop: ["ALL"]`.

Volumes:

- `session` → workspace PVC (already provisioned by Workspace controller).
- `memory` → Memory CR's PVC (T2 below).
- `sa-token` → projected SA token, audience `keese-egress-<tenant>`, TTL 600s.
- `ca` → ConfigMap `keese-system/aigateway-ca` (cert-manager-issued in D3).

`ANTHROPIC_API_KEY` is **deliberately absent**. The Envoy AI Gateway
injects upstream credentials via BackendSecurityPolicy (D3); the agent
only carries a projected SA token. This honors rule
05.2 (no upstream API keys in agent pods).

Acceptance: `kubectl get pod -n alpha -l keese.ai/session=my-session`
shows `Status: Running` with image matching the AgentRuntime spec.

### T2 — Real sqlite BackendProvisioner

Add `internal/controller/memory/sqlite_backend.go` implementing
`BackendProvisioner.Provision`: SSA-apply a PVC named `<memory>-memory`
with `accessModes: [ReadWriteOnce]` and `resources.requests.storage`
from `m.Spec.Provider.SQLite.StorageSize`. Use applyconfiguration
builders + `client.Apply` + `FieldOwner("keese-memory-controller")` +
`ForceOwnership`. Return `BackendStatus{ MountPath:
"/var/run/keese/memory", PVCName: pvc.Name }`.

Wire in `cmd/main.go` to replace `FakeBackendProvisioner` for
`spec.provider.type: sqlite`. Keep Fake for tests.

Memory controller passes `BackendStatus.PVCName` back into Workspace
controller via a status reference, so WorkspaceSession's volume `memory`
in T1 resolves to the correct claim.

Acceptance: applying a sqlite Memory CR results in a PVC bound to a 2 Gi
volume; `kubectl exec` into the session pod shows `/var/run/keese/memory`
mounted with rw access.

### T3 — Wire AgentRuntime resolution

WorkspaceSession needs to read `Workspace.spec.runtimeRef` → fetch the
`AgentRuntime` CR → use its `spec.implementation.goose.image`. Add a
`resolveRuntime()` helper. AgentRuntime is cluster-scoped; namespace lookup
is cross-namespace get.

Acceptance: changing the `AgentRuntime.spec.implementation.goose.image`
field and re-reconciling the session deletes the old pod and starts a
new one with the updated image.

### T4 — Projected SA token shape

Add a `projected` volume on the session pod with a single
`serviceAccountToken` source: `audience: keese-egress-<tenancy>`,
`expirationSeconds: 600`, `path: egress`. Tenant name comes from
`Workspace.spec.tenantRef.name`.

This matches design [04b](../../designs/04b-projected-sa-identity.md).
Other audiences (`workflowRun`, `supervisor`) are post-demo work.

Acceptance: pod-side `cat /var/run/keese/tokens/egress` returns a JWT;
`kubectl get serviceaccount -n alpha ksa-<workspace-uid>` exists.

### T5 — Drain hook (best-effort, no SPI)

Add a `preStop` hook on the agent container that runs `goose session
--export /var/run/keese/memory/checkpoint.json` (or the goose CLI's actual
checkpoint flag — verify against the upstream repo). 30s grace.

This is **not** the full Drain/Resume SPI from design 18 — that's tech
debt. It's the minimum that lets a pod restart not lose the in-progress
turn.

Acceptance: `kubectl delete pod` followed by re-reconcile creates a new
pod that finds `checkpoint.json` and resumes mid-conversation.

## Out of scope (→ tech-debt §runtime)

- Real `AgentRuntime.Bootstrap` / `Drain` / `Resume` SPI in
  `internal/runtime/spi/v1alpha1`.
- ACP transport for IDE attach.
- TokenBudget enforcement on the session pod (currently the controller
  reconciles TokenBudget but doesn't gate sessions on it).
- Capsule namespace projection.
- Non-sqlite Memory backends.

## Verification

- New unit test: `sqlite_backend_test.go` with envtest — applies Memory CR,
  asserts PVC reaches Bound (use the kind cluster's default StorageClass).
- New integration test: `workspacesession_pod_test.go` — applies Workspace
  + AgentRuntime + WorkspaceSession, asserts pod with the right image,
  volumes, and projected token. Use `fake.NewClientBuilder()` for unit;
  envtest for integration.
- Existing envtest suite must still pass.

## Failure modes

| Failure | Symptom | Recovery |
|---|---|---|
| goose image pulls but binary path is wrong | pod CrashLoopBackoff with `exec format error` or `not found` | `kubectl exec` → confirm `which goose`; adjust `command:` |
| Projected token TTL too short for session | gateway returns 401 mid-session | Bump TTL to 1800s for demo; flag as tech debt |
| sqlite WAL mode + RWO PVC + pod restart | DB locked errors | Document single-pod-per-Memory invariant; tech debt entry for read-replicas |
| AgentRuntime not found | session reconcile loops | Set status Degraded with reason `AgentRuntimeNotFound` |

## Iteration log

### Iteration 1 — 2026-04-25

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Five concrete tasks, each with acceptance |
| 2 | Architecture fit | 10 | 1.0 | 10 | Honors rules 04.7 (SSA), 05.2 (no API keys in pod), 05.7 (projected secrets) |
| 3 | Security posture | 15 | 1.0 | 15 | No upstream creds in pod; SA token TTL 600s; readOnlyRootFilesystem |
| 4 | Automatability | 10 | 1.0 | 10 | All file edits + `make test-integration` |
| 5 | Verifiability | 15 | 1.0 | 15 | Two new test files named with explicit assertions |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Failure table covers image, token, sqlite, missing runtime |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines; references designs by path |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter + relative links |
| 9 | Observability | 5 | 0.5 | 2.5 | Pod labels for kubectl filtering; no new metrics in this phase |
| 10 | Operational readiness | 10 | 0.5 | 5 | Drain hook is best-effort; full SPI deferred to tech debt |
| | **Total** | 100 | | **92.5** | |

Verdict: SHIP

Top gaps:
1. preStop hook depends on the upstream goose CLI exposing a checkpoint flag — must verify Saturday before committing. If absent, fall back to "best-effort, no checkpoint" and tag tech-debt P1.
2. RWO PVC + sqlite means single-replica session — fine for demo, capacity bottleneck later.
3. No metrics on session start/stop latency — observability deferred.

Next step: T1 + T3 sequential (T1 blocks on T3's runtime resolution helper); T2 parallel; T4 + T5 last.
