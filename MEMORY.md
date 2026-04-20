<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# keese — Memory

MEMORY.md is a pointer index of **decisions made** and **gotchas hit**.
Keep it scannable. One line per entry: `- [Short title](path/to/detail.md) — one-sentence hook.`
If an entry needs more than two lines, write into `docs/references/` or `docs/designs/` and link here.

Update at the end of a sub-phase or after a surprising discovery. Do not use this file for
ephemeral task state — that belongs in a plan or a TodoWrite list.

## Decisions

### 2026-04-20 — initial scaffolding (P0–P8)

- [Scaffolding plan + 26 decisions](docs/plans/scaffolding-plan.md) —
  license Apache-2.0; API groups `*.operator.keese.ai`; Capsule opt-in;
  GuardrailBinding composition (not Constitution + Policy +
  ToolAllowList); 14 kinds across 9 groups (D26 added keese `Tenant`
  CRD); Envoy AI Gateway + MCPRoute; Argo delegation; OpenTofu cloud;
  GoLand primary IDE; SIGTERM drain; SSA fieldOwner; durable agent
  identity (D24) + GUPP resume contract (D25) added 2026-04-20 after
  Gas Town review; D26 keese Tenant CRD amends D23 for ReBAC backing.
- [Session handoff summary](docs/plans/scaffolding-summary.md) —
  state after P0–P8; next-phase instructions; resume commands after
  clone/move.

## Gotchas

### 2026-04-20 — scaffolding cycle

- [markdownlint relaxations](.markdownlint.json) — MD003/MD004/MD007/
  MD010/MD022/MD029/MD031/MD032/MD034/MD040/MD049/MD050 disabled;
  template + operator-sdk outputs collide with frontmatter and mixed
  marker styles. Re-enable case-by-case once project style stabilizes.
- [shellcheck -S error](.pre-commit-config.yaml) — only errors block
  commits; warnings/info/style are review concerns. Keep until
  `.envrc`/template scripts land dedicated fixes.
- [gitleaks removed from pre-commit](.pre-commit-config.yaml) —
  collided with detect-secrets baseline's own hashed fingerprints.
  Re-add behind `.gitleaks.toml` allowlist if wanted in CI.
- [operator-sdk not in nixpkgs (unverified 2026-04)](flake.nix) — use
  `go install github.com/operator-framework/operator-sdk/cmd/operator-sdk@latest`
  as fallback; `bin/operator-sdk` is gitignored.
- [design-gate LOC_LIMIT=35](scripts/check-design-gate.sh) —
  operator-sdk scaffold lands ~27 non-blank non-comment LOC per
  controller; limit set to accept the stub and trip any real
  implementation.

## Open questions (being tracked)

- Chart versions in `dev/bootstrap/helmfile.yaml` need verifying
  against 2026-Q2 releases. Flagged `# unverified-2026` where relevant.
- Whether `kuttl`, `setup-envtest`, `controller-gen`, `ctlptl`,
  `cmctl`, `tflint` are available in current nixpkgs stable under the
  expected attr names — commented in `flake.nix` with `unverified`.
- OpenBao PVC-backed init sequencing vs `-dev` mode trade-off; current
  seed script assumes non-dev but initial unseal flow not fully
  exercised.

## Format rules

- New entries go at the top of the relevant section.
- Each line: `- [Short title](path) — ≤120-char hook.`
- Link to the detail doc; do not inline context here.
- Dated header is optional; add one when clustering a batch of related entries:
  `### YYYY-MM-DD — short cluster title`.
