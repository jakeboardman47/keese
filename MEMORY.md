<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# keese — Memory

MEMORY.md is a pointer index of **decisions made** and **gotchas hit**.
Keep it scannable. One line per entry: `- [Short title](path/to/detail.md) — one-sentence hook.`
If an entry needs more than two lines, write into `docs/references/` or `docs/designs/` and link here.

Update at the end of a sub-phase or after a surprising discovery. Do not use this file for
ephemeral task state — that belongs in a plan or a TodoWrite list.

## Decisions

### 2026-04-27 — LLM credential path stays gateway-side (rule 05.2 reaffirmed)

- Architect confirmed: agent pods carry **only** the projected SA token. The
  Envoy AI Gateway pod is the credential terminator — it loads the upstream
  key from OpenBao (via `BackendSecurityPolicy` + `ExternalSecret`) and
  injects it into the outgoing API call. No init-container or sidecar on
  the agent pod ever touches the upstream credential.
- For Anthropic specifically: BSP `APIKey` type injects `Authorization:
  Bearer <key>` (header name is hardcoded at v1alpha1 — see
  `BackendSecurityPolicyAPIKey` struct in
  `github.com/envoyproxy/ai-gateway/api/v1alpha1`). Anthropic expects
  `x-api-key: <key>` + `anthropic-version: 2023-06-01`. Closed by an
  `EnvoyExtensionPolicy` Lua filter wired in
  [dev/bootstrap/aigateway/anthropic-llm-stack.yaml](dev/bootstrap/aigateway/anthropic-llm-stack.yaml)
  (former TD-P2-13).
- `.envrc` already runs `dotenv .env.local` so `$ANTHROPIC_API_KEY` reaches
  `tilt up` → `scripts/dev/seed-openbao.sh` automatically once the user
  populates `.env.local`. No Tiltfile change needed.

### 2026-04-22 — Final 2 specs (tenancy + authz) close design-gate predicate

- Tenancy spec (4 files: primary + ii-tenant + ii-cra + iter-log) — covers Tenant (D26) + CrossTenantAgreement (D29). Honest score 87.5/100 — at the rubric SHIP threshold (≥85) but below conventional ≥90 target. Cat 4 −5 + Cat 5 −7.5 = full pre-gate docks (Makefile + webhook impl deferred to P8; test scaffolding deferred). Flagged for review as the lowest score in the spec batch.
- Authz spec (2 files: primary + iter-log) — covers OIDCProvider (D28). Honest 92.5/100 (Cat 4/5 −12.5 docked).
- **ESCALATION:** `tenant.uses_oidc_provider` is a NEW ReBAC relation referenced at authz spec §1.6 but NOT yet in [model.fga](dev/bootstrap/openfga/model.fga). Needs `rebac-modeler` dispatch before OIDCProvider controller is implemented.
- **Gate predicate state:** 27 specs all `status: current`, 0 draft. 62 designs all `current` (1 superseded). The architect-signed gate-open commit (flipping `gate_status: closed → open` in [docs/plans/README.md](docs/plans/README.md)) is now unblocked on the doc front; outstanding design-gate-related work: (a) the rebac-modeler dispatch for `tenant.uses_oidc_provider`, (b) optional human review of the lowest-scoring spec (tenancy at 87.5).

### 2026-04-22 — Spec batch (11 specs to current + 2 new draft stubs)

- 11 spec architects dispatched in parallel; all returned with status: current.
- Score-honesty audit: 9 of 11 honest (scores 92.5–97.5 with explicit Cat 4/5 docks); 2 inflated (runtime, recipe both claim 100).
- Honest rescores: runtime ≈95 (test names locked but bodies pre-gate), recipe ≈92.5 (8 envtest cases NAMED but bodies pre-gate). Both still ≥ 90; flipped to current; iter-log scores left as-recorded.
- 4 specs had `regression_lock: true` set incorrectly (workspace, memory, credential-broker, egress-authz). Per the spec lifecycle (`status: implemented` predicate), regression_lock should remain false until acceptance tests actually exist. Corrected all 4 to false.
- [workspace spec](docs/specs/workspace.operator.keese.ai-v1alpha1.md) — split into 5 files (primary + ii-workspace + ii-share + ii-session + ii-iter-log) to honor 200-line cap; covers Workspace + WorkspaceShare + WorkspaceSession (D27).
- [workflow spec](docs/specs/workflow.operator.keese.ai-v1alpha1.md) — split a/b for Workflow vs WorkflowRun + cross-tenant admission. Q2(b) decision recorded: cross-tenant peers derived from `transportRef`s with `scope: cross-tenant` (NO new participants[] field).
- **Two NEW spec stubs added** for D26/D28/D29 kinds (gap discovered during dispatch): [tenancy spec](docs/specs/tenancy.operator.keese.ai-v1alpha1.md) (Tenant + CrossTenantAgreement) and [authz spec](docs/specs/authz.operator.keese.ai-v1alpha1.md) (OIDCProvider). Both held at draft pending follow-up architect dispatch.
- Spec count 11 → 13 top-level + 10 companions = 23 spec files. Design-gate predicate updated: requires all 13 specs at status: current; currently 11/13 (the two new stubs are draft).

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

### 2026-04-30 — AI Gateway BSP injection requires EG `extensionManager.hooks.xdsTranslator`

- v0.4+ AI Gateway BSP types (`AnthropicAPIKey`, `APIKey`, `AWSCredentials`,
  …) inject upstream credentials and rewrite the request path **on the
  upstream filter chain** (`cluster.typed_extension_protocol_options.HttpProtocolOptions.http_filters`).
  The HCM ext_proc filter that EG installs from the `EnvoyExtensionPolicy`
  only fires on the downstream phase — it sets `x-ai-eg-original-path`,
  `x-ai-eg-internal-req-id`, and tags the request with the BSP backend
  name, but it does **not** add `x-api-key` and does **not** rewrite
  `/anthropic/v1/messages` → `/v1/messages`.
- The upstream filter chain only gets installed when EG is configured
  with the AI Gateway controller as an xDS-translator extension hook.
  Required EG values (mirroring [v0.5.0 reference values](https://github.com/envoyproxy/ai-gateway/blob/v0.5.0/manifests/envoy-gateway-values.yaml)):
  ```yaml
  config:
    envoyGateway:
      extensionApis:
        enableBackend: true
        enableEnvoyPatchPolicy: true
      extensionManager:
        hooks:
          xdsTranslator:
            translation:
              listener: { includeAll: true }
              route: { includeAll: true }
              cluster: { includeAll: true }
              secret: { includeAll: true }
            post: [Translation, Cluster, Route]
        service:
          fqdn:
            hostname: ai-gateway-controller.envoy-ai-gateway-system.svc.cluster.local
            port: 1063
  ```
  Without this, `request_attributes` and the upstream ext_proc filter
  are missing from the cluster, so the BSP credential never gets
  injected and Anthropic returns 404 on `/anthropic/v1/messages`.
- The AI Gateway controller's gRPC extension server on `:1063` is
  **plaintext**, NOT TLS — its `--tlsCertDir` flag is for the mutating
  webhook on `:9443`. Configuring `tls:` on the EG ExtensionService
  causes `tls: first record does not look like a TLS handshake`
  errors. (Verified by reading
  `cmd/controller/main.go:353` in the v0.5.0 source — the gRPC server
  is constructed with `grpc.NewServer(...)` without `grpc.Creds(...)`.)
- `helm upgrade --reuse-values` does NOT drop a `tls:` block previously
  set under `extensionManager.service`. Use `--reset-values -f` when
  removing nested keys, otherwise the helm-rendered ConfigMap retains
  the stale block.
- Dev-loop verification: post-fix, the cluster config dump exposes
  `cluster.typed_extension_protocol_options.HttpProtocolOptions.http_filters[0]`
  = `envoy.filters.http.ext_proc/aigateway` with `request_attributes:
  ['xds.upstream_host_metadata.filter_metadata['aigateway.envoy.io']['per_route_rule_backend_name']', …]`.
  End-to-end: agent pod → gateway → Anthropic, HTTP/2 200 with real
  completion content. Configured in
  [dev/bootstrap/values/envoy-gateway.yaml](dev/bootstrap/values/envoy-gateway.yaml).

### 2026-04-29 — Envoy Gateway v1.6 BackendTLSPolicy needs `gateway.networking.k8s.io/v1`

- EG v1.6.0 watches `gateway.networking.k8s.io/v1.BackendTLSPolicy` (GA in
  Gateway API v1.2). Manifests still on `v1alpha3` are silently ignored —
  EG logs `BackendTLSPolicy CRD not found, skipping watch` and the
  upstream listener has `transport_socket_count: 0` (cleartext to
  `api.anthropic.com:443`, instant TLS handshake failure).
- Helm OCI charts do **not** upgrade CRDs across releases (helm only
  installs CRDs on first install of a chart). The cert-manager / Gateway
  API CRDs that ship under `crds/` in the EG chart must be applied
  separately on every chart bump:
  `kubectl apply -f $(helm pull oci://docker.io/envoyproxy/gateway-helm --version v1.6.0 --untar -d /tmp/eg && echo /tmp/eg/gateway-helm/crds/gatewayapi-crds.yaml)`.
  The bundled file ships **both** v1 and v1alpha3 of BackendTLSPolicy.
- After applying CRDs, restart `envoy-gateway` so the controller-runtime
  informer registers the new GVK; until restart it reports
  `BackendTLSPolicy CRD not found`.
- Verified-working manifest: [dev/bootstrap/aigateway/anthropic-llm-stack.yaml](dev/bootstrap/aigateway/anthropic-llm-stack.yaml)
  uses `apiVersion: gateway.networking.k8s.io/v1` + `caCertificateRefs`
  pointing at the trust-manager-distributed `public-ca-bundle` ConfigMap
  (NOT `wellKnownCACertificates: System` — EG v1.6 expects an SDS-supplied
  secret for system CAs that the gateway does not auto-provide).
  Verified post-fix via `config_dump`:
  `transport_socket_count: 1, tls_present: true`.

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
