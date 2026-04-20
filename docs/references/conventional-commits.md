<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) {{YEAR}} {{ORG_NAME}} -->

---
scope: reference
category: reference
depends: []
related_skills: []
status: current
last_verified: {{LAST_VERIFIED}}
---

# Conventional Commits

Every commit on every branch follows [Conventional Commits 1.0.0]. Enforcement
is machine-checked pre-commit, pre-tool, and on PR.

## Format

```
type(scope): subject

[body]

[footer]
```

## Allowed Types

| Type     | Use for                                                  |
|----------|----------------------------------------------------------|
| feat     | User-visible feature                                     |
| fix      | Bug fix                                                  |
| docs     | Documentation only                                       |
| chore    | Housekeeping (deps, tooling) with no code effect         |
| refactor | Code change that neither fixes a bug nor adds a feature  |
| test     | Add or correct tests                                     |
| ci       | CI config / workflow changes                             |
| build    | Build system, Makefile, Dockerfile                       |
| perf     | Performance improvement                                  |
| style    | Formatting, whitespace, no logic change                  |

Any other type fails validation.

## Scopes

Align the scope with exactly one of:

- A top-level directory (e.g. `api`, `cli`, `scripts`).
- A module or component name.
- A phase ID from `docs/plans/`: `phase-02`, `phase-03a`.

Pick the most specific.

## Subject

- ≤ **72 characters**.
- Imperative mood (`add`, not `added`/`adds`).
- Lowercase first letter.
- No trailing period.

## Breaking Changes

Signal two ways, both required:

1. `!` before the colon: `feat(api)!: rename field X to Y`
2. Footer line: `BREAKING CHANGE: X renamed to Y; bump v1alpha1 -> v1alpha2`

## Enforcement

1. **pre-commit** — `.pre-commit-config.yaml` runs commitizen:
   ```yaml
   - repo: https://github.com/commitizen-tools/commitizen
     hooks: [{ id: commitizen, stages: [commit-msg] }]
   ```
2. **Claude PreToolUse hook** — `.claude/hooks/validate-conventional-commit.sh`
   blocks `git commit` invocations from agents when the message is malformed.
3. **GitHub Action** — `.github/workflows/commitlint.yaml` runs on every PR.

## Good Examples

```
feat(api): add status.phase transition to Provisioning
fix(cli): reject empty --config flag
docs(references): add deployment how-to
refactor(phase-02): extract helper
chore(deps): bump lib X to v1.2.3
ci: sign image in release workflow
feat(api)!: replace spec.mode with spec.engine
```

## Bad Examples

```
Fixed the thing.                         # no type, capital, period
feat: added new feature                  # past tense, missing scope
Feat(API): add status                    # capitalized type, capital scope
update readme                            # not a valid type
```

## Related

- Hooks: [`../../.claude/hooks/validate-conventional-commit.sh`](../../.claude/hooks/validate-conventional-commit.sh).
- Config: `.pre-commit-config.yaml`, `.commitlintrc.json`.
