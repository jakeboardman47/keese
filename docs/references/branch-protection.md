<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: security
depends:
  - docs/references/scorecard-deferrals.md
related_skills: []
status: current
last_verified: 2026-05-07
---

# Branch protection requirements — main

GitHub's Scorecard Branch-Protection check reads branch rules from the API.
This doc records the expected configuration so it is reproducible and auditable.

## Required settings on `main`

| Setting | Value | Rationale |
|---|---|---|
| Require a pull request before merging | true | No direct pushes to main except automated (release-please, bot). |
| Required approving reviews | 1 | At least one human approval for every PR. |
| Dismiss stale reviews on new commit | true | Prevents rubber-stamping stale PRs. |
| Require status checks to pass | true | See required checks list below. |
| Require branches to be up to date before merging | true | Prevents races on main. |
| Require conversation resolution | true | All review threads resolved before merge. |
| Restrict who can push to main | Admins + CI bot only | Enforces the OLM + CI publish flow. |
| Allow force pushes | false | History is immutable on main. |
| Allow deletions | false | Branch cannot be deleted. |
| Require signed commits | true (recommended) | Gate-open commit requires GPG/SSH signature (verify-gate-commit.yaml). |

## Required status checks

These checks must pass before a PR can merge to `main`:

- `Lint / pre-commit-all`
- `Lint / golangci`
- `Lint / kubeconform`
- `Lint / pluto`
- `Test / unit (1.24)`
- `Test / integration (1.24, 1.29.x)`
- `Test / integration (1.24, 1.30.x)`
- `Test / integration (1.24, 1.31.x)`
- `Design Gate / check`
- `Conventional Commits / commitlint`
- `CodeQL / analyze (go)`

## Enforcement

Branch protection is configured via the GitHub UI or GitHub REST API. It cannot
be expressed as a checked-in file in this repo (the API enforces it out-of-band).

The `verify-gate-commit.yaml` workflow enforces that any gate-open commit is
author-verified against the `keese-ai/architects` team membership AND is
cryptographically signed.

## See also

- [docs/references/scorecard-deferrals.md](scorecard-deferrals.md) — checks deferred and why.
- `.github/workflows/scorecard.yaml` — OpenSSF Scorecard runs weekly and surfaces any drift.
