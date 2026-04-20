---
description: Score a plan or spec against docs/plans/rubric.md
argument-hint: "<path/to/plan-or-spec.md>"
allowed-tools:
  - Read
  - Edit
  - Glob
  - Grep
model: sonnet
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 Aviz Networks, Inc. -->

Score the document at `$ARGUMENTS` against `docs/plans/rubric.md`.

Steps:
1. Read the target doc and the rubric.
2. Score each category (0 / half / full). Show the math.
3. Return the total (out of 100), verdict (SHIP ≥ 85, REVISE 65–84, REPLAN < 65),
   the top 3 gaps, and the proposed next step.
4. Append an iteration row to the target doc's iteration table if the doc has one.

Keep the response to < 400 words.
