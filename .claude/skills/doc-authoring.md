---
name: doc-authoring
description: Authoring docs under docs/ with proper frontmatter and cross-references
type: skill
depends: []
options: []
model: haiku
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) {{YEAR}} {{ORG_NAME}} -->

# Doc Authoring

## When to use

Writing or updating any doc under `docs/**`.

## Template

```markdown
<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) {{YEAR}} {{ORG_NAME}} -->

---
scope: design | spec | plan | feature | reference
category: <directory name, e.g. designs, plans, specs, features, references>
depends: [list of sibling doc paths the reader should already know]
related_skills: [list of .claude/skills/* names]
status: draft | current | implemented | historical | superseded
last_verified: YYYY-MM-DD
---

# <Title>

<one-sentence intent>

<body>
```

## Rules

- Hard limit: 200 lines per doc. Split if longer.
- Never reproduce code — reference `pkg/foo/bar.go:42-80`.
- Link aggressively; do not duplicate what a sibling doc already explains.
- Use tables for inventories, numbered steps for algorithms.
- Keep user-facing prose in `book/` (mkdocs). Machine-readable goes in `docs/`.
- **Feature docs** (`docs/features/`) are authored when the implementing plan reaches
  `status: implemented` on `main`. They are **not** release-gated — features
  iterate across multiple plan phases before any tag is cut. A feature doc cites
  the design(s), spec(s), plan(s), and source files that implement it. See
  [../../docs/features/README.md](../../docs/features/README.md) for the template.

## When to split

- If a single doc tries to answer two questions, split.
- If headings below `##` proliferate, it's usually two docs in a trench coat.
- Always leave a redirect line pointing to the new locations.

## References

- [../../docs/references/documentation-system.md](../../docs/references/documentation-system.md)
