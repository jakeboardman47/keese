<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: security
depends:
  - docs/references/branch-protection.md
related_skills: []
status: current
last_verified: 2026-05-07
---

# OpenSSF Scorecard — deferred checks

Records checks we know are failing or partially passing, with rationale and
target date. Review at the start of each milestone.

## Deferred

| Check | Current state | Rationale | Target date |
|---|---|---|---|
| **Branch-Protection** | Unknown — not enforced in code | Scorecard reads branch protection rules from the GitHub API. Required rules are documented in `docs/references/branch-protection.md`; a repository admin must configure them via the GitHub UI or API. Cannot be enforced by a file in this repo. | 2026-05-14 (before v0.1.0-alpha.1 tag) |
| **Fuzzing** | Not wired | Go fuzzing targets require domain-specific corpus design. Highest-value targets are the CEL compilation path in `internal/controller/authz/guardrail_envoy.go` and the ReBAC tuple builder in `internal/rebac/`. Deferred until P3 controller work stabilises the API surface. | 2026-08-01 (v0.2.0 milestone) |
| **CII-Best-Practices** | Not enrolled | Requires manual enrollment at bestpractices.coreinfrastructure.org + passing ≥ 90% of criteria. Enrollment blocked on having a stable enough API surface that the "change control" criterion can be met. Add the badge link to README.md at enrollment. | 2026-08-01 (v0.2.0 milestone) |
| **Maintained** | Partial — low commit frequency during gate phase | Scorecard scores commit activity. During the pre-gate design phase, commit frequency is intentionally low (design docs, not code). Score will improve automatically as controller implementation ramps up post-gate. No action needed. | Resolves automatically post-gate open |
| **Contributors** | Single-contributor during bootstrap | Scorecard rewards multiple distinct contributors. Expected to improve as the project becomes public. No action needed now. | Post-public |
| **Webhooks** | Not applicable | No inbound GitHub webhooks configured on this repo. Scorecard flags configured webhooks without secrets as a risk; having none is neutral. | N/A |

## Clean checks (expected to pass)

The following checks are expected to pass after the TD-P3-10 hardening commit:

| Check | Evidence |
|---|---|
| **Binary-Artifacts** | No compiled binaries committed to the repo; `make` outputs are gitignored. |
| **CI-Tests** | `test.yaml` runs on every PR and main push. |
| **Code-Review** | PR-required by branch protection. |
| **Dangerous-Workflow** | No `pull_request_target` with head-ref checkout. No `if: true` guards that bypass secrets. |
| **Dependency-Update-Tool** | Actions pinned to SHA; Go deps managed via `go.mod` + `govulncheck` pre-commit. Dependabot not yet enabled — see follow-on note below. |
| **License** | `LICENSE` (Apache-2.0) at repo root; every source file has SPDX header. |
| **Packaging** | `image.yaml` builds, signs, and attests OCI images on every tag push. |
| **Pinned-Dependencies** | All `.github/workflows/*.yaml` action references use full commit SHA. |
| **SAST** | `codeql.yaml` runs CodeQL Go analysis on PR, main push, and weekly. `golangci-lint-action` also runs in `lint.yaml`. |
| **Security-Policy** | `SECURITY.md` present at repo root with disclosure email + severity SLA table. |
| **Signed-Releases** | `image.yaml` and `bundle.yaml` sign OCI images via cosign keyless OIDC on every tag. SBOMs attested via syft + cosign. |
| **Token-Permissions** | Every workflow has a top-level `permissions:` block. Every job has its own `permissions:` block narrowed to actual needs. |
| **Vulnerabilities** | `govulncheck ./...` runs in pre-commit. `trivy` and `govulncheck` target < 30-day patch SLA per SECURITY.md. |

## Follow-on: Dependabot

Dependabot for GitHub Actions is the standard mechanism Scorecard uses to verify
`Dependency-Update-Tool` for actions. Enable by adding `.github/dependabot.yml`
with `package-ecosystem: github-actions`. Deferred because SHA-pinning was just
introduced; adding Dependabot too early would immediately propose unpinning back
to tags.

Target: enable Dependabot with PR auto-approve for patch-version bumps once the
SHA pins have been stable for one week (by 2026-05-14).
