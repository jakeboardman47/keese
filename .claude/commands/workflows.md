---
description: One surface to see, tail, and control every parallel run (conductor + issue + chat/Workflow)
argument-hint: "[board | tail <id> | kill <id> | pause <id> | resume <id>]"
allowed-tools:
  - Bash(conductor/workflows.sh *)
  - Read
model: sonnet
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

One surface to see, tail, and control every parallel run — conductor program
runs + phases, GitHub-issue jobs, and chat/Workflow-tool runs. See
[ADR 29 §dispatch substrate](../../docs/designs/29-conductor-orchestration.md).

Run the surface and relay its output:

```bash
conductor/workflows.sh $ARGUMENTS
```

- **no args / `board`** — list program + issue runs (live/dead, pid, status),
  agent worktrees, and recent chat/Workflow runs.
- **`tail <id>`** — follow that run's `session.log` / `stream.jsonl`.
- **`kill <id>`** — SIGTERM a program/issue run.
- **`pause <id>` / `resume <id>`** — hold / release new dispatch for a conductor
  or issue run (already-running phases continue).

Control (pause/kill) applies to **shell-owned** runs only (conductor + issue).
Chat/Workflow-tool runs are harness-managed — this surface can list and tail
them, but stop them with `TaskStop`, not here. Then briefly summarize what is
running and flag anything `dead` but not `done` (a crashed run worth resuming).
