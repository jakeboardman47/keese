<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: developer-experience
depends: [04b-projected-sa-identity.md, 12-network-isolation.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 13 — CLI Tunnel (WireGuard)

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? Human operators need to
attach to a running workspace from their local machine. A WireGuard tunnel
authenticated via SA token and OIDC provides secure, audited access without
exposing cluster ingress._

## Open questions (must be answered before `status: current`)

1. What authenticates the WireGuard peer — a short-lived OIDC token exchanged
   for a WireGuard public key, or a separate SA-derived credential?
2. How does the WireGuard server-side component deploy in-cluster — DaemonSet,
   per-workspace sidecar, or a shared gateway pod?
3. What is the audit trail for tunnel sessions (connection open/close, bytes
   transferred, authenticated identity)?
4. How does the tunnel interact with the workspace `NetworkPolicy` default-deny
   — is the WireGuard endpoint carved out automatically?
5. How does `kubectl-keese attach` invoke the WireGuard tunnel under the hood,
   and what fallback does it offer when WireGuard is unavailable (e.g.
   `kubectl exec` bridge)?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [08b-goose-acp-stdio-k8s.md](08b-goose-acp-stdio-k8s.md)
- [19-ide-and-debugging.md](19-ide-and-debugging.md)
- [../references/ide-and-debugging.md](../references/ide-and-debugging.md)

TODO(design-gate)
