<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: rubric
depends: [rubric.md, README.md, ../references/documentation-system.md]
related_skills: [doc-authoring, diagram-authoring]
status: current
last_verified: 2026-05-29
---

# Documentation Rubric — efficacy & completeness

Scores the **user-facing book (`book/`)** and the **machine-side feature docs
(`docs/features/`)** for how well they teach a newcomer to *use* keese and how
completely they reflect what is implemented on `main`. Companion to the
plan/spec rubric in [rubric.md](rubric.md) (which scores design correctness, not
teaching efficacy). Total 100 points.

Verdicts: **SHIP ≥ 90** · **REVISE 70–89** · **REWORK < 70**. Target ≥ 90 per the
project convention (`CLAUDE.md` → Refinement iterations).

## Categories and weights

| # | Category | Weight | Full points require |
|---|---|---:|---|
| 1 | **Feature coverage / completeness** | 15 | Every implemented feature (per the audit inventory) has both a concept/reference entry and, where user-actionable, a how-to. No silent gaps. |
| 2 | **Accuracy vs. code** | 15 | Every claim matches `main`. No stale status ("gate closed", "stub-only"). Commands, field names, kinds, paths verified against source; cite `file:line`. |
| 3 | **Progressive disclosure / audience fit** | 10 | Quick-reference *and* deep prose. Distinct paths for cluster-operator, tenant-admin, agent-developer, contributor. Concepts build in order; each page states its audience + prerequisites. |
| 4 | **Findability / information architecture** | 10 | Logical nav grouping; working cross-links; glossary; search-friendly headings; every page reachable from nav. |
| 5 | **Task orientation / actionability** | 10 | Guides are runnable end-to-end: copy-pasteable commands, sample manifests, expected output, verification + teardown. Provisioning/setup actually works. |
| 6 | **Diagrams** | 10 | Copious, varied, correct: ≥1 diagram per concept page; uses the right type (sequence/flow/state/ER/class/C4/gantt) per the tool matrix; each illustrates something the prose can't say as fast; all render. |
| 7 | **Examples & scenarios** | 8 | ≥3 end-to-end narrative scenarios spanning multiple features; realistic sample YAML; shows inputs → actions → observable outcome. |
| 8 | **Reference quality** | 10 | API reference covers all CRDs/groups (kinds, key fields, status, printer cols); CLI/tooling reference; config (feature gates, env); metrics/events reference. Precise, not hand-wavy. |
| 9 | **Developer / SDLC coverage** | 7 | SDLC (design-gate → spec → plan → code → feature), CI/CD pipeline, testing strategy (unit/envtest/kuttl/e2e), build & release (OLM/cosign), and an honest built-vs-remaining roadmap. |
| 10 | **Build & hygiene** | 5 | `cd book && mkdocs build --strict` passes (no broken nav/links); SPDX headers present; consistent style/admonitions; conventional structure; no orphan pages. |

## Scoring rules

- Score each category ratio ∈ {0, 0.25, 0.5, 0.75, 1.0} (finer than the plan
  rubric, so iteration progress is visible).
  - **1.0** every bullet satisfied. **0.75** strong, one minor gap. **0.5**
    present but a notable gap. **0.25** stub-level. **0** absent / blocks use.
- Total = Σ(weight × ratio). Round to one decimal.
- Score the **whole documentation set per category** (not per page) for the
  headline number; additionally spot-score weak pages to target revisions.

## Completeness checklist (Category 1 + 8 gate)

A `1.0` on coverage requires, at minimum, that the set contains:

- [ ] Concept page for each subsystem: tenancy, workspaces/sessions, identity &
      zero-trust, ReBAC/OpenFGA, egress & AI Gateway, credential broker, agent
      runtimes (SPI + goose + ADK), memory, RAG, workflows & triggers,
      guardrails, token budgets & observability, transports/messaging, recipes,
      feature gates, cross-tenant agreements, process lifecycle/supervision.
- [ ] How-to guide for each user-actionable capability (install, bootstrap,
      provision tenant, workspace+session, runtime config, recipe, memory, RAG,
      guardrails, token budgets, egress credentials, cross-tenant, cloud deploy,
      backup/DR, observability, feature gates).
- [ ] API reference for all three groups and every CRD kind.
- [ ] CLI/tooling reference (operator binaries, `kubectl` usage, Make targets,
      planned `keese` CLI clearly marked planned).
- [ ] `docs/features/` doc for each implemented feature (per the features/
      template + 200-line limit).
- [ ] Built-vs-remaining roadmap that does not overclaim.

Any unchecked box caps Category 1 at 0.5.

## Iteration log template

Copy into the iteration log doc (`docs/plans/documentation-iterations.md`):

```
### Iteration N — YYYY-MM-DD — focus: <coverage|accuracy|polish>

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Feature coverage | 15 | | | |
| 2 | Accuracy vs code | 15 | | | |
| 3 | Progressive disclosure | 10 | | | |
| 4 | Findability / IA | 10 | | | |
| 5 | Task orientation | 10 | | | |
| 6 | Diagrams | 10 | | | |
| 7 | Examples & scenarios | 8 | | | |
| 8 | Reference quality | 10 | | | |
| 9 | Developer / SDLC | 7 | | | |
| 10 | Build & hygiene | 5 | | | |
| | **Total** | 100 | | **NN.N** | |

Verdict: SHIP | REVISE | REWORK
Top gaps: 1… 2… 3…
Next step: …
```

## Refinement passes (per the project's three-pass discipline)

1. **Coverage & accuracy** — does it document everything that is built, correctly?
2. **Clarity & teaching** — progressive prose, diagrams, examples, findability.
3. **Polish & build** — links, style, `mkdocs build --strict`, conventions.

Iteration cap: 3 passes on the same structure. If still < 90, split a section or
rescope a page — do not iterate a fourth time on the same outline.
