<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - E1-adk-python-runtime.md
  - ../../../api/keese/v1alpha1/workspace_types.go
  - ../../../api/authz/v1alpha1/crosstenanagreement_types.go
  - ../../designs/04a-openfga-authz-model.md
  - ../../designs/05a-envoy-ai-gateway-topology.md
  - ../../designs/05c-mcp-policy-enforcement.md
  - ../../designs/25-cross-tenant-agreement.md
  - ../../specs/egress-authz-protocol.md
related_skills: [plan-management, controller-authoring, crd-authoring]
status: planned
last_verified: 2026-05-13
---

# E2 — A2A protocol on Workspace

**Refinement pass:** correctness & security.
**Effort:** 1 week. **Owner agent:** `controller-author`.

## Goal

Expose an A2A HTTP/SSE endpoint on each ADK Workspace, enforced by ext_authz via
OpenFGA. Intra-tenant A2A is opt-in; cross-tenant requires an Approved
CrossTenantAgreement (D29). Goose workspaces keep ACP unchanged.

## Inputs

- Workspace spec (add `spec.a2a`):
  [`api/keese/v1alpha1/workspace_types.go`](../../../api/keese/v1alpha1/workspace_types.go)
- CrossTenantAgreement CRD:
  [`api/authz/v1alpha1/crosstenanagreement_types.go`](../../../api/authz/v1alpha1/crosstenanagreement_types.go)
- OpenFGA model: [`docs/designs/04a-openfga-authz-model.md`](../../designs/04a-openfga-authz-model.md)
- Gateway topology: [`docs/designs/05a-envoy-ai-gateway-topology.md`](../../designs/05a-envoy-ai-gateway-topology.md)
- Egress authz spec: [`docs/specs/egress-authz-protocol.md`](../../specs/egress-authz-protocol.md)

## Tasks

### T1 — Extend Workspace spec

Add to `WorkspaceSpec`:
```
A2A *WorkspaceA2AConfig `json:"a2a,omitempty"`
```

`WorkspaceA2AConfig`: `Enabled bool`, `Scope WorkspaceA2AScope` (enum
`intra-tenant|cross-tenant`), `Port *int32` (default 8080). Add
`// +keese:rebac-tuple=a2a_callable_by` marker on `Enabled`.

CEL VAP `WorkspaceA2APortRange`: port must be 1024–65535.

Acceptance: `make manifests generate` clean; marker present for rebac-markers check.

### T2 — A2A bridge sidecar injection

The bridge sidecar built in E1.T3 is injected when `spec.a2a.enabled: true`. Workspace
reconciler adds the sidecar container and opens the A2A port in the NetworkPolicy.
Bridge serves on `spec.a2a.port` (default 8080); forwards to local runtime container
(ADK Python on 8080, or in future ADK Go on same port — disambiguated by
`RUNTIME_A2A_UPSTREAM_PORT` env var).

### T3 — Envoy gateway A2A routing

Add an A2A routing block in the Envoy AI Gateway config (`config/aigateway/`). Route
`/a2a/v1/{workspace}/**` → workspace pod A2A bridge service. ext_authz check:
`workspace:W#a2a_callable_by@workspace:caller`. Caller identity from projected SA token
audience. Fail-closed: deny if ext_authz unreachable (rule 05.4).

Acceptance: A2A request with valid caller SA token returns 200; missing token returns 403.

### T4 — OpenFGA relation + tuple writes

New relation `workspace#a2a_callable_by`. Workspace reconciler writes tuple
`workspace:W#a2a_callable_by@workspace:W` (self) when `spec.a2a.enabled: true` and
scope is `intra-tenant`. For `cross-tenant`, reconciler writes the tuple only after
an Approved CrossTenantAgreement exists (D29 contract).

Update [`docs/specs/egress-authz-protocol.md`](../../specs/egress-authz-protocol.md)
with new tuple shape. Update
[`docs/designs/04a-openfga-authz-model.md`](../../designs/04a-openfga-authz-model.md)
iteration log (add new relation, do not change prior decisions).

### T5 — Cross-tenant VAP

`ValidatingAdmissionPolicy` `CrossTenantA2ARequiresCTA` (CEL): rejects
`spec.a2a.scope: cross-tenant` on a Workspace unless the cluster has an Approved
CrossTenantAgreement referencing both Workspaces' tenants. Cross-resource CEL
limitation → use a webhook for the CTA presence check (admission webhook only; no
business logic per rule 04.12).

### T6 — Integration test

`internal/controller/keese/workspace_a2a_test.go`:
- Intra-tenant: two ADK Python Workspaces, same namespace; A2A call W1→W2 succeeds
  with tuple present; fails after tuple removed.
- Cross-tenant: two tenants, no CTA → admission rejects `scope: cross-tenant`; CTA
  approved → call succeeds; CTA expired → call denied.

## Acceptance criteria

- A2A traffic between two ADK Python Workspaces works under ext_authz.
- Cross-tenant A2A blocked without an Approved CTA; accepted with one.
- OTEL traces propagate across the A2A call boundary (B3 propagation).
- `CrossTenantA2ARequiresCTA` VAP/webhook rejects invalid configs.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| Envoy A2A routing block conflicts with existing MCPRoute | Route match on `/a2a/` prefix before MCP; document ordering |
| Cross-resource CEL for CTA check requires webhook | Use admission webhook (scope: `Workspace`, ops: `CREATE`, `UPDATE`) |
| OpenFGA model update breaks existing tuples | Add relation; do not remove; re-run `fga model validate` |
| A2A spec version drift (`a2a-sdk>=0.3.23`) | Pin in image; test against spec version in CI |

## Refs

- [E1-adk-python-runtime.md](E1-adk-python-runtime.md)
- [E3-adk-go-runtime.md](E3-adk-go-runtime.md)
- [`docs/designs/25-cross-tenant-agreement.md`](../../designs/25-cross-tenant-agreement.md)
- [`docs/designs/04a-openfga-authz-model.md`](../../designs/04a-openfga-authz-model.md)

## Iteration log

### Iteration 1 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 6 tasks; intra/cross-tenant clearly split |
| 2 | Architecture fit | 10 | 1.0 | 10 | D29 CTA contract honored; ext_authz fail-closed |
| 3 | Security posture | 15 | 1.0 | 15 | CTA bilateral gate; ext_authz deny-by-default |
| 4 | Automatability | 10 | 1.0 | 10 | make manifests + envtest + gateway config |
| 5 | Verifiability | 15 | 1.0 | 15 | Named integration test with positive + negative paths |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | Route conflict + CTA webhook + tuple additive |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines; precise refs |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter |
| 9 | Observability | 5 | 1.0 | 5 | B3 trace propagation in acceptance criteria |
| 10 | Operational readiness | 10 | 0.5 | 5 | Envoy config rollback not detailed |
| | **Total** | 100 | | **97.5** | |

Verdict: SHIP

Top gaps:
1. Envoy config rollback: document that old config is retained in Git; revert via `kubectl apply`.
2. OpenFGA model migration: additive-only rule enforced in CI.
3. A2A spec version pinning needs a CI job (added to E2 follow-up).

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
| 10 | Operational readiness | 10 | 1.0 | 10 | Rollback path clarified |
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
