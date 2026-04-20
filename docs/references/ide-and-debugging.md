<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: developer-experience
depends: []
related_skills: []
status: draft
last_verified: 2026-04-19
---

# IDE and Debugging

> **Status: draft.** Stub — fill in after design 19 reaches `status: current`.
> See also `dev/ide/{goland,vscode}/` for generated run configs.

## Contents (to expand)

1. **GoLand setup** — Go SDK, kubeconfig context, remote dlv attach config
   (host: `localhost`, port: `2345`, source-root mapping to repo root).
2. **VSCode setup** — `block.vscode-goose` extension; Go extension dlv attach
   launch config; `tasks.json` for `make tilt-up`.
3. **`kubectl-keese attach`** — installation, OIDC auth flow, WireGuard fallback
   to `kubectl exec` bridge.
4. **dlv-in-kind** — `make tilt-up` with `-gcflags='-N -l'`; `kubectl port-forward`
   manager pod :2345; breakpoints in reconcile loops.
5. **Workspace status debugging** — fields (`lastReconcileError`, `reconcileCount`,
   `phaseTransitionTimestamps`) and `kubectl describe workspace` output.

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [../designs/19-ide-and-debugging.md](../designs/19-ide-and-debugging.md)
- [tilt-local-loop.md](tilt-local-loop.md)

TODO(design-gate)
