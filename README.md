<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# keese

> **Status: pre-alpha · DESIGN GATE: CLOSED** — no operator/controller
> implementation code lands until all 62 designs and 11 specs score ≥ 90/100
> against [docs/plans/rubric.md](docs/plans/rubric.md).

Secure multi-tenant, multi-workspace Kubernetes operator orchestrating
autonomous AI agent workflows on pluggable agent runtimes (goose first).
Namespaces are the tenant boundary. Workspaces span one or more pods
running an agent runtime. Identity is a projected Kubernetes ServiceAccount
token; authorization is ReBAC (OpenFGA); egress flows through Envoy AI
Gateway, which terminates the agent's SA token and injects the correct
upstream credential per target. Composition over new CRDs — Capsule for
tenants, Argo Workflows for workflow execution, Knative for outputs.

## If you are Claude Code

Read in this order, stop when the task is answered:

1. [CLAUDE.md](CLAUDE.md) — task → doc → skill routing table.
2. [.claude/rules/](.claude/rules/) — always-loaded non-negotiables
   (01 conventions · 02 security · 03 context-mgmt · 04 kubernetes · 05
   zero-trust · 06 signal-handling · 06 testing).
3. [docs/plans/README.md](docs/plans/README.md) — phase index + gate
   status.
4. Task-specific doc per the routing table in CLAUDE.md.

Do **not** `rg` the whole tree until routed. Do **not** load all designs
or specs at once.

## Repo map

| Path | Purpose |
|---|---|
| [CLAUDE.md](CLAUDE.md) | Task-to-doc-to-skill index (always in prompt cache) |
| [MEMORY.md](MEMORY.md) | Cross-session decision index |
| [.claude/](.claude/) | Agents, commands, hooks, rules, skills, settings |
| [docs/designs/](docs/designs/) | WHY — architecture decisions (≤ 200 lines each) |
| [docs/specs/](docs/specs/) | WHAT — testable contracts (authored AFTER designs) |
| [docs/plans/](docs/plans/) | HOW (phased) + rubric + flake-log |
| [docs/features/](docs/features/) | WHAT IS BUILT (post-gate) |
| [docs/references/](docs/references/) | HOW (steady-state) cookbooks |
| `api/` | CRD Go types (stub-only until gate opens) |
| `internal/controller/` | Reconcilers (stub-only until gate opens) |
| `config/` | Kustomize base + overlays (`dev/`, `kind/`) |
| `deploy/opentofu/` | Cloud deploy — `aws/` `gcp/` `azure/` |
| `dev/` | Local infra bootstrap (kind, tilt, helmfile) |
| `dev/ide/` | GoLand + VSCode dlv/debug configs |
| `bundle/` | OLM bundle (generated) |
| `scripts/` | Helpers + dispatch/merge + design-gate check |

## Design gate

Authoring specs before their owning designs reach `status: current` is
**blocked** by `scripts/check-design-gate.sh` and a GitHub Action. Writing
non-stub bodies to `api/**/*_types.go` or `internal/controller/**/*.go`
is **blocked** until the gate opens. See
[docs/plans/README.md](docs/plans/README.md#gate-status) for the current
state of the 62 designs and 11 specs.

## Quickstart

```sh
direnv allow            # loads nix develop shell
pre-commit install --install-hooks
pre-commit install --hook-type commit-msg
make help               # target catalog
```

Local Kubernetes (after phase P7 lands):

```sh
make kind-up            # ctlptl-managed kind cluster
make bootstrap-infra    # helmfile sync dev deps (cert-manager, Capsule,
                        #   Envoy AI Gateway, OpenFGA, NACK/NATS, ECK,
                        #   OpenBao, ExternalSecrets, Argo, Qdrant,
                        #   Kyverno, OTEL)
make tilt-up            # hot-reload the operator
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and
[.claude/rules/01-conventions.md](.claude/rules/01-conventions.md). Commits
use Conventional Commits (hook-enforced). Every source file carries an
SPDX `Apache-2.0` header. Rubric in
[docs/plans/rubric.md](docs/plans/rubric.md).

## Security posture (5 invariants)

- Agent runtime pods never see Kubernetes API kubeconfigs or upstream
  LLM/MCP credentials. Identity is a projected SA token with audience
  `keese-egress-<tenant>`; TTL 10m.
- All network egress flows through Envoy AI Gateway, fail-closed. No
  wildcard NetworkPolicies anywhere.
- Authorization is ReBAC (OpenFGA). Every authz-affecting CRD field is
  marked `// +keese:rebac-tuple=...` and paired with a design-doc
  reference.
- Upstream credentials live only in OpenBao (or cloud KMS) and are
  swapped for the agent's SA token at the gateway via
  `BackendSecurityPolicy`. They are never returned to the agent.
- Bundle + operator images are signed via Sigstore cosign (keyless OIDC);
  `operator-sdk bundle validate` and OpenSSF scorecard gate releases.

See [SECURITY.md](SECURITY.md) for vulnerability reporting and
[.claude/rules/05-security-zero-trust.md](.claude/rules/05-security-zero-trust.md)
for the full rule set.

## License

[Apache-2.0](LICENSE). Copyright (c) 2026 keese-ai. Maintainers listed in
[CODEOWNERS](CODEOWNERS).
