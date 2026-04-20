<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) {{YEAR}} {{ORG_NAME}} -->

# claude-project-template

A reusable project skeleton for repos built with Claude Code. Captures the
non-domain-specific conventions, docs-system, multi-agent worktree workflow, and
pre-commit scaffolding so you can start a new project without re-deriving them.

## What's in here

```
.claude/
  rules/        always-loaded non-negotiable conventions
  skills/       on-demand how-to modules
  agents/       pre-configured subagents (explorer, implementer, architect, …)
  commands/     slash commands (/dispatch, /score-plan, /merge-worktree)
  hooks/        pre/post tool hooks (block secrets, enforce commit style, SPDX)
  settings.json permission allowlist + hook wiring
docs/
  designs/      WHY — architecture decisions
  specs/        WHAT — contracts testable by harnesses
  plans/        HOW (phased) + rubric + flake log
  features/     WHAT IS BUILT — one doc per user-visible capability
  references/   HOW (steady-state) — cookbooks
scripts/
  lib/          log.sh, env.sh, paths.sh, signals.sh
  agent-dispatch.sh   spawn a subagent into a sibling git worktree
  worktree-merge.sh   merge a finished worktree back to main
  check-diagram-freshness.sh
.github/
  dependabot.yaml
  workflows/    commitlint, docs (mkdocs), scorecard
.pre-commit-config.yaml
.commitlintrc.json
.editorconfig .markdownlint.json .secrets.baseline
.gitignore .gitattributes .envrc .env.local.example
flake.nix lychee.toml
CLAUDE.md MEMORY.md CONTRIBUTING.md SECURITY.md CODEOWNERS LICENSE
```

## What's deliberately NOT in here

No language toolchain, no framework-specific skills, no build/test targets. Those
belong in the project that uses this template. What's here is the invariant scaffolding:

- **Docs system** (5 trees × frontmatter × 200-line cap × cross-link discipline).
- **Plan rubric + iteration loop** (≤ 3 passes, target ≥ 90/100).
- **Multi-agent worktree flow** (dispatch / merge / protected paths).
- **Claude context hygiene** (rules 01–06).
- **Security defaults** (secret blocks, conventional commits, SPDX headers).

## Usage

```sh
# 1. Copy the template
cp -r claude-project-template my-new-project
cd my-new-project
rm -rf .git
git init

# 2. Substitute placeholders
#   {{PROJECT_NAME}}      your project name
#   {{ORG_NAME}}          copyright holder (e.g. "Your Name" or "Your Org, Inc.")
#   {{YEAR}}              copyright year
#   {{LAST_VERIFIED}}     today's date (YYYY-MM-DD)
#   {{SECURITY_CONTACT_EMAIL}}   where to report vulnerabilities
#   {{GITHUB_USER_OR_TEAM}}      your GitHub handle or team
#
# Example (bash):
grep -rl '{{PROJECT_NAME}}' . | xargs sed -i '' 's/{{PROJECT_NAME}}/my-new-project/g'
grep -rl '{{ORG_NAME}}' . | xargs sed -i '' 's/{{ORG_NAME}}/Your Name/g'
grep -rl '{{YEAR}}' . | xargs sed -i '' "s/{{YEAR}}/$(date +%Y)/g"
grep -rl '{{LAST_VERIFIED}}' . | xargs sed -i '' "s/{{LAST_VERIFIED}}/$(date +%Y-%m-%d)/g"
# (repeat for the remaining placeholders)

# 3. Seed the dev environment
direnv allow
nix develop
pre-commit install --install-hooks
pre-commit install --hook-type commit-msg

# 4. Start writing
#    - First design doc:   docs/designs/00-why.md
#    - First phase plan:   docs/plans/phase-00-scaffold.md
#    - Refresh CLAUDE.md:  add task routing rows as you go
```

## Conventions (tl;dr)

- **Conventional Commits** — `type(scope): subject`. Enforced pre-commit and by a
  Claude PreToolUse hook.
- **Every source file** carries an SPDX + copyright header; PostToolUse hook blocks
  writes that forget.
- **`.env.local` is gitignored**; `.env.local.example` documents what goes in it.
  `detect-secrets` + `gitleaks` scan pre-commit.
- **Docs ≤ 200 lines**; cross-link instead of duplicating.
- **CLAUDE.md is a task → doc → skill index**, not a content dump. It belongs to the
  prompt cache prefix — don't mutate it mid-task.

## License

Apache-2.0. See [LICENSE](LICENSE).
