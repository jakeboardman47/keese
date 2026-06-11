<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - E1-adk-python-runtime.md
  - E1b-adk-python-a2a-bridge.md
  - ../../designs/05a-envoy-ai-gateway-topology.md
related_skills: [plan-management, controller-authoring]
status: planned
last_verified: 2026-06-10
phase: E1c
model_tier: sonnet
depends_on: [E1b]
agent: controller-author
outputs:
  - internal/runtime/providers/adkpython/
  - internal/controller/keese/workspacesession_controller.go
---

# E1c — ADK Python NetworkPolicy + envtest hardening

Final E1 increment (see [E1](E1-adk-python-runtime.md)). Locks down the ADK Python
pod's network and completes the envtest suite. When this merges, **mark E1
`status: shipped`** (unblocking E2/E4/E11/E12).

## Scope — E1 tasks T4, T7 (completion)

- **T4 NetworkPolicy** — render it in `adkpython.BuildNetworkPolicy(...)` and wire the
  SSA apply into `workspacesession_controller.go` (a new `applySessionNetworkPolicy`,
  **adkPython branch only** — `applySessionPod` applies only the pod today, so the goose
  path stays untouched). `fieldOwner: keese-workspacesession-controller`. Fail-closed:
  default-deny all ingress+egress (rule 04.17 + 05.4), then **exact** allows only —
  egress to `envoy-ai-gateway.keese-system:443`, egress to NATS `:4222`, ingress from
  peer workspace pods on `:8081`, egress to peer workspace pods on `:8081`. **No
  wildcards** (rule 05.5).
- **T7 envtest completion** —
  `TestADKPythonProvider_NetworkPolicy` (default-deny base + exact egress/ingress rules,
  assert no wildcard) and confirm `TestADKPythonProvider_ThreeReconcileIdempotency`
  (SSA ×3, no spec change) covers the NetworkPolicy object too.

## Acceptance

- `CGO_ENABLED=0 go test -race -count=1 -tags=integration ./internal/runtime/... ./internal/controller/keese/...`
  green incl. the NetworkPolicy + idempotency cases; the NetworkPolicy enumerates exact
  endpoints (no `{}`/`to:[]`/open `ipBlock`). `make lint` clean. SSA-only (rule 04.7).
- On merge: set E1 `status: shipped` + its README row, since E1a+E1b+E1c complete it.

## Notes

- Stay inside `internal/runtime/providers/adk/`. **Commit per logical unit.** **Never
  run bare `git stash`/`pop`/`reset`/`checkout <branch>`** (hits the shared checkout).
  macOS → `CGO_ENABLED=0`. This closes the E1 runtime; E2 (A2A enforcement) follows.
