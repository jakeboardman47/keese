<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) {{YEAR}} {{ORG_NAME}} -->

---
scope: reference
category: reference
depends: [docs/references/agent-dispatch.md]
related_skills: [agent-dispatch]
status: current
last_verified: {{LAST_VERIFIED}}
---

# Model Selection for Subagents

Pick the cheapest model that reliably does the job. Defaults encoded in
`.claude/agents/*.md` frontmatter; overrides via env.

## Tier Map

| Model             | Use for                                                                 |
|-------------------|-------------------------------------------------------------------------|
| Claude Opus 4.7   | Architectural reasoning, security review, complex refactors, plan critique |
| Claude Sonnet 4.6 | Implementation, tests, everyday code production, doc authoring          |
| Claude Haiku 4.5  | Search, summarization, log triage, simple format cleanup                |

## Routing Examples

- "Draft ADR for component X selection" → **Opus** (architect).
- "Add validation + unit tests for a new field" → **Sonnet** (implementer).
- "Find every call to `foo.DoSomething` in the repo" → **Haiku** (explorer).
- "Audit the webhook service account" → **Opus** (security-reviewer).
- "Parse this test failure stack and name the likely cause" → **Haiku** (debugger).
- "Refactor a module into a state machine across 4 files" → **Opus** (architect) drafts plan, **Sonnet** (implementer) executes.

## Per-Agent Configuration

Set in frontmatter of `.claude/agents/<name>.md`:

```markdown
---
name: implementer
description: Writes code and tests against a phase plan.
model: sonnet
allowed-tools: [Read, Write, Edit, Bash, Grep, Glob]
---
```

Valid `model:` values: `haiku`, `sonnet`, `opus`. No explicit version — the
harness resolves to the current 4.x stable for each tier.

## Global Default

In `.claude/settings.json`:

```json
{
  "env": {
    "CLAUDE_CODE_SUBAGENT_MODEL": "sonnet"
  }
}
```

Agents without `model:` frontmatter fall back to this value. For a personal
override without editing the repo, put the same key in `~/.claude/settings.json`.

## When to Escalate Mid-Task

If a Sonnet implementer hits a design ambiguity, it should **stop and write a
question** to `.plan-logs/<phase>/QUESTION.md` rather than guess. The
orchestrator escalates to Opus (architect) for the answer, then re-dispatches
Sonnet.

Never let Haiku write code that ends up in `main`. It's fine for exploration
output consumed by a higher tier.

## Related

- Agents: `.claude/agents/`.
- Settings: `.claude/settings.json`.
- Dispatch: [agent-dispatch.md](agent-dispatch.md).
