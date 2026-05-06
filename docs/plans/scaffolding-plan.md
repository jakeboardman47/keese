<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Keese — Scaffolding Plan

**Target repo:** `github.com/keese-ai/keese`
**Local path:** any; this doc travels with the repo. Currently at
`/Users/marshallmccain/src/github.com/keese-ai/keese` (originally scaffolded
under `github.com/mccodeman/keese` before the repo moved to the `keese-ai`
GitHub org).
**Template source:** `github.com/mccodeman/claude-project-template`
**License:** Apache-2.0 (all source, docs, manifests, and generated artifacts)
**One-liner:** Secure multi-tenant, multi-workspace Kubernetes operator orchestrating autonomous AI agent workflows on pluggable agent runtimes (goose first).
**Plan status:** v3 executed. Phases P0–P8 landed 2026-04-19/20; see
[scaffolding-summary.md](scaffolding-summary.md) for the final state
and resume instructions.

---

## Context

Greenfield scaffold. The scaffolding work must:

1. Let Claude (multi-agent, worktree-parallel) drive most future work autonomously.
2. **Land zero operator/CRD logic** until designs + specs are written and scored ≥ 90.
3. Install cleanly via OLM, run cleanly in kind/tilt locally, be CI/CD-ready day one.
4. Bake in security (zero-trust egress, ReBAC, projected SA tokens, pluggable vault) as non-negotiable scaffolding.
5. Be portable across agent runtimes — goose is first, not only.
6. Prefer composition over new CRDs; prefer upstream primitives where they exist.

Broken into **nine phases (P0–P8)**. Each scored against `docs/plans/rubric.md`. Iteration cap: 3 passes per phase; target ≥ 90/100 before execution begins.

---

## Key decisions (v3 — revised after user feedback + research)

| # | Decision | Notes |
|---|---|---|
| D1 | **Go + Operator SDK** (`go/v4` plugin) | OLM bundle tooling included. |
| D2 | **Multi-group CRDs** under `*.operator.keese.ai` base domain | Renamed from `*.keese.ai` per user. |
| D3 | **Use Capsule `capsule.clastix.io/Tenant` directly** — no keese `Tenant` CRD | Capsule reconciles namespaces + NetworkPolicy/Quota/LimitRange/RBAC per tenant. `vcluster` via `Workspace.spec.isolation: hard` opt-in. |
| D4 | **OpenFGA** for ReBAC — CNCF Incubating, Apache-2.0, DSL + `fga` CLI + playground | SpiceDB is stronger on consistency tokens + raw graph scale; revisit at >100M tuples. Ory Keto rejected (anemic 2024–2026 releases). |
| D5 | **Envoy AI Gateway v0.5.x + Envoy Gateway v1.5+** — not plain Envoy Gateway | `MCPRoute` (JSON-RPC parsing, per-method CEL), `BackendSecurityPolicy` (AWS OIDC / GCP WI / Azure Entra / static API keys), token-cost rate limiting, ext_proc for Presidio/LlamaGuard guardrails. v1alpha1 CRDs — pin by digest, thin adapter in operator. |
| D6 | **NATS JetStream via NACK** (not NATS Operator — deprecated) | NACK `Stream`/`Consumer` CRDs consumed directly; no keese wrapper. |
| D7 | **Pluggable `Workflow.spec.triggers[]` + `.outputs[]`** — compose CronJob / KEDA / Knative / webhook; no new `WorkflowTrigger` CRD | Outputs compose Knative Sinks / NATS streams. |
| D8 | **Goose-first `AgentRuntime` SPI**; goose headless via `goose run --recipe` + ACP-over-stdio for long sessions | Sub-agents up to 10 concurrent. SQLite session store default; Memory CRD for richer backends. |
| D9 | **`AgentRuntime` SPI** — Go interface + CRD; goose one impl; slot for claude-code/aider/etc. | Interface + CRD versioned with SemVer (apidiff-gated). |
| D10 | **OpenBao + ExternalSecrets Operator (or OpenBao Secrets Operator)** → K8s Secret → `BackendSecurityPolicy` | No vault-agent sidecar on gateway pod. Keep sidecar option only for non-AI upstreams needing file-mount creds. |
| D11 | **Kind + Tilt + ctlptl** for local; **cert-manager** for TLS; **Capsule** for tenants; **Argo Workflows** delegated-to by `Workflow` CR; **Elastic APM (bundled with ECK)** for traces in dev (Jaeger fallback if APM > 30s boot) | ctlptl idempotently wires kind + local registry. |
| D12 | **Kustomize** for in-cluster manifests; **OpenTofu** for cloud deployments; **Helm** consumed (cert-manager, NACK, ECK, Capsule, Envoy AI Gateway, Argo) but not authored by keese beyond an OLM-less export | Cloud overlays move out of `config/` and into `deploy/opentofu/{aws,gcp,azure}/`. |
| D13 | **Three-table credential decomposition** at Envoy: Route (→ Backend) + BackendSecurityPolicy (→ credential source) + OpenFGA tuple (→ allow/deny). Projected SA audiences **per-tenant** (`keese-egress-<tenant>`) so tenant Bedrock role trust policies are tight. | Token caching: per-gateway-pod, keyed by (audience, upstream role), refresh at 70% TTL, fail-closed past 95%. See `designs/16-credential-broker.md`. |
| D14 | **Collapse guardrails into one `GuardrailBinding` CRD** — composition metadata only. Composes Kyverno ClusterPolicy + OpenFGA tuples + Envoy `SecurityPolicy` + recipe `pre_flight`/`post_flight` hooks + `TokenBudget`. | Retire `Constitution`, `GuardrailPolicy`, `ToolAllowList` as separate CRDs. Cluster-scoped `default` binding auto-injected via mutating webhook; VAP on update forbids removing it. Strictest-wins merge. Tenant admin inheritance required; workspace admin may only tighten. |
| D15 | **release-please** (Google) — Conventional Commits → Release PR → tag; downstream workflows (`bundle.yaml`, `image.yaml`, `chart.yaml`) each use correct tooling | semantic-release rejected (Node-centric, publishes directly); goreleaser rejected for operator (no OLM bundle CSV story). goreleaser reserved for future `keesectl` CLI. |
| D16 | **Server-Side Apply (SSA) with controller-specific `fieldOwner`** — every controller uses `client.Apply` with `fieldOwner = keese-<kind>-controller` | ValidatingAdmissionPolicy (CEL, K8s 1.30 GA) preferred over webhooks for static invariants; webhooks only where CEL insufficient. |
| D17 | **Elastic APM** for traces in dev (bundled with ECK); **Jaeger** fallback if APM slows boot > 30s; **ELK (ECK)** for logs + metrics in dev and out-of-box | OTEL collector routes traces → APM; logs → ES. Token-accounting exported as OTEL metrics + Prom remote-write. |
| D18 | **OpenTofu for cloud deployment** — `deploy/opentofu/{aws,gcp,azure}/` modules for cloud infra (EKS/GKE/AKS, secret managers, IAM roles, CLB/ALB + external-DNS); keese OLM bundle/CSV installed on top via `operator-sdk run bundle` | Replaces kustomize cloud overlays in D12. |
| D19 | **GoLand as primary IDE**, VSCode as secondary — JetBrains ships native ACP support + superior Go + K8s tooling; VSCode uses community ACP extensions + `block.vscode-goose` | Documented in `docs/references/ide-and-debugging.md`; both IDE configs in `dev/ide/`. |
| D20 | **Argo Workflows delegated-to by keese `Workflow`** — keese CR projects Argo `WorkflowTemplate`; `WorkflowRun` projects Argo `Workflow` | Tekton / Knative Eventing rejected for DAG execution (Knative Eventing still used for `.outputs[]`). |
| D21 | **Signal handling:** controllers + agents must drain on **SIGTERM** (graceful checkpoint to PVC/NATS/ES); SIGKILL (uncatchable) recovery is via durable state + idempotent restart; SIGSTOP is OS/supervisor-controlled and not caught — checkpoint pattern covers the contract | Encoded in `.claude/rules/06-signal-handling.md`; liveness probes tuned to allow drain window. |
| D22 | **Claude agent model discipline** — opus for architecture/strategy (architect, rebac-modeler, crd-author when designing), sonnet for implementation (implementer, test-engineer, controller-author, infra-bootstrap, guardrail-author, olm-author), haiku for narrow lookup (explorer, debugger-investigations); plan-scorer sonnet | Codified in each agent's frontmatter; enforced by `settings.json` `CLAUDE_CODE_SUBAGENT_MODEL` per dispatch. |
| D23 | **Compose over replicate** — drop `Tenant`, `AgentIdentity`, `Entitlement`, `EntitlementGrant`, `TelemetryPipeline`, `WorkflowTrigger`, `Constitution`, `GuardrailPolicy`, `ToolAllowList` as separate CRDs | Each has a superior existing primitive (Capsule / OpenFGA tuples / OTEL Collector CR / pluggable trigger field / GuardrailBinding). |
| D24 | **Agent identity is durable; sessions are ephemeral.** Workspace is the persistent agent identity. Pod churn is expected; state survives via PVC (goose SQLite), NATS JetStream (in-flight), OpenFGA (relations), and `Memory` CRD backends (long-term). | Test obligation: SIGKILL mid-run → resume on a new pod with no duplicate side effects. Added 2026-04-20 after review of Steve Yegge's "Welcome to Gas Town"; prevents conflation of Workspace with agent pod. See `docs/designs/23-agent-supervision.md`. |
| D25 | **GUPP contract for AgentRuntime SPI.** Every runtime exposes `Resume(ctx, workspace)`; the controller MUST invoke it on observing pending work with no active session. Timeout → event `AgentUnresponsive`; escalation ladder lives in `docs/designs/23-agent-supervision.md`. | "GUPP" = Yegge's *"if there is work on your hook, YOU MUST RUN IT."* Prevents agents sitting idle while work sits on the hook. Added 2026-04-20. |
| D26 | **Keese `Tenant` CRD owns keese-specific tenancy config; delegates namespace aggregation to Capsule (Mode B) or derives from labels (Mode A).** New group `keese.ai/v1alpha1/Tenant`. Cluster-scoped. Spec: `guardrailBindings[]`, `tokenBudgetRef`, `credentialPoolRef`, `defaultQuota`, optional `capsuleTenantRef`. Does **not** reimplement namespace aggregation. Kind count 13 → 14 across 9 groups. | Partial amendment to D23 (Tenant was on the drop list). Rationale: tenants need a first-class K8s object for ReBAC backing, tenant-wide config (no ConfigMap sprawl), events, and finalizers. Capsule still owns namespace aggregation + quota in Mode B. Added 2026-04-20 after architectural review; migration plan: `docs/plans/migration-d23-tenant-crd.md` (to author). Detailed design: `docs/designs/24-tenant-crd.md`. |
| D27 | **`WorkspaceSession` CRD represents one active interactive attach session.** Namespaced, lives in the Workspace's namespace. Created by the operator on `kubectl-keese attach` when `Workspace.spec.interactive: true`, keyed by `(workspaceRef, attachSubject, sessionName)` — default session name is `default`; additional named sessions allowed via API. Owner-ref to parent Workspace; finalizer drives pod drain + PVC release + tuple cleanup. Kind count 14 → 15; group count unchanged (stays in `keese.ai`). | Kubectl-native delete + GitOps UX + independent RBAC + independent lifecycle from the template. `per-user`/`per-attach`/`shared` session modes handled by `(subject, sessionName)` uniqueness rules. Added 2026-04-21 after multi-session-per-user + interactive-vs-workflow-mutual-exclusion architectural review. Detailed spec: `docs/designs/02-workspace-model.md` iter-2 and `docs/designs/08b-goose-acp-stdio-k8s.md` iter-2. |
| D28 | **`OIDCProvider` CRD carries per-issuer JWT-to-OpenFGA-subject transformation config.** New group `authz.keese.ai/v1alpha1/OIDCProvider`. Cluster-scoped. Spec: `issuer`, `audiences[]`, `subjectTemplate` (Go template over JWT claims), `jwksUri`, `normalization`. Operator bootstraps defaults for google / github-actions / azure-entra / okta / keycloak / gitlab; tenants opt-in via `Tenant.spec.oidc.allowedProviders[]`. Kind count 15 → 16; group count 9 → 10 (new `authz.keese.ai`). | Declarative + VAP-validated + GitOps-friendly. Not replicating OpenFGA primitives: OpenFGA sees only transformed subject strings; OIDCProvider is the config layer BEFORE OpenFGA. Agent SA subject form is `user:ksa-<workspace-uid>` (bare SA name; OpenFGA is per-cluster so no further domain disambiguation needed). Human subject form is `user:<email-or-sub@domain>` per provider template. Added 2026-04-21. Detailed spec: `docs/designs/04b-projected-sa-identity.md` iter-2. |
| D29 | **`CrossTenantAgreement` CRD with cert-manager-style bilateral handshake gates cross-tenant a2a messaging.** New kind in `keese.ai/v1alpha1/CrossTenantAgreement`. Cluster-scoped (cross-tenant by definition). Spec: `from: {tenantRef, workspaceSelector}`, `to: {tenantRef, workspaceSelector}`, `scope: {natsSubjects[], a2aRoles[]}`, `expiresAt`. Status: `phase: Pending|Approved|Rejected|Expired`, `approvals[]: {tenant, approvedBy, approvedAt, signature}`. Controller writes `tenant:T_to#allows_messaging@tenant:T_from` + `workspace:W_to#messageable_from@workspace:W_from` ReBAC tuples ONLY after both-side approval. Manual tuple-writing supported (third-party authz workflows) — controller no-ops when tuple already exists out-of-band. Intra-tenant a2a is implicit via Workflow definition (NATS topic existence IS authz). Kind count 16 → 17; group count unchanged (stays in `keese.ai`). | Workspace-author UX requires a CRD (declarative, GitOps-friendly, kubectl-native). Cert-manager-style handshake (both tenants approve before tuple write) prevents unilateral cross-tenant escalation. Out-of-band tuple writing supported for third-party / out-of-cluster authz systems. Amends D23 (CrossTenantAgreement was on the drop list — now justified by the workspace-as-security-boundary reframe). Added 2026-04-21 after a2a/cross-tenant messaging architectural reframe. Detailed spec: `docs/designs/25-cross-tenant-agreement.md`. Drives 04a iter-5 (`tenant.allows_messaging` + `workspace.messageable_from` relations), 04b iter-3 (`audienceTemplates` including `workflowRun`), 09 iter-3 (a2a peer-auth modes reduced to 2 + scope field), 03 iter-3 (Workflow controller topic provisioning + cross-tenant admission check). |

---

## Final kind list — **17 kinds across 10 groups** (+1 kind since D28)

All groups under `*.operator.keese.ai`, all `v1alpha1`:

| Group | Kinds | Count |
|---|---|---:|
| `keese.ai` | `Tenant` (D26), `CrossTenantAgreement` (D29) | 2 |
| `keese.ai` | `Workspace`, `WorkspaceShare`, `WorkspaceSession` (D27) | 3 |
| `keese.ai` | `Workflow`, `WorkflowRun` | 2 |
| `keese.ai` | `AgentRuntime`, `RuntimeExtension` | 2 |
| `keese.ai` | `Memory`, `SharedMemory` | 2 |
| `keese.ai` | `Recipe`, `RecipeSource` | 2 |
| `authz.keese.ai` | `GuardrailBinding` | 1 |
| `policy.keese.ai` | `TokenBudget` | 1 |
| `keese.ai` | `Transport` | 1 |
| `authz.keese.ai` | `OIDCProvider` (D28) | 1 |
| **TOTAL** | | **17** |

**Deferred / composed (no CRD):**
- **Namespace aggregation** → Capsule `capsule.clastix.io/v1beta2/Tenant` (Mode B) or label selector on namespaces (Mode A). Keese `Tenant` (D26) holds keese-specific config and references Capsule's in Mode B.
- **Identity** (AgentIdentity, Entitlement, EntitlementGrant) → OpenFGA tuples written by Workspace/GuardrailBinding controllers; ConfigMap-backed tuple writer for ToolAllowList.
- **Guardrail primitives** (Constitution, GuardrailPolicy, ToolAllowList) → folded into `GuardrailBinding` composition.
- **Workflow triggers** → `Workflow.spec.triggers[]` projects CronJob / KEDA ScaledObject / Knative Trigger / HTTPRoute-webhook.
- **Workflow outputs** → `Workflow.spec.outputs[]` projects Knative Sinks / NATS streams / SlackSource / S3 / gh-PR.
- **Telemetry pipeline** → ship a sample `opentelemetry.io/OpenTelemetryCollector` + `monitoring.coreos.com/ServiceMonitor`; no keese wrapper.
- **TransportBinding** → NACK `Consumer` or Dapr `Subscription` directly.

**Unique value each KEPT CRD provides:**
- `Workspace` — single spec projects ~7 resources (Deployment + PVC + SA + NP + HTTPRoute + OpenFGA tuples + Capsule labels); lifecycle FSM, idle eviction, runtime binding.
- `Workflow` — Argo projection + guardrail binding + messaging wiring in one CR.
- `AgentRuntime` / `RuntimeExtension` — pluggable runtime SPI (goose first) — unique to keese.
- `Memory` / `SharedMemory` — workspace/tenant/shared memory backend (sqlite/redis/qdrant/pgvector/neo4j/mem0/zep) via discriminated one-of; ReBAC gates on shared scope.
- `Recipe` / `RecipeSource` — OCI-first recipe distribution + admission gating against workspace entitlements (tools, model); no upstream match.
- `GuardrailBinding` — single pane for "what guardrails apply here" + merge engine.
- `TokenBudget` — unique spec, composed enforcement (OTEL processor + Envoy rate-limit).
- `Transport` — `spec.type` selector (nats, a2a, mcp, stdio); thin pluggability surface.
- `WorkspaceShare` — opt-in cross-ns sharing via ReferenceGrant + OpenFGA tuples.

---

## Rubric (per-phase scoring)

From `docs/plans/rubric.md` (template, 100 pts). `SHIP ≥ 85` · `REVISE 65–84` · `REPLAN < 65`.

| # | Category | Wt | # | Category | Wt |
|---|---|---:|---|---|---:|
| 1 | Scope clarity | 10 | 6 | Failure-mode awareness | 10 |
| 2 | Architecture fit | 10 | 7 | Context efficiency for Claude | 10 |
| 3 | Security posture | 15 | 8 | Docs quality | 5 |
| 4 | Automatability | 10 | 9 | Observability | 5 |
| 5 | Verifiability | 15 | 10 | Operational readiness | 10 |

---

## Phase map

| Phase | Title | Blocks next on | Iter-3 score |
|---|---|---|---:|
| P0 | Repo foundation & licensing (Apache-2.0) | — | 90 |
| P1 | Claude automation (copy + customize `.claude/`) | P0 | 92 |
| P2 | Dev env (flake, direnv, Makefile, envs) | P1 | 90 |
| P3 | Pre-commit hardening (Go + K8s + OLM) | P2 | 89 |
| P4 | Docs skeleton — **designs first, specs after** | P1 | 93 |
| P5 | CI/CD (10 GH Actions workflows) | P3 | 90 |
| P6 | Go operator scaffold (13 empty-stub kinds) | P2, P4-designs | 92 |
| P7 | Local infra bootstrap (kind/tilt/Capsule/Envoy AI GW/OpenFGA/NATS/OpenBao/ECK/Qdrant/Argo) | P6 | 91 |
| P8 | Design-gate freeze enforcement | P4, P6 | 91 |

**Hard gate:** No `*_types.go` body, no `*_controller.go` reconcile logic, no webhook logic beyond auto-generated stubs until P8 passes. Every stub carries `TODO(design-gate)` + ≤ 20 LOC.

**Sequencing discipline** (new): **All 22 design docs must reach `status: current` and score ≥ 90 before ANY spec doc is authored.** Specs are contracts derived from designs. This enforces "complete designs before specs" per user feedback.

---

## Phase 0 — Repo foundation & licensing

**Goal:** Empty, linted, **Apache-2.0** repo ready for Claude on first clone.

### Artifacts
- `git init`; default branch `main`.
- Template files copied and substituted (`{{PROJECT_NAME}}→keese`, `{{ORG_NAME}}→keese-ai`, `{{YEAR}}→2026`, `{{LICENSE_ID}}→Apache-2.0`, `{{LAST_VERIFIED}}→2026-04-19`): CONTRIBUTING.md, SECURITY.md, MEMORY.md, .gitignore, .gitattributes, .editorconfig, .markdownlint.json, .commitlintrc.json, lychee.toml, .secrets.baseline.
- **LICENSE** → Apache-2.0 text (the template already ships this; retained); all SPDX markers are `Apache-2.0` (template default); `addlicense` pre-commit stays on `-l apache`.
- README.md rewritten (keese day-one): north-star paragraph · **Claude routing block** (CLAUDE.md → docs/plans/README.md → .claude/rules/*) · repo-map table · **Design-gate banner** (STATUS: CLOSED) · quickstart · contributing · security posture in 5 bullets · **Apache-2.0 license** note.
- `.env.local.example` tailored for keese (see P2.3).
- `.envrc` → `nix develop`.
- **.gitignore +:** `bin/`, `dist/`, `cover.out`, `cover.html`, `*.tar`, `envtest/`, `envtest-bin/`, `testbin/`, `kubeconfig*`, `.kube/`, `.tilt/`, `tilt_modules/`, `charts/*.tgz`, `dev/kind/.kube/`, `dev/bootstrap/*/charts/`, `dev/bootstrap/*/crds/downloaded/`, `bundle/tests/scorecard/*.json`, `manifests.tmp/`, `.keese/local-state/`, `deploy/opentofu/**/.terraform/`, `deploy/opentofu/**/terraform.tfstate*`, `deploy/opentofu/**/.tofu/`.
- **.gitattributes +:** `zz_generated.deepcopy.go`, `config/crd/bases/*.yaml`, `bundle/manifests/**`, `bundle/metadata/**` → `linguist-generated`.
- **CODEOWNERS** — three-team split (`@keese-ai/{maintainers,architects,security}`) per user confirmation.

### Initial commits (5 atomic, Conventional Commits)
1. `chore(repo): initial scaffold from claude-project-template`
2. `chore(repo): substitute template placeholders for keese (Apache-2.0 retained)`
3. `docs(readme): author keese day-one README and security callouts`
4. `chore(repo): extend .gitignore/.gitattributes for operator-sdk + opentofu artifacts`
5. `chore(repo): seed CODEOWNERS with architects/maintainers/security split`

### Acceptance
- `pre-commit run --all-files` passes.
- `git log --oneline` shows the five commits.
- `scripts/verify-placeholders.sh` — zero `{{...}}` outside test fixtures.
- Every source/doc file has `SPDX-License-Identifier: Apache-2.0`; non-Apache SPDX markers are rejected by the `addlicense` pre-commit hook.

### Iteration log — iter 3 total **90/100** (SHIP)

---

## Phase 1 — Claude automation customization

**Goal:** `.claude/` tailored to keese so every session routes correctly on day one.

### Model discipline (D22) — per-agent frontmatter fixed
- **Opus** (architecture/strategy): `architect`, `rebac-modeler`, `crd-author` (while designing — switches to sonnet during stub-generation), design-focused agents.
- **Sonnet** (implementation + scoped work): `implementer`, `test-engineer`, `controller-author`, `infra-bootstrap`, `guardrail-author`, `olm-author`, `plan-scorer`, `security-reviewer` (when reviewing), `debugger` (when fixing).
- **Haiku** (narrow lookup): `explorer`, `debugger` (during initial investigation only).

Each agent's frontmatter carries `model:` + `isolation:` + `tools:` + `allowed_paths:` exactly. `scripts/agent-dispatch.sh` reads model from frontmatter and sets `CLAUDE_CODE_SUBAGENT_MODEL` accordingly.

### Copy verbatim from template
All 7 agents, 3 commands, 3 hooks, 4 rules, 6 skills; scripts (`agent-dispatch.sh`, `worktree-merge.sh`, `scripts/lib/{log,env,paths,signals}.sh`).

### Per-agent keese deltas (append before copying)
- **explorer** — `rg` scoped to `api/ internal/ config/ docs/ deploy/`; never read `.env.local` or `kubeconfig*`.
- **implementer** — before commit: `make fmt vet lint manifests generate`; if `internal/controller/**` or `api/**` touched → `make test-integration` mandatory. Hand off to `debugger` instead of stubbing. Never `panic`/`log.Fatal` in controller code. **Use Server-Side Apply** (`client.Apply` with fieldOwner).
- **architect** (opus) — read `designs/20-api-group-layout.md` + `07-agent-runtime-provider-interface.md` first. D1–D26 load-bearing.
- **test-engineer** — unit fakes (`internal/controller/fake/`); integration envtest; e2e kuttl; idempotency test mandatory per reconciler (≥ 3 reconciles stable).
- **plan-scorer** — cat-3 (security) line-by-line vs `rules/05`; cat-5 (verifiability) requires envtest + kuttl *test names* present.
- **debugger** — on controller loop: dump status + events + last 100 reconciles; stern logs → `.plan-logs/`; never `time.Sleep` as fix.
- **security-reviewer** — `trivy fs`, `gosec`, `govulncheck`, `operator-sdk bundle validate`, `scripts/check-netpol-wildcards.sh`; CRITICAL on any `resources: [*]` / `verbs: [*]`.

### Six new keese-specific agents
1. **`crd-author.md`** — opus (design phase) / sonnet (stub generation); worktree `agent/crd-<slug>`; tools: `operator-sdk create api`, `controller-gen`, `make manifests generate`; edits only `api/`, `config/crd/`, `config/samples/`.
2. **`controller-author.md`** — sonnet; worktree `agent/controller-<slug>`; +`make test-integration`, `kuttl`, `stern`; edits only `internal/controller/`. Enforces SSA.
3. **`olm-author.md`** — sonnet; solo; tools: `operator-sdk generate bundle`, `operator-sdk bundle validate`.
4. **`infra-bootstrap.md`** — sonnet; worktree `agent/infra-<slug>`; owns `dev/bootstrap/` (Helmfile + kustomize). Ensures `make bootstrap-infra` ≤ 5 min.
5. **`rebac-modeler.md`** — **opus**; solo (ReBAC model is cross-cutting). Tools: `fga` CLI. Authors `dev/bootstrap/openfga/model.fga` + `docs/specs/egress-authz-protocol.md`. Refuses tuple shapes without design-doc reference.
6. **`guardrail-author.md`** — sonnet; worktree `agent/guardrail-<slug>`. Owns `GuardrailBinding` CRs + `dev/samples/guardrails/`; enforces default-inherit webhook pattern.

### Rules (new + updated)
- **`rules/04-kubernetes.md`** (12 rules) — domain `<group>.operator.keese.ai` · v1alpha1 first · controller-runtime-only reconcilers · `status` subresource + `observedGeneration` · finalizer IDs `finalizers.<kind>.operator.keese.ai/<purpose>` · no `panic`/`log.Fatal`/`os.Exit` · no wildcard RBAC · printer columns mandatory · event reasons enumerated · envtest-first with ≥ 3-reconcile idempotency · webhooks do only validation/defaulting · samples pass `--dry-run=server`. **+ rule: all writes via Server-Side Apply with controller-specific fieldOwner**.
- **`rules/05-security-zero-trust.md`** (13 rules) — agent pods get no kubeconfig, no LLM keys · identity = projected SA token with **per-tenant audience** `keese-egress-<tenant>` · upstream creds live only in OpenBao/KMS, swapped at Envoy AI Gateway via `BackendSecurityPolicy` · NetworkPolicy fail-closed · every authz-affecting CRD field carries `// +keese:rebac-tuple=...` marker · secrets = projected files under `/var/run/keese/secrets/` · no `host{Network,PID,IPC}`/`privileged`/`allowPrivilegeEscalation` · images pinned by digest in CSVs · ext_authz logs (tuple, SA, host, decision) — never tokens/bodies · `keese.ai/unsafe-*` blocked unless `break-glass=true` · `kubectl apply` to `prod-*` contexts denied · SBOM + cosign signature required · **+ rule: credential caching per-gateway-pod, refresh at 70% TTL, fail-closed past 95%**.
- **NEW `rules/06-signal-handling.md`** (D21) — 8 rules:
  1. Every long-running process installs a SIGTERM handler that drains in-flight work, checkpoints state (PVC/NATS/ES), and exits 0 within the configured `terminationGracePeriodSeconds` (default 60s; agent runtime 120s).
  2. SIGHUP reloads config where relevant (operator: re-read leader-election + health ports); otherwise is logged and ignored.
  3. SIGSTOP + SIGKILL are uncatchable. Processes MUST be restart-safe: all state needed to resume lives in durable stores (PVC for SQLite, NATS JetStream for messages, ES for logs, OpenBao for secrets). Recovery after SIGKILL is by idempotent restart.
  4. Controllers drain their reconcile queue on SIGTERM and release the leader lease before exit.
  5. Agent runtimes (goose) flush session state to SQLite-on-PVC every reconcile step and on SIGTERM.
  6. Envoy Gateway pods drain via `gateway.envoyproxy.io` native draining (`preStop: sleep 30` + Envoy `/healthcheck/fail`).
  7. Liveness probes accommodate graceful-drain window (initialDelay + periodSeconds × failureThreshold ≥ drain budget).
  8. Every process logs a structured `shutdown` event with (reason, drain-duration, checkpoint-location) at exit.
- **Update `rules/01-conventions.md`** header — add rule precedence ladder: `05-security > 04-kubernetes > 06-signal-handling > 02-security > 03-context-mgmt > 01-conventions > 06-testing`.

### Two new skills
- **`skills/crd-authoring.md`** — naming, `<group>.operator.keese.ai`, openAPIV3Schema, CEL `XValidation`, printer columns, status convention, conversion strategy, discriminated one-of for provider-style fields (per `Memory.spec.provider`), sample discipline (≥ 2 per CRD).
- **`skills/controller-authoring.md`** — reconcile idiom (fetch → deepcopy → desired → **SSA with fieldOwner** → status patch), idempotency, finalizers, spec/status separation, event reasons table, `predicate.GenerationChangedPredicate`, rate limiting (`DefaultControllerRateLimiter`; max 1000s), envtest patterns (`Eventually` 10s/250ms), **SIGTERM drain pattern**.

### `.claude/settings.json` deltas
- **allow +:** `operator-sdk *`, `controller-gen *`, `setup-envtest *`, `kustomize *`, `kubectl --dry-run=* *`, `kubectl --context=kind-keese* *`, `kubectl {get,describe,logs} *`, `kind {get,create cluster --name=keese*,delete cluster --name=keese*}`, `tilt {up,down} --context=kind-keese*`, `helm {template,lint,upgrade --install --dry-run} *`, `helmfile {lint,diff,template} *`, `ctlptl {apply,get} *`, `kubeconform *`, `pluto *`, `kuttl *`, `fga *`, `stern *`, `k9s *`, `gosec *`, `govulncheck *`, `tofu {init,plan,validate,fmt} *` (OpenTofu read-only / plan-only).
- **ask +:** `kubectl {apply,delete,patch} *`, `docker {build,run} *`, `operator-sdk run *`, `helmfile sync *`, `tofu apply *`.
- **deny +:** `kubectl * --context=prod-*`, `kubectl * --context=*production*`, `kubectl * --context=*prd*`, `docker push*`, `helm install *` (prefer `upgrade --install`), `helm upgrade * --context=prod-*`, `operator-sdk run bundle * --index-image=*prod*`, `Read(**/kubeconfig)`, `Read(dev/bootstrap/**/secrets/**)`, `fga store delete*`, `kind delete clusters` (bare), `tofu {destroy,workspace delete} *`, `tofu apply * --context=prod-*`.

### CLAUDE.md keese edition — 15 task rows (add OpenTofu + IDE rows)
Adds rows for: Create CRD · Implement reconciler · Edit admission webhook · OLM bundle · Bootstrap local · Guardrail · OpenFGA model change · AgentRuntime provider · Envoy AI Gateway config · OTEL/logs · Goose recipe/extension · Open/close design gate · Score plan/design/spec · **Cloud deployment (OpenTofu)** → `deploy/opentofu/README.md` → `references/opentofu-cloud-deployment.md` · **IDE setup** → `references/ide-and-debugging.md` → `dev/ide/{goland,vscode}/`.

### Iteration log — iter 3 total **92/100** (SHIP)

---

## Phase 2 — Dev env (flake, direnv, Makefile, envs)

**Goal:** `direnv allow` once → every tool pinned and ready.

### flake.nix additions (after template's language-toolchain marker)
Go: `go_1_24`, `gopls`, `delve`, `golangci-lint`, `gotools`, `govulncheck`, `gofumpt`.
K8s: `kubectl`, `kubernetes-helm`, `helmfile`, `kustomize`, `kubebuilder`, `kind`, `ctlptl`, `tilt`, `stern`, `k9s`, `kubeconform`, `pluto`, `kuttl`*, `operator-sdk`*, `setup-envtest`*, `controller-gen`*, `cfssl`, `cmctl`*.
OpenTofu: `opentofu` (the `tofu` binary), `tflint`, `terraform-ls` (LSP; works with OpenTofu).
Aux: `crane`, `skopeo`, `opa`, `conftest` (OpenTofu policy), `argo-cli` (Argo Workflows).
(*) unverified in nixpkgs stable — implementer confirms; overlay fallback `nix/overlays/operator-tools.nix`.

### Makefile target grid (CI-load-bearing names; two files: keese wrapper + `Makefile.operator-sdk-generated`)
Standard targets plus new entries for this plan:
- `tofu-plan` — `tofu init && tofu plan` across `deploy/opentofu/{aws,gcp,azure}/`.
- `tofu-validate` — `tofu fmt -check && tofu validate && conftest test deploy/opentofu/`.
- `bundle-sign` — cosign keyless on bundle image (CI-only path).
- `ide-config` — stamps out GoLand + VSCode debug configs (`dev/ide/` → `.idea/`, `.vscode/`).
- `smoke` — P7 smoke (post-gate) and `smoke-ci` (CI variant without interactive pieces).
- All previous targets retained (`fmt`, `vet`, `lint`, `test-{unit,integration,e2e}`, `test`, `verify`, `manifests`, `generate`, `bundle`, `bundle-validate`, `docker-{build,push}`, `deploy`, `install`, `kind-up/down`, `tilt-up/down`, `bootstrap-infra`, `envtest-setup`, `vuln`, `tidy`).

### `.env.local.example` (keese-tailored, Apache-2.0 SPDX-headed)
Groups: worktree base · **Goose provider** (`GOOSE_PROVIDER`, `GOOSE_MODEL`, `ANTHROPIC_API_KEY`, optional `OPENAI_API_KEY`) · **OpenFGA** (`OPENFGA_API_URL`, `OPENFGA_STORE_ID`, `OPENFGA_AUTHORIZATION_MODEL_ID`) · **OpenBao** (`BAO_ADDR`, `BAO_TOKEN`) · **NATS** (`NATS_URL`) · **Envoy AI Gateway** (`EGRESS_GATEWAY_URL`, `EGRESS_EXT_AUTHZ_CLUSTER`, `EGRESS_SA_AUDIENCE_TEMPLATE`) · **OTEL** (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME=keese-operator`) · **Kind/Tilt** (`KIND_CLUSTER_NAME=keese-dev`, `TILT_HOST=127.0.0.1`) · **Registries** (`IMG`, `BUNDLE_IMG` commented) · **Envtest** (`KUBEBUILDER_ASSETS`) · **OpenTofu** (`TF_VAR_aws_region`, `TF_VAR_gcp_project`, `TF_VAR_azure_subscription_id` — commented) · **Elasticsearch** (`ELASTIC_USERNAME=elastic`, `ELASTIC_PASSWORD`) · **APM** (`APM_TOKEN`).

### Acceptance
`nix develop` prints banner; every Makefile target runs `--help` or no-op; `pre-commit run --all-files` green.

### Iteration log — iter 3 total **90/100** (SHIP)

---

## Phase 3 — Pre-commit hardening

**Goal:** Catch Go + K8s + OLM + OpenTofu errors before commit. Cache-friendly.

### Additions to `.pre-commit-config.yaml` (speed-tiered)
- **Go fast** (<1s): `gofumpt`, `goimports -local github.com/keese-ai/keese`, `go mod tidy -diff`.
- **Go slow**: `golangci-lint run --new-from-rev=HEAD~1` (on commit), `govulncheck ./...` (on pre-push; CI enforces).
- **K8s manifests**: `controller-gen` freshness via `scripts/check-controller-gen.sh`; `kubeconform -strict`; `pluto detect-files` against K8s 1.30.0 + 1.31.0; `scripts/check-kustomize-overlays.sh`; `scripts/check-crd-validation.sh` (envtest API server); `scripts/check-rebac-markers.sh` (every `// +keese:rebac-tuple=...` field); `scripts/check-netpol-wildcards.sh`.
- **OLM**: `operator-sdk bundle validate --select-optional suite=operatorframework` scoped `files: ^bundle/`.
- **OpenTofu**: `tofu fmt -check -recursive`, `tofu validate` in each module, `conftest test deploy/opentofu/` (OPA Rego policies under `policy/opentofu/`).
- **YAML**: `yamllint` scoped `^(config/|bundle/|dev/|deploy/)`.
- **SIGNAL-HANDLING check** (new): `scripts/check-signal-handling.sh` — greps every `main.go` + every long-running binary under `cmd/` for presence of a SIGTERM handler (`signal.Notify(c, syscall.SIGTERM)`); fails if absent.

### `scripts/check-crd-validation.sh`, `scripts/check-rebac-markers.sh`, `scripts/check-signal-handling.sh`
All under `set -euo pipefail`, source `scripts/lib/{paths,log}.sh`, bounded readiness, cleanup traps. Fixtures at `hack/testdata/` prove each fails on a bad input. Scripts tested via `test/scripts/*.bats` run in CI `lint.yaml`.

### `.golangci.yml` baseline (2026)
As before: `errcheck, govet, ineffassign, staticcheck, unused, gofumpt, goimports, revive, gocritic (diagnostic+performance), gosec, bodyclose, rowserrcheck, nilerr, errorlint, copyloopvar, contextcheck, forbidigo (no fmt.Print*, panic, time.Sleep), noctx, prealloc, unparam, tparallel`. Disabled: `exportloopref` (removed), `golint` (deprecated).

### Acceptance
All hooks pass on empty scaffold; seeded bad samples fail loudly; timing emitted via log.sh timer.

### Iteration log — iter 3 total **89/100** (SHIP)

---

## Phase 4 — Docs skeleton — **designs first, specs after**

**Goal:** Every design MUST exist before any spec. Every design < 200 lines (split across sub-docs if needed). User rule: "Build complete designs before starting on specs."

### Design stubs (22 docs — reorganized + expanded)

Each: SPDX (Apache-2.0) + frontmatter (`scope: design`, `category`, `depends`, `related_skills`, `status: draft`, `last_verified`, `rollback`) + `TODO(design-gate)` sentinel + 3–5 open questions + refs.

**Foundations:**
- `01-tenancy-capsule.md` — Capsule direct usage; vcluster opt-in via `Workspace.spec.isolation`; namespace layout per tenant; quota/LimitRange/PSS defaults.
- `02-workspace-model.md` — single-pod vs pod-per-agent; PVC model; idle eviction; scheduling (nodeSelector/toleration); status FSM.
- `03-workflow-argo-delegation.md` — Argo `WorkflowTemplate` projection; `WorkflowRun` vs Argo `Workflow` mapping; artifact passing; retry budget. *(was 03-workflow-model)*

**Identity & authz (split for < 200 lines each):**
- `04a-openfga-authz-model.md` — tuple shapes, relations, check semantics, consistency strategy (eventual vs HIGHER_CONSISTENCY per-call).
- `04b-projected-sa-identity.md` — projected SA tokens per-tenant audience, TTL policy, OIDC trust anchoring.
- `04c-token-revocation.md` — revocation latency SLO, version-tagged caches, fail-closed on OpenFGA down.

**Egress / credential broker (split):**
- `05a-envoy-ai-gateway-topology.md` — gateway deployment per-cluster / optional per-tenant; MCPRoute + AIGatewayRoute shape; ext_authz → OpenFGA.
- `05b-credential-injection-patterns.md` — three-table decomposition (Route / BackendSecurityPolicy / OpenFGA); static vs OIDC-STS vs dynamic creds.
- `05c-mcp-policy-enforcement.md` — CEL per-tool policies; request.mcp.tool/method semantics; how `ToolAllowList` (ConfigMap) projects to MCPRoute rules.

**Guardrails (composed):**
- `06-guardrailbinding.md` — role model (cluster/tenant/workspace admin), default binding, strictest-wins merge lattice, composition across Kyverno + OpenFGA + Envoy SecurityPolicy + recipe hooks + TokenBudget.

**Runtime SPI + goose:**
- `07-agent-runtime-spi.md` — Go interface, capability matrix, SemVer via apidiff, lifecycle ownership.
- `08a-goose-headless-modes.md` — recipe-run vs `goose serve` ACP; selection criteria; resource sizing.
- `08b-goose-acp-stdio-k8s.md` — stdio transport in K8s (no-port problem); `kubectl exec` bridge; `kubectl-keese attach` plugin.
- `08c-goose-subagents-limits.md` — 10-concurrent sub-agent ceiling; enforcement via ReBAC-at-spawn.

**Transport:**
- `09-transport-crd.md` — `spec.type: nats|a2a|mcp|stdio` enum semantics; delivery (at-least-once, dedup owner); TLS via cert-manager.

**Observability:**
- `10a-otel-topology.md` — Deployment collector; pipelines (traces → APM, metrics+logs → ES); sampling.
- `10b-token-accounting.md` — TokenBudget CR enforcement via Envoy AI GW cost filter + OTEL; billing export.

**Secrets:**
- `11-secrets-pluggable-vault.md` — OpenBao local / cloud KMS providers; ExternalSecrets Operator bridge; rotation model.

**Network isolation:**
- `12-network-isolation.md` — default-deny per workspace; ReferenceGrant opt-in cross-ns; operator egress.

**CLI tunnel:**
- `13-cli-tunnel-wireguard.md` — WireGuard tunnel for human → workspace attach; SA-auth + OIDC.

**OLM packaging (split):**
- `14a-olm-channels-upgrades.md` — `alpha`/`beta`/`stable` channels, `replaces` chain.
- `14b-olm-dependencies.md` — declared deps on cert-manager / Envoy Gateway / Envoy AI Gateway / Capsule / NACK / ECK / Argo operators; webhook CA bootstrap.

**Memory + recipes (new, per research):**
- `15-memory-management.md` — `Memory` CRD discriminated one-of (sqlite/redis/qdrant/pgvector/neo4j/mem0/zep); `SharedMemory` with ReBAC gates; goose MCP-memory integration story.
- `16-recipe-distribution.md` — `RecipeSource` (OCI-first) + `Recipe` CRD; cosign signature verification; admission validation against workspace entitlements.

**Credential broker detail (new):**
- `17-credential-broker.md` — explicit caching tiers, failure table, refresh ladder, OpenBao/ExternalSecrets integration.

**Signal handling + lifecycle (new):**
- `18-process-lifecycle.md` — SIGTERM drain patterns for controllers and agents; SIGKILL recovery invariants; checkpoint locations.

**IDE / debugging (new):**
- `19-ide-and-debugging.md` — GoLand primary + VSCode secondary; `kubectl-keese attach`; Workspace status debugging fields; dlv-in-kind.

**API surface:**
- `20-api-group-layout.md` — which kinds in which group under `operator.keese.ai`; version strategy (v1alpha1 → v1beta1 requires conversion webhook); shared-types package.

**Cloud deployment (new):**
- `21-opentofu-cloud-deployment.md` — per-cloud modules (EKS/GKE/AKS + secret manager + IAM/WI + DNS); keese OLM install step; state backend (S3/GCS/Azure Storage with encryption + locking).

**Workflow composition examples (new):**
- `22-workflow-composition-examples.md` — concrete Workflow patterns (cron-triggered autonomous-dev pipeline; webhook-triggered PR review; NATS-fanout summarizer/reviewer); how `.triggers[]`, `.outputs[]`, Argo, NATS, Knative compose.

### Gate order (enforced)
1. All 22 design docs reach `status: current` + rubric score ≥ 90 (rev-3 iteration each).
2. **THEN** 9 spec docs authored:
   - `keese.ai-v1alpha1.md`
   - `keese.ai-v1alpha1.md`
   - `keese.ai-v1alpha1.md`
   - `keese.ai-v1alpha1.md`
   - `keese.ai-v1alpha1.md`
   - `authz.keese.ai-v1alpha1.md`
   - `policy.keese.ai-v1alpha1.md`
   - `keese.ai-v1alpha1.md`
   - `agent-runtime-spi.md` + `egress-authz-protocol.md` + `credential-broker-protocol.md` (SPI/contracts, not CRD specs).
3. **THEN** controller implementation may begin (per P8 gate).

Each spec frontmatter: `scope: spec`, `status`, `tests: { unit, envtest, kuttl }`, `metrics: [...]`, `events: [...]`, `regression_lock: false` (flips to `true` on impl land).

### References (copy + 6 new)
Copy template refs. New: `references/envtest-kuttl-harness.md`, `tilt-local-loop.md`, `olm-bundle-authoring.md`, `crd-design-checklist.md`, **`opentofu-cloud-deployment.md`**, **`ide-and-debugging.md`**.

### `docs/plans/README.md` — gate-status banner + phase index + parallel groups + gate-check reference.

### Iteration log — iter 3 total **93/100** (SHIP)

---

## Phase 5 — CI/CD

**Goal:** Every PR lint+test+bundle+image+sign+scorecard; cloud-deploy dry-run on tf changes.

### Workflows (11, up from 10)
| Workflow | Triggers | Perms |
|---|---|---|
| `commitlint.yaml` | PR | contents:read |
| `lint.yaml` | PR, push main | contents:read |
| `test.yaml` | PR, push main; matrix go 1.24 × k8s {1.29, 1.30, 1.31} | contents:read |
| `e2e.yaml` | nightly, tag, dispatch; matrix k8s {1.30, 1.31} | contents:read, packages:read |
| `bundle.yaml` | push main, tag `v*` | packages:write, id-token:write |
| `image.yaml` | tag `v*`; arch matrix amd64/arm64 single manifest | packages:write, id-token:write |
| `docs.yaml` | docs PRs, push main | pages:write on push |
| `scorecard.yaml` | weekly, push main, branch_protection_rule | security-events:write, id-token:write |
| `release.yaml` | push main (conv-commit filter) — **release-please action** | contents:write, pull-requests:write |
| `design-gate.yaml` | PR touching api/controller/docs | contents:read |
| **NEW `opentofu.yaml`** | PR touching `deploy/opentofu/**` | `tofu fmt -check`, `tofu validate`, `tofu plan` (read-only; artifacts posted as PR comments); `conftest` against `policy/opentofu/`; **no `tofu apply`** in CI except on release pipelines with manual approval. | contents:read, id-token:write (OIDC to cloud for plan-only). |

### Cosign keyless OIDC
`sigstore/cosign-installer@v3` → `docker/build-push-action@v6` (push + provenance + sbom) → `cosign sign --yes image@digest` → `syft -o spdx-json` + `cosign attest --type spdxjson`.

### Release tool: **release-please** (D15) — rationale locked.

### OpenTofu in CI
- Uses GitHub OIDC → AWS/GCP/Azure (no long-lived cloud keys in repo).
- State backend: S3+DynamoDB / GCS / Azure Storage with lock.
- `tofu apply` requires manual approval via environment protection rules (`environments: cloud-prod`).

### Iteration log — iter 3 total **90/100** (SHIP)

---

## Phase 6 — Go operator scaffold (13 empty-stub kinds)

**Goal:** `operator-sdk init` + `create api` run for the 13 kinds. Every `*_types.go` + `*_controller.go` is an empty `TODO(design-gate)` stub (≤ 20 LOC each).

### Init
```
operator-sdk init \
  --domain=operator.keese.ai \
  --repo=github.com/keese-ai/keese \
  --plugins=go/v4 \
  --project-name=keese
```

### `create api` sequence (13 kinds across 8 groups)
```
workspace/v1alpha1:     Workspace, WorkspaceShare
workflow/v1alpha1:      Workflow, WorkflowRun
runtime/v1alpha1:       AgentRuntime, RuntimeExtension
memory/v1alpha1:        Memory, SharedMemory
recipe/v1alpha1:        Recipe, RecipeSource
guardrail/v1alpha1:     GuardrailBinding
observability/v1alpha1: TokenBudget
transport/v1alpha1:     Transport
```
Command per kind: `operator-sdk create api --group=<g> --version=v1alpha1 --kind=<K> --resource --controller`. Idempotent re-run guard `scripts/guard-create-api.sh`. PROJECT pre-set `multigroup: true`.

### Directory layout
Standard SDK + new: `deploy/opentofu/{aws,gcp,azure}/`, `dev/ide/{goland,vscode}/`, `hack/{envtest-apiserver/, testdata/}`, `policy/opentofu/` (OPA Rego).

### Admission strategy (D16)
**VAP (CEL) first** for static-field immutability on every CRD (Workspace tenant binding, AgentRuntime kind, Entitlement triple, Constitution hash, Transport type, etc.). **Webhooks** only where CEL insufficient:
- **Validating webhook** (cross-resource): `GuardrailBinding` (merge-lattice weakening check against tenant/default), `WorkspaceShare` (ReferenceGrant existence check), `Recipe` (digest-signature + entitlement gate), `Memory` (provider one-of schema).
- **Mutating webhook** (defaulting): `Workspace` (default SA, NetworkPolicy label, NP inject, GuardrailBinding inherit `default`), `Workflow` (TTL, concurrency), `AgentRuntime` (imagePullPolicy, SA projection), sample generators.

No conversion webhooks at v1alpha1 (added at v1beta1 promotion per rule in design 20).

### go.mod — vendor only what compiles
Keep SDK defaults + `github.com/stretchr/testify`. Defer `openfga/go-sdk`, `openbao/api`, `nats.go`, `otel/*`, `goose/acp-go`, `envoyproxy/gateway` client, `argoproj/argo-workflows` client, `capsule-proxy` client — add each when its controller lands.

### PROJECT file — default multi-group + SDK plugins; no custom plugins at v1alpha1.

### Acceptance
`make manifests generate test` passes; `operator-sdk bundle validate ./bundle` passes; `grep -rn 'TODO(design-gate)' api/ internal/` = 26 markers (13 kinds × types + controllers).

### Iteration log — iter 3 total **92/100** (SHIP)

---

## Phase 7 — Local infra bootstrap

**Goal:** `make tilt-up` brings up kind + full dev stack in ≤ 5 min.

### Kind topology (1 control + 3 workers, K8s 1.30.x, ctlptl-managed)
- worker-1 (`system`): infra pods (tainted).
- worker-2 (`tenant-a`), worker-3 (`tenant-b` / `isolated` tainted).
- `dev/kind/ctlptl.yaml` + `dev/kind/kind-config.yaml` (PodSecurity feature gate; portMappings 80/443/9200/5601/8200/8080/4318; containerd persistence).

### Helmfile (dev dep layer) — new components added
`dev/bootstrap/helmfile.yaml` `needs:`-ordered:
- **cert-manager** v1.15.x (+ self-signed + CA Certificate + CA ClusterIssuer).
- **Capsule** v0.7+ (CNCF) — reconciles `Tenant` → namespaces + policies.
- **Envoy Gateway** v1.5+ (operator + GatewayClass).
- **Envoy AI Gateway** v0.5.x (MCPRoute + AIGatewayRoute CRDs).
- **OpenFGA** Deployment + seed Job (`dev/bootstrap/openfga/model.fga`).
- **NACK** (NATS controllers for K8s) + NATS server with JetStream + seeded Streams/Consumers.
- **ECK operator** + 1-node Elasticsearch (`-Xms/-Xmx=512m`, mmap off) + Kibana + APM Server + fluent-bit DaemonSet.
- **OpenBao** PVC-backed (not `-dev`) + one-shot init + seed secrets.
- **ExternalSecrets Operator** (or OpenBao Secrets Operator) — bridges OpenBao → K8s Secret → `BackendSecurityPolicy`.
- **Argo Workflows** (workflow-controller + server UI); required by keese `Workflow` → Argo delegation (D20).
- **Qdrant** (Helm) — default vector DB for `Memory.spec.provider.qdrant` (dev-mode 1 node, emptyDir).
- **Kyverno** — for Kyverno-side guardrails composed into GuardrailBinding.
- **OTEL collector** Deployment (OTLP gRPC :4317 / HTTP :4318; processors: batch, resource, tail_sampling; exporters: elasticsearch + elasticapm + debug + prom remote-write stub).

### Boot-order DAG
```
cert-manager
  ├── Capsule
  ├── Envoy Gateway → Envoy AI Gateway → (MCPRoute + AIGatewayRoute + BackendSecurityPolicy CRDs)
  ├── OpenFGA (+seed) + Kyverno
  ├── NACK → NATS (+seed) + Argo Workflows
  ├── ECK → ES + Kibana + APM + fluent-bit
  ├── OpenBao (PVC + init + seed) → ExternalSecrets Operator
  └── Qdrant
       ↓
    OTEL collector
       ↓
    keese operator (Tilt-live-reloaded)
```

### Operator hot-reload
Go build on host (`-gcflags='all=-N -l'`) → Tilt `live_update: sync('./bin/manager','/manager')` + restart → dlv listens :2345. Feedback target 5–12s typical, <15s worst.

### OpenBao seed
PVC-backed. Seed script `dev/bootstrap/openbao/seed.sh` enables `kv-v2` at `keese/`, writes `keese/tenants/tenant-a/anthropic api_key=sk-...`, `keese/tenants/tenant-a/github pat=...`. ExternalSecret CRs project these to `BackendSecurityPolicy`-referenced K8s Secrets. Revocation via version bumps.

### Signal-handling smoke
`scripts/dev/sigterm-drain-test.sh` — SIGTERMs the keese operator pod and asserts (a) the reconcile queue drains within 30s, (b) leader lease is released, (c) exit code 0. Similar for an agent pod running goose — asserts session is checkpointed to SQLite before exit.

### Sample CRs (`dev/samples/`)
Post-gate smoke targets:
1. `capsule-tenant-alpha.yaml` — bare Capsule `Tenant`.
2. `workspace-research.yaml` — keese `Workspace` in tenant-alpha, `runtimeRef: goose-default`, `memoryRefs: [ws-sessions-sqlite, tenant-knowledge-qdrant]`, `guardrailBindingRef: tenant-alpha-strict`.
3. `workflow-summarize-then-review.yaml` — `Workflow` with Argo projection, two steps connected via NATS stream, triggers (cron + webhook), outputs (slack + s3).
4. `recipe-source-github.yaml` + `recipe-pr-reviewer.yaml` — `RecipeSource` pointing at OCI registry; pulled + gated against workspace `ToolAllowList`.
5. `guardrail-binding-strict.yaml` — binds Kyverno policies + OpenFGA tuple ConfigMap + Envoy SecurityPolicy + recipe hooks.
6. `sharedmemory-team-kb.yaml` — cross-workspace `SharedMemory` with ReBAC gate.

### `make smoke` (post-gate)
Asserts tenant ready, workspace ready, workflow run succeeded, OpenFGA `check` returns `allowed=true` for authorized tool, negative case for disallowed tool returns `.status.phase=Denied` with OpenFGA log entry, ES has workflow logs, APM has operator trace, token usage decremented on `TokenBudget`, SIGTERM drain test green.

### Cloud parity dry-run (new — D18)
`make tofu-plan` runs OpenTofu `plan` against each cloud module; fails phase acceptance on any drift from committed plan snapshot (helps spot laptop-only dev-isms).

### Iteration log — iter 3 total **91/100** (SHIP)

---

## Phase 8 — Design-gate freeze enforcement

**Goal:** No implementation lands until all 62 designs AND all 13 specs score ≥ 90 AND architect opens gate.

### `scripts/check-design-gate.sh`
"Stub body" iff file contains `TODO(design-gate)` + ≤ 20 non-blank non-comment LOC. For every non-stub file, find matching design + spec in `docs/designs/` + `docs/specs/`, confirm `status: current` OR `regression_lock: true`, confirm top iter-log score ≥ 90. Exit non-zero with reason list. Additional check: if ANY `docs/specs/*.md` exists with `status != draft` BEFORE all `docs/designs/*.md` reach `current` → fail ("specs cannot be authored until designs complete").

### `regression_lock` field (frontmatter)
Blocks spec status downgrade unless same commit adds `docs/plans/migration-<slug>.md`. Script validates both directions (ship & retract).

### `.github/workflows/design-gate.yaml`
Triggers on PRs touching `api/**`, `internal/controller/**`, `docs/{designs,specs,plans}/**`. Runs `bash scripts/check-design-gate.sh`, captures to `$GITHUB_STEP_SUMMARY`, posts sticky PR comment via `<!-- keese-design-gate -->` marker. Branch-protection required check `design-gate / check`.

### `verify-gate-commit.yaml`
Verifies `open-gate` commit signed by an architect identity (GPG key in org); bats tests at `test/scripts/check-design-gate.bats` run in `lint.yaml`.

### Exit criteria
- All 62 designs + 13 specs score ≥ 90.
- `docs/plans/README.md` frontmatter flips `gate_status: open`.
- Architect-signed commit: `docs(architecture): open design gate — 62 designs + 13 specs ≥ 90/100`.
- Required check green on `main`.

### Iteration log — iter 3 total **91/100** (SHIP)

---

## Cross-phase critical files

| Purpose | Path |
|---|---|
| Rubric | `docs/plans/rubric.md` |
| Plan index + gate status | `docs/plans/README.md` |
| CLAUDE.md keese | `CLAUDE.md` |
| Pre-commit | `.pre-commit-config.yaml` |
| Dev shell | `flake.nix` |
| Operator Makefile | `Makefile` |
| Operator SDK manifest | `PROJECT` (multigroup:true; domain: operator.keese.ai) |
| Tiltfile | `Tiltfile` |
| Helmfile | `dev/bootstrap/helmfile.yaml` |
| Kind cluster (ctlptl) | `dev/kind/ctlptl.yaml` + `kind-config.yaml` |
| OpenFGA model | `dev/bootstrap/openfga/model.fga` |
| Zero-trust rules | `.claude/rules/05-security-zero-trust.md` |
| K8s rules | `.claude/rules/04-kubernetes.md` |
| Signal-handling rules | `.claude/rules/06-signal-handling.md` |
| Design-gate check | `scripts/check-design-gate.sh` |
| OpenTofu modules | `deploy/opentofu/{aws,gcp,azure}/` |
| IDE configs | `dev/ide/{goland,vscode}/` |
| License | `LICENSE` (Apache-2.0) |

---

## End-to-end verification (after all 9 phases)

1. Fresh checkout → `direnv allow` → nix shell → `pre-commit run --all-files` green.
2. `make kind-up bootstrap-infra tilt-up` — full stack healthy ≤ 5 min.
3. `kubectl get crd | grep -E 'keese\.ai'` — CRDs across 3 groups (`keese.ai`, `authz.keese.ai`, `policy.keese.ai`).
4. `make test` — envtest + empty-reconciler idempotency green.
5. `make bundle bundle-validate` — OLM bundle valid.
6. `make tofu-validate tofu-plan` — cloud modules lint + plan (no apply).
7. No-op PR — all 11 GH workflows green.
8. Writing non-stub body to `internal/controller/workspace/workspace_controller.go` → design-gate hook + CI block commit.
9. `grep -rn 'TODO(design-gate)' api/ internal/` → 26 markers.
10. SIGTERM drain test on operator pod → clean exit within 30s.
11. Architect walks 62 designs + 13 specs, scores each ≥ 90, commits gate-open → gate flips to `open`.

---

## Meta-score: this plan (iter-3)

| # | Category | Wt | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | 9 phases + hard gate + 13 kinds + 62 designs + 13 specs explicit. |
| 2 | Architecture fit | 10 | 1.0 | 10 | 23 locked decisions; composition-first; upstream primitives preserved. |
| 3 | Security posture | 15 | 1.0 | 15 | Zero-trust, three-table credential decomposition, SIGTERM drain, SSA fieldOwner. |
| 4 | Automatability | 10 | 1.0 | 10 | Every step behind make/script; CI 11-workflow matrix. |
| 5 | Verifiability | 15 | 0.95 | 14.25 | Acceptance + rubric + iter-logs; smoke post-gate; SIGTERM test added. |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | SIGKILL recovery, OpenFGA-down fail-closed, regression_lock, OpenBao PVC, SPIRE defer documented. |
| 7 | Context efficiency | 10 | 1.0 | 10 | Stable CLAUDE.md, rules-always, skills-on-demand, designs < 200 lines. |
| 8 | Docs quality | 5 | 1.0 | 5 | Apache-2.0 SPDX everywhere; frontmatter complete; design-first-spec-after rule; cross-link discipline. |
| 9 | Observability | 5 | 1.0 | 5 | OTEL + ECK + APM + token-accounting + Prom remote-write; logged ext_authz decisions. |
| 10 | Operational readiness | 10 | 1.0 | 10 | OLM + branch protection + rollback hooks + bootstrap timing gate + OpenTofu plan-only-in-CI. |
| | **Total** | 100 | | **99.25** | **Verdict: SHIP.** |

---

## Appendix: phase → Claude agent assignment

| Phase | Primary agent(s) | Worktree |
|---|---|---|
| P0 | implementer (sonnet) | — (root files) |
| P1 | architect (opus) + implementer (sonnet) | `agent/p1-claude` |
| P2 | implementer (sonnet) | `agent/p2-devenv` |
| P3 | implementer (sonnet) + security-reviewer (sonnet) | `agent/p3-hooks` |
| P4 | architect (opus) + rebac-modeler (opus) + guardrail-author (sonnet) + plan-scorer (sonnet) | `agent/p4-designs` → `agent/p4-specs` |
| P5 | implementer (sonnet) | `agent/p5-cicd` |
| P6 | crd-author (sonnet for stubs) | `agent/p6-apis` |
| P7 | infra-bootstrap (sonnet) + debugger (haiku→sonnet) | `agent/p7-infra` |
| P8 | architect (opus) + plan-scorer (sonnet) | — (solo) |

---

## Iteration history (meta)

- **Iter 1 (2026-04-19 10:00):** Initial 9-phase scaffold + rubric + D1–D14. Mean phase score 86.7; P7 at 82 (REVISE).
- **Iter 2 (2026-04-19 11:30):** Integrated 3 Plan-agent outputs; added D15 (release-please), D16 (VAP+SSA), D17 (APM-dev). Resolved kind count to 18. P7 → 92. Meta 98.
- **Iter 3 (2026-04-19 14:00):** User feedback integrated — API group `*.operator.keese.ai`, Capsule direct (drop Tenant CRD), guardrail consolidation to `GuardrailBinding`, 13 kinds (down from 18) across 8 groups. Envoy AI Gateway (not plain Envoy GW) + MCPRoute + BackendSecurityPolicy. Three-table credential decomposition (D13 revision). OpenTofu for cloud (D18). GoLand primary IDE (D19). Argo Workflows delegation (D20). SIGTERM drain rules (D21, new rule 06). Model-discipline per agent (D22). Compose-over-replicate (D23). 22 design docs (split for < 200 lines); designs-before-specs gate. Meta 99.25 (SHIP).
- **Iter 3.1 (2026-04-19 post-review):** License reverted to **Apache-2.0** per user. SPDX + `addlicense` config stay on `apache`; no other changes.
