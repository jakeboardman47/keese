---
name: diagram-authoring
description: Author text-based diagrams (D2, Mermaid, Graphviz) and keep them in sync with code
type: skill
depends: [doc-authoring]
options: []
model: sonnet
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Diagram Authoring

## When to use

Creating or updating any diagram in `docs/**` or `book/**`. Also when a code
change alters depicted structure — the affected diagram must ship in the same commit.

## Tool-per-type

| Diagram type | Preferred tool | Render command |
|---|---|---|
| Hierarchical layout / container topology | **D2** | `d2 in.d2 out.svg` |
| Message flow / sequence | **Mermaid** sequence | inline in Markdown, or `mmdc -i in.mmd -o out.svg` |
| Lifecycle / state machines | **Mermaid** state | same |
| Entity / owner-ref graphs | **Mermaid** ER *or* **D2** | D2 if nested, Mermaid if flat |
| Generic flowchart | **Mermaid** flowchart | inline |
| Dense dependency graph | **Graphviz / DOT** | `dot -Tsvg in.dot -o out.svg` |
| Inline pseudo-diagram in code comments | ASCII | — |

## File conventions

- Source sits next to the doc: `docs/<tree>/diagrams/<slug>.{d2,mmd,dot}`.
- Render sits beside source with a matching name: `docs/<tree>/diagrams/<slug>.svg`.
- **Commit source AND render.** The pre-commit
  [check-diagram-freshness](../../scripts/check-diagram-freshness.sh) hook
  re-renders sources and blocks commits where source and render have drifted.
- Every diagram source begins with a header comment (syntax per tool):

  ```
  # SPDX-License-Identifier: Apache-2.0
  # Copyright (c) 2026 keese-ai
  # source_refs: pkg/foo/bar.go:42-180, internal/service/handler.go
  # depicts:     <what this diagram shows>
  ```

  `source_refs:` is load-bearing. A diagram that does not cite source is a
  stale diagram.

## Embedding

### In `docs/**` (machine-readable)

```markdown
![Component topology](diagrams/component-topology.svg)
<!-- source: diagrams/component-topology.d2 -->
```

### In `book/**` (mkdocs, user-facing)

Same image syntax. Material for MkDocs renders SVG natively.

### Mermaid inline (short, ≤ 40 lines)

Small Mermaid blocks may be inlined in the Markdown body — GitHub and mkdocs
render them natively. Larger Mermaid belongs in a `.mmd` file next to the doc.

## Keeping in sync

Diagrams are a **source of truth** for users. Drift is a bug. The rules:

1. Change to code that alters depicted structure → update the diagram in the
   **same commit**.
2. If a change invalidates a diagram but the new diagram is out of scope: add
   `status: stale` to the diagram's header comment and open a follow-up phase
   doc. Stale is tolerated for **one phase** maximum.
3. Renaming, moving, or deleting a source file cited in `source_refs:`:
   update the `source_refs:` line or stale-mark the diagram.

## Steps — new diagram

1. Pick the tool from the matrix above.
2. Create the source file under `docs/<tree>/diagrams/<slug>.{d2,mmd,dot}`.
3. Add SPDX + copyright header + `source_refs:` + `depicts:` metadata.
4. Render locally: `d2`, `mmdc`, or `dot`.
5. Commit source and render together.
6. Link from the owning doc with standard Markdown image syntax.
7. Pre-commit re-renders on save; if drift appears, re-render and re-stage.

## Verification

- `scripts/check-diagram-freshness.sh` reports no drift.
- The SVG renders cleanly in GitHub preview and in mkdocs `mkdocs serve`.
- Every `source_refs:` path still exists (a CI link-check will fail if not).

## References

- Reference cookbook: [../../docs/references/diagram-authoring.md](../../docs/references/diagram-authoring.md)
- Rule: [../rules/01-conventions.md](../rules/01-conventions.md) (Documentation → Diagrams)
