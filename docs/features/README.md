<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) {{YEAR}} {{ORG_NAME}} -->

---
scope: reference
category: index
depends: [../designs/README.md, ../specs/README.md]
related_skills: [doc-authoring]
status: current
last_verified: {{LAST_VERIFIED}}
---

# features/ — WHAT IS BUILT

Feature docs summarize **what is implemented on `main`** in user-visible terms, one
file per feature. They close the loop from design → spec → plan → code by giving a
single place to look up "does the project do X, and how?"

A feature doc is written as an output of the plan phase that implements it — **not**
when a release is tagged. The feature doc then evolves as subsequent plan phases
iterate on the capability. Release tagging is a separate event; release notes
will cite feature docs that have landed since the previous tag.

## How features/ differs from the other trees

| Tree | Answers | Written when | Updated when |
|---|---|---|---|
| `designs/` | **Why** the system is shaped this way | Before the work | Beliefs change |
| `specs/` | **What** the contract looks like | During spec phase | Contract evolves |
| `plans/` | **How** to execute a phase of work | Before implementation | Phase iterates |
| `features/` | **What is built** and what it does for the user | When the implementing plan reaches `status: implemented` | Each phase that refines the capability |
| `references/` | **How** to perform a recurring operation | Anytime | Tools / vendors evolve |

A feature doc is the **outcome statement**. It links back to the design(s),
spec(s), and plan(s) that produced it, and forward to the source files that
implement it. The feature may iterate many times in code before any release tag.

## When to add a feature doc

- A plan in `plans/` reaches `status: implemented` — the feature doc is one of
  that phase's **outputs** (declared in the plan's frontmatter).
- Subsequent plans that refine the capability update the existing feature doc
  rather than creating new ones; each change appends to the feature's
  `## Change history` section.
- A breaking reshape: the old behavior gets `status: superseded` with a forward
  link; a new feature doc describes the new behavior.

Do **not** add a feature doc for internal refactors, test scaffolding, or
anything without user-visible surface area. Do **not** wait for a release tag —
the feature doc exists as soon as the code on `main` implements it.

## File format

```markdown
<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) {{YEAR}} {{ORG_NAME}} -->

---
scope: feature
category: feature
depends: [list of docs that define / justify this feature]
implements_specs: [list of docs/specs/* this feature implements]
implements_plans: [list of docs/plans/phase-*.md phases that built or refined this feature]
source_refs: [list of file:line ranges — where this is implemented]
related_skills: [list of .claude/skills/* for modifying this feature]
status: in-development | implemented | deprecated | superseded
implemented_in_phase: phase-NN-slug
first_released_in:
last_verified: YYYY-MM-DD
---

# <Feature name>

## Summary

<One paragraph — what this feature does for the user.>

## Behavior

<Bulleted list of concrete behaviors. How a user invokes it, what they see, edge cases.>

## Configuration surface

<Fields, flags, env vars that control this feature. Reference the spec; don't reproduce it.>

## Observability

<Metrics, events, conditions this feature surfaces.>

## Known limitations

<What this feature does NOT do, or current constraints.>

## Change history

<One line per notable behavior change. Link to the commit or plan phase.>

## References

- Design: `docs/designs/NN-*.md`
- Spec: `docs/specs/*.md`
- Plan: `docs/plans/phase-NN-*.md`
- Source: `...`
```

## Conventions

- **Hard limit: 200 lines.** Split if the feature has multiple distinct sub-capabilities.
- **One feature per file.** Do not combine unrelated capabilities — separate
  docs, cross-linked.
- **Status** reflects the feature's state **on `main`**, not release-readiness:
  - `in-development` — implementing plan is executing; behavior partial on `main`.
  - `implemented` — behavior complete on `main`. May still iterate.
  - `deprecated` — still present, scheduled for removal; add
    `removed_in_phase:` once the removal phase exists.
  - `superseded` — behavior replaced by a newer feature doc; link to it.

  Release-readiness (alpha / beta / GA) is tracked separately on release tags,
  not in feature doc frontmatter.
- **Link to source.** Every feature doc cites the source files that implement it
  (`source_refs:` in frontmatter). Keeps code and docs honest.

## Contents

Populated as features land on `main`. Each feature doc is independently loadable.
Avoid preloading the directory; use CLAUDE.md routing and the feature's
`source_refs:` to load only what's needed.
