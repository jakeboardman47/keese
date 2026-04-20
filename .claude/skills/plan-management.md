---
name: plan-management
description: Phase file format, rubric scoring, iteration cap, split-vs-extend heuristic
type: skill
depends: []
options: [rubric_path, plans_dir]
model: sonnet
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Plan Management

## When to use

Creating or revising a `docs/plans/phase-*.md` file, or running the rubric loop.

## Inputs

- `rubric_path`: default `docs/plans/rubric.md`.
- `plans_dir`: default `docs/plans/`.

## Phase file shape

```markdown
---
scope: plan
category: phase
phase: NN
title: <Title>
depends_on: [NN, NN]
model_tier: haiku | sonnet | opus
inputs: [list of docs / prereqs]
outputs: [list of files this phase produces]
status: draft | planned | in-progress | implemented | historical | superseded
last_verified: YYYY-MM-DD
---

# Phase NN — <Title>

## Goal
## Preconditions
## Work items
## Verification
## Exit criteria
## Known gotchas
## Iteration log
| Iter | Date | Score | Verdict | Notes |
|------|------|-------|---------|-------|
```

## Scoring loop

1. Write / revise the phase doc.
2. `/score-plan docs/plans/phase-NN-*.md` — scores against
   [../../docs/plans/rubric.md](../../docs/plans/rubric.md).
3. Record iteration in the log table. Verdict: SHIP ≥ 85, REVISE 65-84, REPLAN < 65.
4. Revise and re-score. **Iteration 3 cap**: if still REVISE at 3, split or rescope.

## Split-vs-extend heuristic

- **Split** when the diff is growing unbounded, or unrelated risks are coupled.
- **Extend** when the scope is right but reviewers keep asking "but what about X?"

## MEMORY.md

After SHIP, add a one-liner to `MEMORY.md` decisions section linking the phase doc.
