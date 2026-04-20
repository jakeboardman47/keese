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

# designs/ — WHY

Design docs explain **why** the project is shaped the way it is. Each doc answers a
single architectural question. Together they form a coherent position.

A design doc:

- Starts with a clear question or thesis.
- Lists options considered and their trade-offs.
- Records a decision and the constraints that forced it.
- References the specs that will implement it.
- Does **not** include implementation details — those belong in specs, plans, or references.

## Contents

| # | Doc | Topic |
|---|---|---|
| 00 | _add your first design doc here (e.g. 00-why.md)_ | — |

## Lifecycle

- Stable. Changes must be discussed in a plan iteration before landing.
- `last_verified` is bumped whenever the doc is re-read against current code and
  found accurate.
- Conflicting documents are a bug; resolve by retiring one (status: `superseded`).
