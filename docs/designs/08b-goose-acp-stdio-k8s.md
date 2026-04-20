<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: runtime
depends: [08a-goose-headless-modes.md, 09-transport-crd.md]
related_skills: []
status: draft
last_verified: 2026-04-19
rollback: TODO — document migration path when status flips to current
---

# 08b — Goose ACP stdio over K8s

> **Status: draft.** This doc is a gate-placeholder. Architect agent
> fills content across 3 rubric iterations (target ≥ 90/100) before
> any spec or controller code references this design.

## Context

_One paragraph. What problem does this design address? Running ACP over stdio
in Kubernetes requires bridging the no-open-port constraint via `kubectl exec`
attach. This design covers the stdio transport adapter and the
`kubectl-keese attach` plugin._

## Open questions (must be answered before `status: current`)

1. What is the exact `kubectl exec` invocation used by `kubectl-keese attach`
   to multiplex ACP stdio, and how does it handle websocket reconnection?
2. How does the stdio bridge authenticate the attaching client — SA token,
   OIDC, or a short-lived kubeconfig credential?
3. What is the backpressure and buffering model when the agent is processing
   and the client sends a new message — queue, drop, or error?
4. How does the stdio transport interact with the `Transport` CRD — is it a
   first-class `spec.type: stdio` or an implementation detail hidden inside
   the runtime?
5. What is the cleanup contract when the `kubectl exec` session drops — does
   the agent checkpoint and pause, or terminate?

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [08a-goose-headless-modes.md](08a-goose-headless-modes.md)
- [09-transport-crd.md](09-transport-crd.md)
- [13-cli-tunnel-wireguard.md](13-cli-tunnel-wireguard.md)

TODO(design-gate)
