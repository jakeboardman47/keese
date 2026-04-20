<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: reference
depends: []
related_skills: [doc-authoring]
status: current
last_verified: 2026-04-19
---

# Documentation System

Project docs live in five trees under `docs/`. Each tree answers one question.

| Tree              | Question           | Example                                |
|-------------------|--------------------|----------------------------------------|
| `docs/designs/`   | **Why?**           | `0003-component-selection.md`          |
| `docs/specs/`     | **What?** (contract) | `my-resource-v1alpha1.md`            |
| `docs/plans/`     | **How, phased?**   | `phase-02-auth-rewrite.md`             |
| `docs/features/`  | **What is built?** | `my-feature.md`                        |
| `docs/references/`| **How, steady?**   | `kubebuilder-workflow.md`              |

## Frontmatter by Scope

Every doc starts with the SPDX header (HTML comment) then YAML. Only the `scope`
line and a few scope-specific fields change.

**design** (`docs/designs/NNNN-title.md`):

```markdown
<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: adr
depends: [docs/designs/0001-system-topology.md]
related_skills: [architect]
status: accepted   # proposed | accepted | superseded
last_verified: YYYY-MM-DD
---
```

**spec** (`docs/specs/<kind>-v<version>.md`):

```markdown
---
scope: spec
category: contract
depends: [docs/designs/0002-related-design.md]
related_skills: [doc-authoring]
status: current
last_verified: YYYY-MM-DD
---
```

**plan** (`docs/plans/phase-NN-title.md`):

```markdown
---
scope: plan
category: phase
phase_id: phase-02
depends: [docs/specs/my-resource-v1alpha1.md]
related_skills: [implementer]
status: in_progress   # todo | in_progress | done | abandoned
last_verified: YYYY-MM-DD
---
```

**reference** (`docs/references/<topic>.md`): see top of this file.

## Hard Limits and Split Rules

- **200 lines max** per file (including frontmatter).
- If you approach the limit, split along natural seams:
  - Specs: split by sub-resource or domain.
  - Plans: split by phase (phase-02a, phase-02b).
  - References: split by subtopic.
- Never split designs — if one design grows, it's probably two decisions; file a second ADR.

## Cross-Reference Pattern

Point at a specific line range with the form `path/to/file.ext:START-END`:

```
See src/handler.go:142-175 for the request dispatch logic.
Spec at docs/specs/my-resource-v1alpha1.md:30-48 defines the status phases.
```

Prefer paths relative to the repo root for stability.

## Source-Code Annotations

Source files may carry special comment prefixes that tools index:

```
// Architecture: docs/designs/0002-system-topology.md
// Spec:         docs/specs/my-resource-v1alpha1.md#status
```

A `make docs-verify` target walks these comments and fails CI if the referenced path is missing.

## Adding a New Doc

1. Copy `docs/_templates/<scope>.md` (if present) into the right tree, or follow
   the shape above.
2. Fill frontmatter; keep `last_verified` equal to today.
3. Append a row to the appropriate index (e.g. `docs/references/README.md`).
4. If the doc adds a new repeatable task, add a row to the task matrix in `CLAUDE.md`.
5. Commit with `docs(<scope>): add <short-title>` — see
   [conventional-commits.md](conventional-commits.md).

## Related

- Skills: [`../../.claude/skills/doc-authoring.md`](../../.claude/skills/doc-authoring.md).
- Enforcement: `.pre-commit-config.yaml` runs `scripts/doc-lint.sh` (if configured).
- Index: [`../README.md`](../README.md).
