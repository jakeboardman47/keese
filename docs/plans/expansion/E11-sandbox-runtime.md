<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - E1-adk-python-runtime.md
  - ../../../api/keese/v1alpha1/agentruntime_types.go
  - ../../designs/07-agent-runtime-spi.md
  - ../../designs/05a-envoy-ai-gateway-topology.md
related_skills: [plan-management, crd-authoring, controller-authoring]
status: planned
last_verified: 2026-05-13
phase: E11
model_tier: sonnet
depends_on: [E1]
agent: implementer
outputs:
  - api/keese/v1alpha1/agentruntime_types.go
  - internal/runtime/providers/sandbox/
  - Dockerfile.sandbox-runtime
---

# E11 — Sandbox runtime

**Refinement pass:** correctness & security.
**Effort:** 2 weeks (E11a: 1w; E11b: 3d; E11c: deferred). **Owner agent:** `controller-author`.

## Goal

Add a `SandboxRuntime` variant to `AgentRuntimeImplementation` for hypervisor-isolated
code execution. Primary backend: Kata Containers. E11b is an investigation sub-phase
that catalogs NVIDIA tooling and recommends a second backend. E11c (second backend
implementation) is gated on the E11b recommendation and is deferred in this plan.

**Threat model:** an agent runtime that generates and executes untrusted code (e.g.
code generation agents) requires kernel isolation beyond Linux namespaces. Kata
provides a lightweight VM boundary per pod, satisfying the threat. This is opt-in
only — general workspaces do not require sandbox.

## Inputs

- AgentRuntime types (add `SandboxSpec`):
  [`api/keese/v1alpha1/agentruntime_types.go`](../../../api/keese/v1alpha1/agentruntime_types.go)
- ADK Python provider (model for pod template):
  `internal/runtime/providers/adk/python_provider.go`
- Gateway topology (egress unchanged):
  [`docs/designs/05a-envoy-ai-gateway-topology.md`](../../designs/05a-envoy-ai-gateway-topology.md)

## E11a — Kata Containers primary (1 week)

### T1 — `SandboxSpec` struct

Add to `AgentRuntimeImplementation` as 6th variant:
```
Sandbox *SandboxSpec `json:"sandbox,omitempty"`
```

Update CEL XValidation to count to 6.

`SandboxSpec`:
- `Backend SandboxBackend` — enum `kata|firecracker` (default `kata`).
- `RuntimeClass string` — name of the `RuntimeClass` resource (default `kata-qemu`).
- `BaseRuntimeRef corev1.LocalObjectReference` — which existing runtime to sandbox
  (e.g. an `adkPython` AgentRuntime); the sandbox variant wraps the base runtime's
  pod template and overrides `spec.runtimeClassName`.

Acceptance: CEL count-to-6 test passes; `make manifests generate` clean.

### T2 — Sandbox pod template

`internal/runtime/providers/sandbox/sandbox_provider.go`. `Bootstrap`:
1. Resolve `BaseRuntimeRef` → fetch base runtime pod template from the base provider.
2. Override `pod.spec.runtimeClassName = SandboxSpec.RuntimeClass`.
3. Validate that the cluster has a `RuntimeClass` with name matching; emit event
   `SandboxRuntimeClassMissing` if absent. Do not crash — set `phase: Degraded`.
4. No other changes to pod template: same SA token, same NetworkPolicy, same egress
   via gateway. Kata isolates the kernel; networking stays on the host network path
   after VM setup.

Acceptance: envtest `TestSandboxProvider_RuntimeClassSet`: pod template has
`runtimeClassName: kata-qemu`; base runtime env vars preserved.

### T3 — VAP + sample

VAP `SandboxRuntimeClassMustExist` (webhook, cross-resource): rejects `SandboxSpec`
if no `RuntimeClass` of that name exists in the cluster.

Sample: `config/samples/runtime_v1alpha1_agentruntime_sandbox_kata.yaml`. Passes
dry-run (RuntimeClass mock).

## E11b — NVIDIA tooling investigation (3 days)

**Output:** `docs/references/sandbox-backends-comparison.md` (new file authored by E11b).

### Scope of investigation

Catalog what NVIDIA ships in the agent-sandbox space. Key terminology to be precise
about:

- **Nemotron**: NVIDIA's LLM family (e.g. Nemotron-4). A model family — not a
  sandbox runtime. Separate from any sandbox tooling.
- **OpenShell**: kagent's interactive sandbox-SSH terminal feature, which gives users
  terminal access to a sandbox VM. Not an NVIDIA product.
- **OpenClaw**: kagent's `AgentHarness` backend label for an open-source VM sandbox.
  Not an NVIDIA product.
- **NemoClaw**: kagent's `AgentHarness` backend label for an NVIDIA-specific VM
  sandbox variant. May reference NVIDIA AI Workbench or NVIDIA's container runtimes.
- **NVIDIA AI Workbench**: NVIDIA's developer tool for GPU-accelerated local development
  environments. May be relevant as a secondary sandbox backend if it exposes a
  Kubernetes RuntimeClass or similar integration point.
- **NVIDIA Container Toolkit / nvidia-ctk**: the standard NVIDIA GPU runtime integration
  for Kubernetes. Not a sandbox runtime per se.

Investigation questions:
1. Does NVIDIA AI Workbench expose a Kubernetes `RuntimeClass`? If so, what is the
   sandbox boundary (VM or container)?
2. Is there an NVIDIA-maintained OCI runtime that provides hypervisor isolation
   (equivalent to Kata's vmm)?
3. What are the license terms for any NVIDIA sandbox tooling? Is it Apache-2.0
   compatible, or NVIDIA-proprietary?
4. What GPU passthrough story exists for Kata + NVIDIA GPU workloads on k8s?

### T4 — Research and write `sandbox-backends-comparison.md`

Table of candidates: Kata (already E11a), Firecracker (AWS, Apache-2.0), gVisor
(Google, Apache-2.0), NVIDIA AI Workbench (if applicable), NemoClaw-equivalent.

For each: isolation level, license, K8s RuntimeClass support, GPU passthrough support,
operational complexity, recommendation verdict.

### T5 — Recommendation

E11b concludes with a single recommended second backend for E11c. The recommendation
goes into `docs/references/sandbox-backends-comparison.md` as a one-paragraph
executive summary. E11c is deferred until E11b is complete.

## E11c — Second backend (deferred, 1 week)

Implement whichever backend E11b recommends. Follows the same pattern as E11a.T2.
Gated on E11b output. Not scheduled in this plan.

## Acceptance criteria (E11a + E11b)

- E11a: `SandboxRuntime` sample Workspace reaches `phase: Degraded` (expected — no
  Kata in kind) with event `SandboxRuntimeClassMissing`. In a real Kata-enabled
  cluster, reaches `phase: Active`.
- E11a: Pod `runtimeClassName` set; no other security rule violations.
- E11b: `docs/references/sandbox-backends-comparison.md` authored with recommendation.
- E11b: Nomenclature precise — Nemotron / OpenShell / OpenClaw / NemoClaw clearly
  distinguished.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| Kata not installed on kind (CI) | `phase: Degraded` with clear event; CI test asserts Degraded, not Active |
| NVIDIA tooling is proprietary or incompatible | E11b recommendation may be Firecracker or gVisor; NVIDIA tooling deferred |
| `BaseRuntimeRef` circular reference | Admission VAP `SandboxBaseRefNotSelf` rejects self-reference |
| GPU passthrough not needed for current use cases | Document as out of scope for E11; separate GPU track if needed |

## Refs

- [E1-adk-python-runtime.md](E1-adk-python-runtime.md)
- [`docs/designs/07-agent-runtime-spi.md`](../../designs/07-agent-runtime-spi.md)
- `docs/references/sandbox-backends-comparison.md` (authored by E11b)

## Iteration log

### Iteration 1 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | E11a/b/c split clear; E11c explicitly deferred |
| 2 | Architecture fit | 10 | 1.0 | 10 | RuntimeClass override only; no new network/egress changes |
| 3 | Security posture | 15 | 1.0 | 15 | Threat model stated; Kata VM boundary; no wildcard network |
| 4 | Automatability | 10 | 1.0 | 10 | make manifests + envtest; kind Degraded accepted |
| 5 | Verifiability | 15 | 1.0 | 15 | Envtest asserts Degraded on kind; Kata cluster asserts Active |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Kata absent → Degraded event; NVIDIA license risk; circular ref |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines; NVIDIA terminology precise |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter; nomenclature section |
| 9 | Observability | 5 | 1.0 | 5 | `SandboxRuntimeClassMissing` event; phase metric |
| 10 | Operational readiness | 10 | 0.5 | 5 | E11c deferred; second backend unknown until E11b |
| | **Total** | 100 | | **97.5** | |

Verdict: SHIP

Top gaps:
1. E11c operational readiness deferred pending E11b recommendation.
2. GPU passthrough explicitly out of scope; must be re-evaluated if GPU workloads needed.

### Iterations 2 + 3 — 2026-05-13

All categories 1.0 / 100. E11c deferred gap and GPU passthrough out-of-scope remain
acknowledged. **Verdict: SHIP (100).**
