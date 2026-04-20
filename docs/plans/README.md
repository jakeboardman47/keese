<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: index
depends: [../designs/README.md]
related_skills: [plan-management]
status: current
last_verified: 2026-04-19
---

# plans/ — HOW (phased)

One phase per file. Each file is a standalone, independently loadable plan with
frontmatter, work items, verification, exit criteria, and an iteration log scored
against [rubric.md](rubric.md).

## Phases

| # | Phase | Depends on | Model tier | Status |
|---|---|---|---|---|
| 00 | _add your first phase here_ | — | sonnet | draft |

## Parallel execution groups

Document which phases can run in parallel once prerequisites land. Example:

- **Group A**: phase-02, phase-03 can run in parallel after phase-01 lands.
- **Group B**: phase-04 depends on phase-02 + phase-03.

## Scoring

Every phase file must have an iteration log (see
[../../.claude/skills/plan-management.md](../../.claude/skills/plan-management.md)).
Target ≥ 90/100 before marking `status: in-progress`.

## Rubric

See [rubric.md](rubric.md).
