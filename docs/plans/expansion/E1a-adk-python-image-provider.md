<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - E1-adk-python-runtime.md
  - E0-runtime-spi-expansion.md
  - ../../../internal/runtime/providers/goose/goose.go
  - ../../designs/05a-envoy-ai-gateway-topology.md
related_skills: [plan-management, controller-authoring]
status: shipped-with-stubs
last_verified: 2026-06-11
revisit_when_image_built: true
phase: E1a
model_tier: sonnet
depends_on: [E0]
agent: controller-author
outputs:
  - Dockerfile.adk-python
  - internal/runtime/providers/adkpython/
  - internal/controller/keese/workspacesession_controller.go
---

# E1a — ADK Python image + provider pod template + discriminator

First of three E1 increments (see [E1](E1-adk-python-runtime.md) for full context +
security rules). Delivers a working **single-container** ADK Python pod that the
WorkspaceSession reconciler routes to. The A2A bridge sidecar (E1b) and the
NetworkPolicy (E1c) follow.

## Scope — E1 tasks T1, T2, T5 (+ PodRender envtest)

- **T1 Dockerfile.adk-python** (repo root): `python:3.12-slim` base → install
  `google-adk>=1.28.1 a2a-sdk>=0.3.23` + thin entrypoint wrapper → distroless
  Python final stage (`gcr.io/distroless/python3-debian12`). Digest-pinnable.
- **T2 `Bootstrap`** in `internal/runtime/providers/adk/python_provider.go` (fill the
  E0 stub): single ADK container (image from `ADKPythonSpec.Image`), **non-secret env
  only — NO API keys** (rule 05.2; gateway base-URLs per E1 T2), session PVC + projected
  SA token (`keese-egress-<tenant>`, 600s, rule 05.7) + CA bundle, hardened
  SecurityContext (RO root FS, drop ALL, non-root, rule 05.11). Mirror the goose provider.
- **T5 discriminator** in `internal/controller/keese/workspacesession_controller.go`:
  when `AgentRuntime.spec.implementation.adkPython` is set, call
  `adkPythonProvider.Bootstrap(...)` instead of the goose path; goose unaffected.

## Acceptance

- `TestADKPythonProvider_PodRender` (envtest): zero `*API_KEY*`/`*SECRET*` env vars,
  SA token projected, RO root FS, CA bundle mounted; + a 3-reconcile idempotency case.
- `CGO_ENABLED=0 go test -race -count=1 -tags=integration ./internal/controller/keese/... ./internal/runtime/...`
  green; `make lint` clean. SSA-only (rule 04.7). Docker build is a follow-up note (no
  `docker build` in the worktree — set `revisit_when_image_built` if you stub it).

## Notes

- Stay inside the three `outputs:` paths. **Commit per logical unit** (rate-limit
  resilience). **Never run bare `git stash`/`pop`/`reset`/`checkout <branch>`** (hits
  the shared checkout). macOS → `CGO_ENABLED=0`. E1b adds the bridge sidecar next.
