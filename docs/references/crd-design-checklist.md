<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: api
depends: []
related_skills: []
status: draft
last_verified: 2026-04-19
---

# CRD Design Checklist

> **Status: draft.** Stub — fill in after design 20 (API group layout) reaches `status: current`.

## Contents (to expand)

1. **Naming conventions** — group one of `keese.ai`, `authz.keese.ai`, `policy.keese.ai` (see `docs/designs/20a-api-group-layout.md`), kind PascalCase, singular/plural/shortName.
2. **Schema requirements** — `openAPIV3Schema` with `description`, `// +kubebuilder:validation:*`,
   `XValidation` CEL rules, discriminated one-of for provider-style fields.
3. **Status and conditions** — `observedGeneration`, `conditions[]` with `type/status/reason/message`,
   printer columns mandatory.
4. **Immutability** — VAP CEL for immutable fields; list of fields that must be immutable per kind.
5. **Samples discipline** — ≥ 2 samples per CRD; `--dry-run=server` must pass; `// +kubebuilder:example`.

## Refs

- [../plans/rubric.md](../plans/rubric.md)
- [Plan file](/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md)
- [../designs/20-api-group-layout.md](../designs/20-api-group-layout.md)

TODO(design-gate)
