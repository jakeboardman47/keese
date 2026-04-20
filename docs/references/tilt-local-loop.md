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

# Tilt Local Loop

> **Status: draft.** Stub — fill in after P7 infra bootstrap is complete.

## Contents (to expand)

1. **`make tilt-up`** — ctlptl kind cluster creation + local registry wiring.
2. **Live-update config** — `sync('./bin/manager', '/manager')` + restart trigger.
3. **dlv attach** — `gcflags='-N -l'` build, port-forward :2345, GoLand/VSCode config.
4. **Helmfile dependency order** — cert-manager → Capsule → Envoy AI GW → keese operator.
5. **Feedback timing** — target 5–12s rebuild, ≤ 15s worst case; profiling tips.

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [ide-and-debugging.md](ide-and-debugging.md)

TODO(design-gate)
