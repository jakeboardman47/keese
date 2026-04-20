<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: index
depends: []
related_skills: [doc-authoring]
status: current
last_verified: 2026-04-19
---

# docs/ — machine-readable project documentation

Docs in this tree are **optimized for Claude and other automation**. User-facing prose
lives in `book/` (mkdocs). Keep it that way.

## Five trees

| Tree | Question answered | Lifecycle | Frontmatter scope |
|---|---|---|---|
| [designs/](designs/) | **WHY** | Stable; change requires an architecture-review commit | `design` |
| [specs/](specs/) | **WHAT** (contract) | Testable; parsed by e2e / verification harnesses | `spec` |
| [plans/](plans/) | **HOW** (phased) | Execution-oriented; one file per phase | `plan` |
| [features/](features/) | **WHAT IS BUILT** | Implemented behavior on `main`; updated as phases iterate | `feature` |
| [references/](references/) | **HOW** (steady-state) | Living; updated as tooling evolves | `reference` |

## Conventions

- Every doc starts with SPDX + copyright HTML-comment header and YAML frontmatter
  (see [references/documentation-system.md](references/documentation-system.md)).
- Hard limit: 200 lines per doc. Split if longer.
- Never reproduce source code — reference `file:line` ranges instead.
- Cross-link aggressively; let Claude follow pointers rather than preloading context.
- User prose belongs in `book/`. If a doc starts reading like a tutorial, move it.

## Navigation

- Overall project index: [../CLAUDE.md](../CLAUDE.md)
- Plans: [plans/README.md](plans/README.md) (also contains [plans/rubric.md](plans/rubric.md))
- References: [references/README.md](references/README.md)

## Contributing to docs

Use the `doc-authoring` skill. It enforces frontmatter, length, and cross-reference
discipline. See [.claude/skills/doc-authoring.md](../.claude/skills/doc-authoring.md).
