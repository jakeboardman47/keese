<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Contributing

Welcome. This doc is the onboarding path for new contributors (human or agent).
Start with **[How this project is organized](#how-this-project-is-organized)** — the
repo is structured deliberately and a lot of the conventions only make sense after
you see the shape.

## Contents

- [How this project is organized](#how-this-project-is-organized)
- [Development environment](#development-environment)
- [Commits](#commits)
- [Copyright and SPDX headers](#copyright-and-spdx-headers)
- [Secrets](#secrets)
- [Doc hygiene](#doc-hygiene)
- [Phase / rubric / iteration loop](#phase--rubric--iteration-loop)
- [Multi-agent worktree workflow](#multi-agent-worktree-workflow)
- [Model tier selection](#model-tier-selection)
- [Testing](#testing)

## How this project is organized

The repo has **two brain halves**. Keep them mentally separate.

- `docs/` — knowledge **about the project** (what we're building and why).
- `.claude/` — knowledge **about how to work on the project** (rules, skills, agents,
  guardrails). Claude Code loads these; humans benefit from reading them too.

### `docs/` — five trees, one question each

| Tree | Question | Lifecycle | Who reads it |
|---|---|---|---|
| [designs/](docs/designs/) | **WHY** | Stable. Changes require a design review. | Architects, humans |
| [specs/](docs/specs/) | **WHAT** (contract) | Concrete enough that test harnesses parse it. | Implementers, test harness |
| [plans/](docs/plans/) | **HOW** (phased) | Execution-oriented. One file per phase. | Plan scorer, implementers |
| [features/](docs/features/) | **WHAT IS BUILT** | One file per implemented capability; evolves as plans iterate. | Users, support, implementers |
| [references/](docs/references/) | **HOW** (steady-state) | Living cookbooks. Updated as tools evolve. | Any role, humans |

The five trees form a pipeline:

```
designs/   →  specs/     →  plans/       →  code     →  features/
(invariants)  (contracts)   (work items)    (impl)      (implemented behavior on main)
                            ↘ references/ (cookbooks consumed throughout)
```

Every doc has YAML frontmatter and is kept under 200 lines. See
[docs/README.md](docs/README.md) for the full map.

### `.claude/`

- [rules/](.claude/rules/) — non-negotiable conventions (always loaded).
- [skills/](.claude/skills/) — on-demand how-to modules.
- [agents/](.claude/agents/) — pre-configured subagents with model tiers.
- [commands/](.claude/commands/) — slash commands.
- [hooks/](.claude/hooks/) — pre/post tool hooks (block secrets, enforce commit style, verify headers).

## Development environment

```sh
direnv allow                 # one-time, on first clone
nix develop                  # or rely on direnv's auto-activation
pre-commit install --install-hooks
pre-commit install --hook-type commit-msg
```

See [docs/references/nix-dev-env.md](docs/references/nix-dev-env.md).

## Commits

Conventional Commits. Format: `type(scope): subject`.

- Types: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `ci`, `build`, `perf`, `style`.
- Subject ≤ 72 chars, imperative, lowercase, no trailing period.
- One logical change per commit.

Enforced by [`.claude/hooks/validate-conventional-commit.sh`](.claude/hooks/validate-conventional-commit.sh)
and pre-commit/CI. See [docs/references/conventional-commits.md](docs/references/conventional-commits.md).

## Copyright and SPDX headers

Every source file starts with:

```
// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai
```

Equivalent comment syntax for other languages. `addlicense` pre-commit + the
[`enforce-spdx-header.sh`](.claude/hooks/enforce-spdx-header.sh) Claude hook enforce this.

## Secrets

- Never commit secrets.
- `.env.local` is gitignored; use `.env.local.example` for placeholders.
- `detect-secrets` + `gitleaks` run pre-commit.
- See [`.claude/rules/02-security.md`](.claude/rules/02-security.md).

## Doc hygiene

See [docs/references/documentation-system.md](docs/references/documentation-system.md).

Highlights:

- 200 lines per doc max.
- Every doc has SPDX header + YAML frontmatter.
- Link aggressively; never duplicate explanation across docs.
- `last_verified: YYYY-MM-DD` updated whenever the doc is re-read against code.

## Phase / rubric / iteration loop

1. Author a phase doc in `docs/plans/phase-NN-*.md`.
2. Run `/score-plan docs/plans/phase-NN-*.md` against
   [docs/plans/rubric.md](docs/plans/rubric.md).
3. Iterate up to 3 times. Target ≥ 90/100 before `status: in-progress`.
4. If still below SHIP at iteration 3: split or rescope.

See the [`plan-management` skill](.claude/skills/plan-management.md).

## Multi-agent worktree workflow

- Dispatch an agent into an isolated worktree:
  ```sh
  scripts/agent-dispatch.sh phase-04 implementer
  ```
- Merge back when the agent reports complete:
  ```sh
  scripts/worktree-merge.sh agent/phase-04-implementer
  ```

See [docs/references/agent-dispatch.md](docs/references/agent-dispatch.md) and
[docs/references/git-worktree-merging.md](docs/references/git-worktree-merging.md).

## Model tier selection

See [docs/references/model-selection.md](docs/references/model-selection.md).

Quick heuristics:

- **Haiku** — search, summarization, log triage.
- **Sonnet** — implementation, tests, doc authoring.
- **Opus** — architecture, security review, complex refactors.

## Testing

See [`.claude/rules/06-testing.md`](.claude/rules/06-testing.md).

Core rules:

- Race detector (or equivalent) enabled in CI.
- Never `sleep` in tests — use `Eventually` / polling.
- Table-driven tests; no shared mutable globals.
- Don't mock types you don't own.
