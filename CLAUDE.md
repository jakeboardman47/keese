<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# keese — Claude Index

CLAUDE.md is a **task → doc → skill** map. It is not a content dump.
Do not paste design, spec, or implementation text here. Link to it.

Goal: keep this file small and stable so prompt caching stays warm across sessions.

## Always loaded

- This file (`CLAUDE.md`)
- `.claude/rules/*.md` — non-negotiable conventions (01 · 02 · 03 · 04 · 05 · 06 signal-handling · 06 testing)
- `MEMORY.md` — running log of decisions and gotchas

## Project quick reference

| Area | Path |
|---|---|
| Project overview | [README.md](README.md) |
| Plans index + gate status | [docs/plans/README.md](docs/plans/README.md) |
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

| Layer | Technology |
|---|---|
| Language | Go 1.24+ |
| Framework | Operator SDK (go/v4 plugin), controller-runtime |
| Packaging | OCI images, OLM bundle, Sigstore cosign (keyless OIDC) |
| Manifests | Kustomize (in-cluster), OpenTofu (cloud infra), Helmfile (dev deps) |
| Multi-tenancy | Capsule (namespaces + policies), vcluster (optional hard isolation) |
| ReBAC | OpenFGA |
| Egress | Envoy Gateway + Envoy AI Gateway (MCPRoute + BackendSecurityPolicy) |
| Messaging | NATS JetStream via NACK |
| Workflow | Argo Workflows (keese Workflow wraps) |
| Memory | Pluggable `Memory` CRD: SQLite/Redis/Qdrant/pgvector/Neo4j/Mem0/Zep |
| Secrets | OpenBao + ExternalSecrets Operator |
| Observability | OpenTelemetry → Elastic APM (traces) + ECK (logs, metrics) |
| Local dev | ctlptl + kind + Tilt + Helmfile |
| Pre-commit | Conventional Commits, detect-secrets, gitleaks, shellcheck, markdownlint |
| CI/CD | GitHub Actions + release-please + OpenSSF Scorecard |
| Primary IDE | GoLand (native ACP); VSCode secondary |

## Task → docs → skills map

| Task area | Load first | Then if needed | Skill / Agent |
|---|---|---|---|
| Write or modify a design doc | `docs/designs/README.md` | `docs/designs/NN-*.md` | `doc-authoring` · agent `architect` |
| Write or modify a spec | `docs/specs/README.md` | related spec + owning design | `doc-authoring` · agent `architect` |
| Document an implemented feature | `docs/features/README.md` | related spec + source files | `doc-authoring` |
| Author or update a diagram | `docs/references/diagram-authoring.md` | depicted source files | `diagram-authoring` |
| Create or update a plan phase | `docs/plans/README.md` + `docs/plans/rubric.md` | the phase doc | `plan-management` |
| Edit a Makefile or recipe script | `.claude/skills/makefile-authoring.md` | `scripts/lib/{log,signals}.sh` | `makefile-authoring` |
| Multi-agent worktree | `docs/references/agent-dispatch.md` | `scripts/agent-dispatch.sh` | `agent-dispatch` |
| Auto-merge subagent work | `docs/references/git-worktree-merging.md` | `scripts/worktree-merge.sh` | `worktree-merge` |
| Create a new CRD | `docs/references/crd-design-checklist.md` | `docs/designs/20-api-group-layout.md` + owning design | `crd-authoring` · agent `crd-author` · `/gen-crd` |
| Implement a reconciler | `docs/references/envtest-kuttl-harness.md` | owning spec in `docs/specs/` (e.g. `keese.ai-v1alpha1-<kind>.md`, `authz.keese.ai-v1alpha1.md`, `policy.keese.ai-v1alpha1.md`) | `controller-authoring` · agent `controller-author` |
| Edit an admission webhook | `.claude/rules/04-kubernetes.md` | owning spec | `controller-authoring` |
| Author/update OLM bundle | `docs/references/olm-bundle-authoring.md` | `docs/designs/14a-olm-channels-upgrades.md` + `14b-olm-dependencies.md` | agent `olm-author` · `/validate-bundle` |
| Bootstrap local kind + infra | `docs/references/tilt-local-loop.md` | `dev/bootstrap/README.md` | agent `infra-bootstrap` |
| End-to-end smoke (kind) | `docs/references/e2e-smoke.md` | `scripts/dev/e2e-smoke.sh` | agent `test-engineer` |
| Add/revise a guardrail binding | `docs/designs/06-guardrailbinding.md` | `docs/specs/authz.keese.ai-v1alpha1-guardrail.md` | agent `guardrail-author` |
| Change OpenFGA auth model | `docs/designs/04a-openfga-authz-model.md` | `docs/specs/egress-authz-protocol.md` | agent `rebac-modeler` |
| Add an AgentRuntime provider | `docs/designs/07-agent-runtime-spi.md` | `docs/specs/agent-runtime-spi.md` | `doc-authoring` then `controller-authoring` |
| Edit Envoy AI Gateway config | `docs/designs/05a-envoy-ai-gateway-topology.md` | `.claude/rules/05-security-zero-trust.md` | agent `infra-bootstrap` |
| Add an OTEL / logs pipeline | `docs/designs/10a-otel-topology.md` | `dev/bootstrap/otel-collector/README.md` | agent `infra-bootstrap` |
| Write a goose recipe / extension | `docs/designs/08a-goose-headless-modes.md` + `08c-goose-subagents-limits.md` | `dev/samples/recipes/` | `doc-authoring` |
| Cloud deploy (OpenTofu) | `docs/references/opentofu-cloud-deployment.md` | `deploy/opentofu/README.md` | agent `infra-bootstrap` |
| IDE setup (debug attach, ACP) | `docs/references/ide-and-debugging.md` | `dev/ide/{goland,vscode}/` | — |
| Open / close the design gate | `docs/plans/README.md` | `scripts/check-design-gate.sh` | `plan-management` · agent `architect` |
| Score a plan / design / spec | `docs/plans/rubric.md` | target doc | `plan-management` · agent `plan-scorer` |
| Commit or push | `.claude/rules/01-conventions.md` | `docs/references/conventional-commits.md` | (hook-enforced) |
| Write or run tests | `.claude/rules/06-testing.md` | test harness refs | agent `test-engineer` |

## Loading strategy

1. **Always**: this file, `.claude/rules/*`, `MEMORY.md`.
2. **Per task**: only the row matching the task — the *load first* doc; fetch *then if needed*
   on demand; activate the *skill* or dispatch the *agent* only when doing real work on that area.
3. **Never auto-load**: all designs, all plans, all specs, or large source trees.

## Conventions

- **Copyright**: every source file has `// SPDX-License-Identifier: Apache-2.0` and
  `// Copyright (c) 2026 keese-ai` (equivalent comment syntax for other languages).
- **Doc headers**: every doc has the SPDX/copyright HTML-comment pair plus YAML frontmatter
  (`scope`, `category`, `depends`, `related_skills`, `status`, `last_verified`).
- **Commits**: Conventional Commits enforced via pre-commit (`type(scope): subject`).
  Types: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `ci`, `build`, `perf`, `style`, `release`.
  Scopes align with sub-phase IDs or top-level directories (`api`, `controller`, `bundle`,
  `dev`, `rebac`, `guardrail`, `repo`, `readme`, `lint`).
- **No secrets in git — ever.** `.env.local` is gitignored; use `.env.local.example`.
- **Designs before specs.** No spec reaches `status: current` before its owning design
  does. Enforced by `scripts/check-design-gate.sh` (P3/P8).
- **Design gate before controller code.** No non-stub body in `internal/controller/` or
  `api/` until all 62 designs + 13 specs score ≥ 90 and the gate opens.
- **Server-Side Apply** with `fieldOwner = keese-<kind>-controller` for every
  controller write (rule 04.7).
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
- Delegate bulk research and large tool output to subagents with the right model tier:
  - **Opus** for architecture/strategy (`architect`, `rebac-modeler`).
  - **Sonnet** for implementation (`implementer`, `crd-author`, `controller-author`,
    `olm-author`, `infra-bootstrap`, `guardrail-author`, `test-engineer`, `plan-scorer`,
    `security-reviewer`).
  - **Haiku** for narrow lookup (`explorer`, `debugger` investigations).
- Long command outputs go to `.plan-logs/` via helper scripts; reference by path.
- Do not mutate this file mid-task; cache warmth depends on its stability.
