<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - E1-adk-python-runtime.md
  - E1a-adk-python-image-provider.md
  - ../../specs/egress-authz-protocol.md
related_skills: [plan-management, signal-handling]
status: planned
last_verified: 2026-06-10
phase: E1b
model_tier: sonnet
depends_on: [E1a]
agent: implementer
outputs:
  - internal/runtime/a2a/bridge/
  - internal/runtime/providers/adk/
  - docs/specs/egress-authz-protocol.md
---

# E1b — ADK Python A2A bridge sidecar

Second of three E1 increments (see [E1](E1-adk-python-runtime.md)). Adds the A2A
bridge sidecar to the E1a pod template + the not-yet-enforced ReBAC tuple stub that
E2 will enforce.

## Scope — E1 tasks T3, T6

- **T3 bridge binary** `internal/runtime/a2a/bridge/` (small Go): listens `:8081`
  (A2A inbound from peer workspaces), forwards to the ADK server on `localhost:8080`,
  reads the MCP server list from a projected ConfigMap
  (`/var/run/keese/mcp-config/config.json` — empty until E6, non-fatal), propagates
  trace context. Rule 06 SIGTERM (drain in-flight, exit 0) + a `cmd`-style signal test.
  Add the `a2a-bridge` sidecar container to `python_provider.go`'s pod template
  (image `$(OPERATOR_IMAGE_BASE)/a2a-bridge`, digest-pinned in prod).
- **T6 ReBAC stub:** document the `workspace:W#a2a_callable_by@workspace:caller`
  relation in `docs/specs/egress-authz-protocol.md` (NOT enforced yet — E2 enforces);
  add the `// +keese:rebac-tuple=a2a_callable_by` marker so `check-rebac-markers.sh`
  passes on the field E2 will add.

## Acceptance

- `TestA2ABridgeForward` (unit): bridge forwards `:8081`→`localhost:8080`; the bridge
  binary compiles + the SIGTERM test passes.
- The E1a `PodRender` envtest still passes with the sidecar added (two containers, both
  hardened, no API keys). `CGO_ENABLED=0 go test -race ./internal/runtime/...` green;
  `make lint` clean.

## Notes

- Stay inside the three `outputs:` paths. **Commit per logical unit.** **Never run
  bare `git stash`/`pop`/`reset`/`checkout <branch>`** (hits the shared checkout).
  macOS → `CGO_ENABLED=0`. E1c adds the NetworkPolicy (incl. the `:8081` peer rules).
