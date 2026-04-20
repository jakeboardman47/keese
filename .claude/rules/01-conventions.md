---
description: Repo-wide conventions (always loaded)
paths:
  - "**/*"
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) {{YEAR}} {{ORG_NAME}} -->

# Conventions (always loaded)

## License & copyright

Every source file begins with:

```
// SPDX-License-Identifier: Apache-2.0
// Copyright (c) {{YEAR}} {{ORG_NAME}}
```

Equivalent comment syntax for other languages (`#` for shell/YAML/Make/Nix/Python,
`<!-- … -->` for Markdown/HTML). `addlicense` pre-commit enforces this.

**Do not** add "updated YYYY" or per-file author names. Git history is the record.

## Commits (Conventional Commits, enforced by commitlint)

```
<type>(<scope>): <subject>

[optional body]

[optional footer — BREAKING CHANGE: …]
```

- Types: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `ci`, `build`, `perf`, `style`.
- Scopes align with top-level directories or phase IDs (`phase-03`, `phase-04`, …).
- Subject: imperative mood, lowercase, no trailing period, ≤72 chars.
- One logical change per commit. If a refactor landed alongside a fix, split it.

Examples:
```
feat(api): add new resource type
fix(core): retry on transient failure
docs(designs): add key-beliefs doc
refactor(controller): extract helper
```

## Bash

- Every script: `#!/usr/bin/env bash`, SPDX header, `set -euo pipefail`, `IFS=$'\n\t'`.
- `shellcheck` clean with `-x`. `shfmt -i 2 -ci -bn -w`.
- Idempotency: check-before-add. Re-running should be safe.
- Log via `scripts/lib/log.sh` helpers.
- Mutation boundary: wrap steps in `run::step <id> <desc> <func>` so resumes work.

## Documentation

- Every doc lives under `docs/` or `book/`.
- Every doc has SPDX + copyright comment and YAML frontmatter (see
  `docs/references/documentation-system.md`).
- Hard limit: 200 lines per doc. Split if longer.
- Never reproduce code in docs; reference `file:line` or an import.
- User-facing prose goes in `book/`; Claude-readable structure goes in `docs/`.

### Diagrams

- **Text-first, deterministic.** Every diagram is generated from a source-text
  file (D2, Mermaid, Graphviz/DOT). Never commit hand-drawn binaries,
  Visio/Lucid exports, or pasted screenshots.
- **Right tool per diagram type.** See
  [`../skills/diagram-authoring.md`](../skills/diagram-authoring.md).
- **Diagrams are a source of truth.** A diagram that drifts from implementation
  is a bug with the same severity as stale API docs. Every diagram header
  declares `source_refs:` (the code it depicts).
- **Ship diagram + code together.** A code change that alters depicted
  structure must update the relevant diagram in the same commit. The pre-commit
  `check-diagram-freshness` hook blocks commits where source and render
  have drifted.

## Refinement iterations

Plans, specs, and implementation targets run up to **three** review passes:

1. Correctness & security
2. Performance & quality
3. Operational readiness

Score against `docs/plans/rubric.md`. Target ≥ 90 / 100 before landing. Record scores
in `.plan-logs/` or in the plan doc's iteration table.
