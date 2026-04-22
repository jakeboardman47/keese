<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# keese — Memory

MEMORY.md is a pointer index of **decisions made** and **gotchas hit**.
Keep it scannable. One line per entry: `- [Short title](path/to/detail.md) — one-sentence hook.`
If an entry needs more than two lines, write into `docs/references/` or `docs/designs/` and link here.

Update at the end of a sub-phase or after a surprising discovery. Do not use this file for
ephemeral task state — that belongs in a plan or a TodoWrite list.

## Decisions

### 2026-04-21 — Final design batch (12, 13, 14a, 14b, 15, 16, 19, 21, 25)

- 9 designs taken from `draft` to `current` via parallel architect dispatch.
  All scored ≥ 90 honestly; design count 53 → 62 (8 new companions).
- [12 network isolation](docs/designs/12-network-isolation.md) — NP-1 (default-deny) + NP-2 (egress to AI Gateway:443 + NATS:4222 only); SSA by Workspace controller; no Capsule overlap.
- [13 CLI tunnel](docs/designs/13-cli-tunnel-wireguard.md) — keesectl tunnel via WireGuard; OIDC ephemeral peer keys (audience template `keese-tunnel-<tenant>`); routes only ClusterIPs (no K8s API).
- [14a OLM channels](docs/designs/14a-olm-channels-upgrades.md) — three channels (stable/candidate/fast); replaces+skipRange upgrade graph; cosign verify + manual-only rollback.
- [14b OLM dependencies](docs/designs/14b-olm-dependencies.md) — four hard OLM deps via GVK syntax (cert-manager, Capsule, Argo, ExternalSecrets); rest Helmfile-only with per-component justification.
- [15 memory management](docs/designs/15-memory-management.md) — Memory + SharedMemory CRDs; 7-backend one-of (sqlite default + redis/qdrant/pgvector/neo4j/mem0/zep); EmbeddingDimImmutable VAP.
- [16 recipe distribution](docs/designs/16-recipe-distribution.md) — OCI-first via oras + cosign; three-gate admission (tools/model/extensions); reads GuardrailBinding effective policy (TOCTOU guard).
- [19 IDE + debugging](docs/designs/19-ide-and-debugging.md) — GoLand primary, VSCode secondary; dlv via SYS_PTRACE only (not privileged); ACP attach reuses 08b + D28.
- [21 OpenTofu cloud deployment](docs/designs/21-opentofu-cloud-deployment.md) — per-cloud modules (EKS/GKE Autopilot/AKS); state in S3+DynamoDB / GCS versioning / Azure lease; Conftest Rego policies.
- [25 CrossTenantAgreement CRD](docs/designs/25-cross-tenant-agreement.md) — full spec (4 files); resolves all five stub Qs; introduces NEW OpenFGA relation `tenant.can_approve_cra` (computed from admin); cosign or SA-token signature; TOFU snapshot for selectors.
- **Score-honesty audit:** 6 of 9 agents self-reported 100/100; spot-audit found Cat 4/5 inflation pattern (test SPECS named in design ≠ test FILES committed). Honest rescores: 12 ≈95, 14a ≈92.5, 14b ≈95, 16 ≈92.5, 19 ≈92.5, 21 ≈95. All still ≥ 90; flipped to current. Iter-log scores left as-recorded; audit notes captured here for future reviewers.

### 2026-04-21 — D29 + a2a/cross-tenant messaging reframe

- [D29 ratified](docs/plans/scaffolding-plan.md) — `CrossTenantAgreement` CRD (`tenancy.operator.keese.ai/v1alpha1`, cluster-scoped, cert-manager-style bilateral handshake). Kind count 16 → 17. Amends D23.
- [04a iter-5](docs/designs/04a-openfga-authz-model.md) — added `tenant.allows_messaging` + `workspace.messageable_from` ReBAC relations; old proposed `workspace#can_message` dropped. Cross-tenant a2a authz is workspace-pair-scoped.
- [04b iter-3](docs/designs/04b-projected-sa-identity.md) — `audienceTemplates` (`egress`, `workflowRun`, `supervisor`); agent pods now mount three projected SA tokens at `/var/run/keese/tokens/{egress,workflowRun,supervisor}`.
- [09 iter-3](docs/designs/09-transport-crd.md) — a2a peer-auth modes 4 → 2 (`workspace-sa`, `mutual-tls`); dropped `user-oidc` + `none`; new `spec.a2a.scope: intra-tenant | cross-tenant`. NATS is the primary intra-tenant transport.
- [03 iter-3 + 03c](docs/designs/03c-workflow-messaging-plane.md) — Workflow controller owns NATS topic provisioning (`keese.tenant.<t>.wf.<r>.*`), `workflowRun` audience injection, CRA admission, stream teardown.
- [Q2(b) decision](docs/designs/03c-workflow-messaging-plane.md) — cross-tenant peers derived implicitly from `transportRef`s with `scope: cross-tenant`. NO new `WorkflowRun.spec.participants[]` field.
- [Design 25 stub](docs/designs/25-cross-tenant-agreement.md) — CRD spec authoring deferred; full design pending (held at draft).
- Design count 48 → 53 (added `02-ii`, `04a-iii`, `09-ii`, `03c`, `25` to index).

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

### 2026-04-20 — 05b credential injection patterns

- [05b + 05b-ii authored](docs/designs/05b-credential-injection-patterns.md) —
  BSP encoding for static/AWS/GCP/Azure/pool credential types; rotation drain
  formula `max(remaining_old_TTL, 0.70 × new_TTL)`; workspace > tenant > cluster
  BSP precedence; vault-agent sidecar on gateway pod (not agent pod) for non-AI
  upstreams; iter-1 score 92.5 SHIP. 17 iter-1 flagged for pool state machine.

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
