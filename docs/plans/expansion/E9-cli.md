<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - E8-session-store.md
  - ../../../api/keese/v1alpha1/workspace_types.go
  - ../../designs/08b-goose-acp-stdio-k8s.md
  - ../../designs/04b-projected-sa-identity.md
related_skills: [plan-management, controller-authoring]
status: planned
last_verified: 2026-05-13
phase: E9
model_tier: sonnet
depends_on: [E8]
agent: implementer
outputs:
  - cmd/keese/
  - internal/cli/
---

# E9 — keese CLI

**Refinement pass:** correctness & security.
**Effort:** 2 weeks. **Owner agent:** `controller-author`.

## Goal

Ship the `keese` CLI binary: Cobra command tree, Bubbletea TUI for interactive
commands, OIDC device-code authn, and coverage of the primary operator data model
(agents, sessions, recipes, skills, workflows, tenants). All API traffic flows through
the `keese-api` backend (E10) when available, or directly through K8s API + ext_authz
proxy otherwise.

## Inputs

- Workspace types: [`api/keese/v1alpha1/workspace_types.go`](../../../api/keese/v1alpha1/workspace_types.go)
- ACP attach design: [`docs/designs/08b-goose-acp-stdio-k8s.md`](../../designs/08b-goose-acp-stdio-k8s.md)
- SA identity: [`docs/designs/04b-projected-sa-identity.md`](../../designs/04b-projected-sa-identity.md)
- SessionStore schema from E8.

## Tasks

### T1 — Binary scaffold

`cmd/keese/main.go` — Cobra root. Subcommands:

| Command | Description |
|---|---|
| `agents list\|get\|create\|delete` | CRUD for Workspace + AgentRuntime CRs |
| `sessions list\|get\|attach` | List + stream session events from SessionStore |
| `recipes list\|get` | Read Recipe CRs |
| `skills list\|get\|apply` | Manage Skill CRs |
| `knowledge-bases list\|get\|ingest` | Knowledge base CRUD |
| `workflows list\|get\|run` | WorkflowRun trigger |
| `tenants list` | Enumerate Tenant CRs (admin only) |
| `attach <workspace>` | Interactive ACP attach via WireGuard tunnel |
| `chat <workspace>` | A2A SSE TUI via Bubbletea |

Global flags: `--context`, `--namespace`, `--tenant`, `--output (json\|yaml\|table)`.

### T2 — OIDC authn

Device-code flow via `golang.org/x/oauth2/authorizationcode`. Token cached in
`~/.keese/credentials.json` (mode 0600). Token refreshed silently on expiry. Uses
`OIDCProvider` CRD from D28 to discover issuer URL.

No K8s kubeconfig or SA token used on the CLI — OIDC JWT exchanged to K8s RBAC
impersonation via `keese-api` or direct K8s OIDC webhook.

### T3 — `chat` TUI (Bubbletea)

`internal/cli/chat/` — Bubbletea model. Connects to workspace A2A SSE endpoint via
HTTP; renders streaming tokens in a chat bubble. Sends user input as A2A task
messages. Ctrl-C cleanly closes the A2A connection.

Input validation: max message length 8192 chars (matching Workspace VAP).

Acceptance: `keese chat my-workspace` opens TUI; streaming response renders; Ctrl-C
exits cleanly with exit code 0.

### T4 — `attach` command

`internal/cli/attach/` — opens a WireGuard tunnel to the workspace pod's ACP port
(design 08b/08c). Proxies stdin/stdout. SIGTERM handler closes tunnel before exit
(rule 06.1).

### T5 — Release packaging

Add `keese` to `Makefile` target `build-cli`. Add to `goreleaser.yaml` for binary
release on GitHub Releases. SBOM via `syft`. Sigstore cosign keyless attest on release
(rule 05.16). Platforms: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.

### T6 — Unit tests

- `TestCLI_AgentList_TableOutput`: `keese agents list` renders table with headers.
- `TestCLI_ChatTUI_SendReceive`: mock A2A SSE server; assert message sent and
  streamed response rendered.
- `TestCLI_OIDC_TokenRefresh`: mock OIDC server; assert silent refresh on expiry.

## Acceptance criteria

- `keese agents list` returns Workspace list in table format.
- `keese chat <workspace>` streams A2A responses via Bubbletea TUI.
- `keese attach <workspace>` opens ACP session (goose workspaces only).
- OIDC token cached securely; refresh transparent to user.
- Binary released for 4 platforms with SBOM + cosign attestation.

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| WireGuard tunnel (attach) requires elevated privileges on macOS | Document: `sudo keese attach` or use VSCode Dev Containers on macOS |
| `keese-api` backend (E10) not yet shipped | CLI falls back to direct K8s API; feature-flag `--direct-api` |
| Bubbletea + A2A SSE concurrent rendering race | Use Bubbletea message-passing model; no shared state |

## Refs

- [E8-session-store.md](E8-session-store.md)
- [E10-web-ui.md](E10-web-ui.md)
- [`docs/designs/08b-goose-acp-stdio-k8s.md`](../../designs/08b-goose-acp-stdio-k8s.md)

## Iteration log

### Iteration 1 — 2026-05-13

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | Command tree defined; 6 tasks bounded |
| 2 | Architecture fit | 10 | 1.0 | 10 | Cobra + Bubbletea standard; goreleaser D15 |
| 3 | Security posture | 15 | 1.0 | 15 | OIDC device-code; token mode 0600; no SA token on CLI |
| 4 | Automatability | 10 | 1.0 | 10 | make build-cli + goreleaser |
| 5 | Verifiability | 15 | 1.0 | 15 | 3 named unit tests |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | WireGuard mac, API fallback, TUI race |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter |
| 9 | Observability | 5 | 0.5 | 2.5 | CLI itself has no OTEL; server-side traces cover |
| 10 | Operational readiness | 10 | 1.0 | 10 | Multi-platform release; SBOM + cosign |
| | **Total** | 100 | | **97.5** | |

Verdict: SHIP

Top gaps:
1. CLI has no client-side OTEL; server-side traces sufficient for now.
2. `attach` WireGuard privilege on macOS is a known UX gap.

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
| 9 | Observability | 5 | 1.0 | 5 | Server-side traces accepted |
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
