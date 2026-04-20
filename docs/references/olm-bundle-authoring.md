<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: packaging
depends: []
related_skills: []
status: draft
last_verified: 2026-04-19
---

# OLM Bundle Authoring

> **Status: draft.** Stub — fill in after designs 14a and 14b reach `status: current`.

## Contents (to expand)

1. **`operator-sdk generate bundle`** — flags, CSV shape, icon, description fields.
2. **Channel strategy** — `alpha`/`beta`/`stable` + `replaces` chain maintenance.
3. **Dependency declarations** — GVK-based and package-based; version ranges.
4. **Webhook CA bootstrap** — cert-manager dependency ordering; `certmanager.io/inject-ca-from`.
5. **`operator-sdk bundle validate`** — scorecard suites; CI integration in `bundle.yaml`.

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [../designs/14a-olm-channels-upgrades.md](../designs/14a-olm-channels-upgrades.md)
- [../designs/14b-olm-dependencies.md](../designs/14b-olm-dependencies.md)

TODO(design-gate)
