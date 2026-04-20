# keese — Claude Index

CLAUDE.md is a **task → doc → skill** map. It is not a content dump.
Do not paste design, spec, or implementation text here. Link to it.

Goal: keep this file small and stable so prompt caching stays warm across sessions.

## Always loaded

- This file (`CLAUDE.md`)
- `.claude/rules/*.md` — non-negotiable conventions, security, context hygiene
- `MEMORY.md` — running log of decisions and gotchas

## Project quick reference

| Area | Path |
|---|---|
| Project overview | [README.md](README.md) |
| Plans index | [docs/plans/README.md](docs/plans/README.md) |
| Scoring rubric | [docs/plans/rubric.md](docs/plans/rubric.md) |
| Designs index | [docs/designs/README.md](docs/designs/README.md) |
| Specs index | [docs/specs/README.md](docs/specs/README.md) |
| Features index | [docs/features/README.md](docs/features/README.md) |
| References index | [docs/references/README.md](docs/references/README.md) |
| Claude rules (always) | [.claude/rules/](.claude/rules/) |
| Claude skills (on demand) | [.claude/skills/](.claude/skills/) |
| Claude agents | [.claude/agents/](.claude/agents/) |
| Memory index | [MEMORY.md](MEMORY.md) |
| Secrets template | [.env.local.example](.env.local.example) |
| Dev shell | [flake.nix](flake.nix) |

## Technology stack

<!-- Edit this table to reflect the project's chosen stack. -->

| Layer | Technology |
|---|---|
| Language | _add_ |
| Packaging | OCI images, signed with Sigstore cosign (keyless OIDC) |
| Observability | OpenTelemetry (metrics, traces, logs) |
| Dev env | Nix flake |
| Pre-commit | conventional-commits, detect-secrets, gitleaks, shellcheck, markdownlint |

## Task → docs → skills map

<!-- Add rows as the project grows. Below are generic starter rows. -->

| Task area | Load first | Then if needed | Skill |
|---|---|---|---|
| Write or modify a design doc | `docs/designs/README.md` | specific `docs/designs/NN-*.md` | `doc-authoring` |
| Write or modify a spec | `docs/specs/README.md` | specific spec + related design | `doc-authoring` |
| Document an implemented feature | `docs/features/README.md` | related spec + source files | `doc-authoring` |
| Author or update a diagram | `docs/references/diagram-authoring.md` | source files it depicts | `diagram-authoring` |
| Create or update a plan phase | `docs/plans/README.md` + `docs/plans/rubric.md` | phase doc | `plan-management` |
| Edit Makefile or recipe scripts | `.claude/skills/makefile-authoring.md` | `scripts/lib/log.sh`, `scripts/lib/signals.sh` | `makefile-authoring` |
| Multi-agent worktree | `docs/references/agent-dispatch.md` | `scripts/agent-dispatch.sh` | `agent-dispatch` |
| Auto-merge subagent work | `docs/references/git-worktree-merging.md` | `scripts/worktree-merge.sh` | `worktree-merge` |
| Commit or push | `.claude/rules/01-conventions.md` | `docs/references/conventional-commits.md` | (hook-enforced) |
| Write or run tests | `.claude/rules/06-testing.md` | test harness docs | `test-engineer` (agent) |

## Loading strategy

1. **Always**: this file, `.claude/rules/*`, `MEMORY.md`.
2. **Per task**: only the row matching the task — the *load first* doc; fetch *then if needed*
   on demand; activate the *skill* only when doing real work on that area.
3. **Never auto-load**: all designs, all plans, all specs, or large source trees.

## Conventions

- **Copyright**: every source file has `// SPDX-License-Identifier: Apache-2.0` and
  `// Copyright (c) 2026 keese-ai` (or the equivalent comment syntax).
- **Doc headers**: every doc has the SPDX/copyright HTML-comment pair plus YAML frontmatter
  (`scope`, `category`, `depends`, `related_skills`, `status`, `last_verified`).
- **Commits**: Conventional Commits enforced via pre-commit (`type(scope): subject`).
  Types: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `ci`, `build`, `perf`, `style`.
  Scopes align with sub-phase IDs or top-level directories.
- **No secrets in git — ever.** `.env.local` is gitignored; use `.env.local.example`.
- **Pre-release**: we do not narrate change history in docs. `git log` is the record.
  Breaking changes are acceptable before the first release.
- **Multi-agent**: use git worktrees via `scripts/agent-dispatch.sh`; automated merge via
  `scripts/worktree-merge.sh`.

## Refinement iterations

Every plan, spec, or implementation target passes through **up to three** review passes:

1. **Correctness & security** — does it do the right thing safely?
2. **Performance & quality** — is it efficient and idiomatic?
3. **Operational readiness** — can it be deployed, observed, and rolled back?

Score against the relevant rubric (`docs/plans/rubric.md`). Target >= 90/100 before landing.

## Claude-specific context hygiene

- Prefer **reading one doc** cited in the task table over globbing. See
  `.claude/rules/03-context-mgmt.md`.
- Delegate bulk research and large tool output to subagents with appropriate model tier
  (Haiku for search, Sonnet for implementation, Opus for architecture).
- Long command outputs go to `.plan-logs/` via helper scripts; reference by path.
- Do not mutate this file mid-task; cache warmth depends on its stability.
