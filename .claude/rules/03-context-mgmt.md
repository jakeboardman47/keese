---
description: Context window management and Claude token hygiene
paths:
  - "**/*"
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) {{YEAR}} {{ORG_NAME}} -->

# Context management (always loaded)

Keep Claude context lean. Token cost and cache hit rate both matter.

## Loading discipline

1. `CLAUDE.md` is a **task → doc → skill** index. Never paste design or spec content
   into it. Link to it.
2. On any task, read only the row in the CLAUDE.md task table that matches. Do **not**
   glob `docs/**` or `.claude/skills/**`.
3. Read docs lazily: start with the "load first" doc, expand to "then if needed" only
   when the first doc doesn't answer the question.
4. Never load all phases, all designs, or all specs at once. They are independently
   loadable by design.

## Prompt caching

- Do not mutate `CLAUDE.md`, `.claude/rules/*`, or `MEMORY.md` mid-task. Those three are
  the cache prefix on every turn; touching them breaks cache for the rest of the session.
- Prefer consistent absolute file paths when re-reading. Switching between
  `/path/to/foo.go` and `../foo.go` breaks the content-address cache.
- Keep imports (`@path`) in `CLAUDE.md` in a stable order. Reordering breaks cache.

## Subagent delegation

Use a subagent when:

- A task requires reading > 5 files you do not know in advance.
- A tool output will be > ~1k tokens (long command dumps, log tails, test output).
- Authoring repetitive boilerplate (manifest stamps, table-driven test stubs).

When delegating:

- Pick the cheapest model that can do the job. Haiku for search / summarization,
  Sonnet for implementation, Opus for architecture and security review. See
  `.claude/agents/` for pre-configured agents.
- Pass exact file paths, line numbers, and acceptance criteria — never "based on your
  findings, fix it."
- Ask for a short written report (< 200 words) unless more is required.

## Tool output discipline

- Prefer narrow queries (exact flags, field selectors, jsonpath) over full dumps when you
  only need a field.
- Pipe long dumps through `scripts/lib/log.sh` helpers (`log::step`) to land them in
  `.plan-logs/`. Reference by path; do not paste.
- Never dump >100 lines into chat unless the user asked for it.

## MEMORY.md

- Use for cross-session context: decisions made, gotchas hit, deviations from plan.
- One line per entry: `- [Title](docs/.../detail.md) — ≤120-char hook.`
- Update at the end of a sub-phase, not mid-execution.
- If an entry would exceed a couple of lines, write into the target doc and link here.

## Task vs. memory vs. plan

- **Task list (TodoWrite)**: ephemeral; current session only.
- **Plan doc (`docs/plans/phase-*.md`)**: structured, reviewable, scored against the rubric.
- **MEMORY.md**: cross-session pointer index.
- **Auto-memory (`~/.claude/...`)**: user/feedback/project/reference facts.

Use the right layer. Do not put transient task state in MEMORY.md.

## Docs that are too long

A doc over 200 lines likely answers more than one question. Split it:

- Keep one responsibility per file.
- Add the new file to the appropriate `docs/**/README.md` index.
- Update any CLAUDE.md row that pointed at the old file.
