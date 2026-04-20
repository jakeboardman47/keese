<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: handoff
depends: [scaffolding-plan.md, rubric.md]
related_skills: [plan-management]
status: current
last_verified: 2026-04-20
---

# Scaffolding — session handoff summary

This doc is the authoritative pointer after the initial scaffolding
session (2026-04-19/20). The repo now contains everything it needs;
no path on the developer's local disk is load-bearing.

## What landed (phases P0–P8)

| Phase | Commit | What shipped |
|---|---|---|
| P0 repo foundation | `90395cd` → `241ec40` (5+1) | Template copy + placeholder substitution + day-one README + `.gitignore`/`.gitattributes` extensions + three-team CODEOWNERS + markdownlint/shellcheck relaxations for template content |
| P1 Claude automation | `23f9d89` | Rules 04/05/06 (k8s, zero-trust, signal-handling) + 6 new agents (crd-author, controller-author, olm-author, infra-bootstrap, rebac-modeler, guardrail-author) + 2 skills (crd-authoring, controller-authoring) + 2 commands (`/gen-crd`, `/validate-bundle`) + updated settings.json + keese-edition CLAUDE.md |
| P2 dev env | `d6d321b` | flake.nix (Go + K8s + OpenTofu + Helmfile) + top-level Makefile with CI-load-bearing target grid + `.env.local.example` + `scripts/guard-kube-context.sh` |
| P3 pre-commit | `47ca56b` | Go (gofumpt/goimports/tidy/golangci-lint/govulncheck) + K8s (controller-gen freshness, kubeconform, pluto, kustomize overlays, CRD sample validation via envtest, ReBAC markers, NetworkPolicy wildcards, SIGTERM handlers) + OLM bundle validate + OpenTofu (fmt/validate/conftest) + yamllint + `.golangci.yml` + `.yamllint.yaml` + 7 new `scripts/check-*.sh` |
| P4 docs skeleton | `ca2434b` | 30 design stubs under `docs/designs/`, 11 spec stubs under `docs/specs/`, 6 new reference docs, updated `docs/plans/README.md` (gate banner) + `docs/plans/flake-log.md` |
| P5 CI/CD | `7a819a0` | 8 new GH Actions workflows (lint, test, e2e, bundle, image, release, design-gate, opentofu) + release-please config |
| P6 operator scaffold | `5dd8d07` | operator-sdk init + create-api for 13 kinds across 8 groups under `operator.keese.ai` v1alpha1 — every `*_types.go` + `*_controller.go` is a `TODO(design-gate)` stub (26 markers) — OLM bundle validate green |
| P7 local infra | `0e73f78` | `dev/kind/ctlptl.yaml` + kind-config, `dev/bootstrap/helmfile.yaml` (14 releases: cert-manager, Capsule, Envoy Gateway, Envoy AI Gateway, OpenFGA, NACK/NATS, ECK, Kyverno, OpenBao, ExternalSecrets, Argo Workflows, Qdrant, OTEL collector, capsule extras) + 15 per-release values files + OpenFGA/NATS/OpenBao seeds + Tiltfile + `config/overlays/dev` + 3 smoke samples + 4 dev scripts |
| P8 design-gate freeze | `2ac6094` | `scripts/check-design-gate.sh` + `verify-gate-commit.yaml` workflow + bats tests |

Commit graph: `git log --oneline` reveals 14 atomic Conventional
Commits from `90395cd` (initial scaffold) to `2ac6094` (design-gate).

## Repo state (snapshot 2026-04-20)

- 415 files · ~25,140 LOC
- `pre-commit run --all-files` — green across all enabled hooks
- `go build ./...` — clean
- `go test -short ./api/... ./internal/...` — 8 controller envtest
  suites pass (after `make envtest-setup`)
- `operator-sdk bundle validate ./bundle --select-optional
  suite=operatorframework` — "All validation tests have completed
  successfully"
- `scripts/check-design-gate.sh` — exits 0 (no violations; 13 stubs
  classified correctly)
- 26 `TODO(design-gate)` markers across `api/` and
  `internal/controller/`
- Design gate: **CLOSED** per `docs/plans/README.md`
  `gate_status: closed`. Controller code may not have non-stub bodies
  until all 33 designs + 11 specs score ≥ 90/100 on the rubric and
  the gate commit opens it.

## Resume instructions (after moving or cloning the repo)

The repo is location-independent. To resume on any machine:

```sh
direnv allow              # loads nix develop
nix develop               # if direnv isn't wired yet
pre-commit install --install-hooks
pre-commit install --hook-type commit-msg
make envtest-setup        # downloads kube-apiserver + etcd binaries
make verify               # fmt + vet + lint + test + bundle-validate
```

If the target machine hasn't run `operator-sdk` before, the scaffold
expects it on PATH. Install via the `go install` path or the
(unverified-in-nixpkgs) overlay sketched in the
[plan file](scaffolding-plan.md) D1/P2 sections.

Local dev stack (post-gate smoke):

```sh
make kind-up              # ctlptl apply
make bootstrap-infra      # helmfile sync 14 releases
make tilt-up              # hot-reload operator via Tilt
```

`scripts/dev/time-bootstrap.sh` fails acceptance if kind-up +
bootstrap-infra exceed 300s on the host.

## What the next agent should do

Per the plan, the next phase after scaffolding is **architect-driven
design authoring** — walk the 33 design stubs in
`docs/designs/` and iterate each to `status: current` with
rubric score ≥ 90. The plan enforces **designs complete before
specs**. Model discipline (D22) assigns these tasks to the
architect (opus) agent.

Recommended authoring order (dependency-free first):

1. `01-tenancy-capsule`, `20-api-group-layout` (foundational)
2. `04a/04b/04c` (OpenFGA model + projected SA + token revocation)
3. `05a/05b/05c` (Envoy AI Gateway topology + credential injection +
   MCP policy)
4. `06-guardrailbinding`, `14a/14b` (OLM)
5. `02-workspace-model`, `03-workflow-argo-delegation`,
   `07-agent-runtime-spi` (core primitives)
6. `08a/08b/08c` (goose integration details)
7. `09-transport-crd`, `10a/10b` (observability + token accounting)
8. `11-secrets-pluggable-vault`, `12-network-isolation`,
   `13-cli-tunnel-wireguard`
9. `15-memory-management`, `16-recipe-distribution`,
   `17-credential-broker`, `18-process-lifecycle`
10. `19-ide-and-debugging`, `21-opentofu-cloud-deployment`,
    `22-workflow-composition-examples`

Once all 33 designs are `current`, the 11 specs under `docs/specs/`
are authored (each depends on 1–3 designs). When the 11 specs reach
`current`, an architect opens the gate with a commit flipping
`gate_status: closed -> open` in
[docs/plans/README.md](README.md). The gate-open commit must be
GPG/SSH-signed by a member of `@keese-ai/architects` — enforced by
[`verify-gate-commit.yaml`](../../.github/workflows/verify-gate-commit.yaml).

## Pointers (load order for a resuming Claude)

1. [`CLAUDE.md`](../../CLAUDE.md) — task → doc → skill routing
2. [`.claude/rules/*.md`](../../.claude/rules/) — always-loaded
3. [`docs/plans/README.md`](README.md) — gate status + phase index
4. [`docs/plans/rubric.md`](rubric.md) — scoring framework
5. [`docs/plans/scaffolding-plan.md`](scaffolding-plan.md) — full
   scaffolding plan (23 key decisions, 9 phases)
6. [`docs/designs/README.md`](../designs/README.md) — design index
7. [`docs/specs/README.md`](../specs/README.md) — spec index

## What is NOT committed (recreate after clone/move)

- `bin/` — tool binaries (operator-sdk, kustomize, controller-gen,
  envtest, setup-envtest, dlv, etc.). Rebuild via `nix develop` +
  `make envtest-setup`, or `go install` for operator-sdk (falls
  outside nixpkgs as of 2026-04).
- `.tilt/`, `tilt_modules/` — Tilt scratch
- `envtest-bin/`, `testbin/`, `k8s/<version>-*/` — setup-envtest
  downloads
- `bundle/tests/scorecard/*.json` — operator scorecard runtime
  artifacts
- `.keese/local-state/` — dev seed-state markers
- `deploy/opentofu/**/.terraform*` + `*.tfstate*` + `*.tfplan` —
  OpenTofu state
- `.env.local` — copy from `.env.local.example` and fill secrets;
  never commit
