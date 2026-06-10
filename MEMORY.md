<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# keese — Memory

MEMORY.md is a pointer index of **decisions made** and **gotchas hit**.
Keep it scannable. One line per entry: `- [Short title](path/to/detail.md) — one-sentence hook.`
If an entry needs more than two lines, write into `docs/references/` or `docs/designs/` and link here.

Update at the end of a sub-phase or after a surprising discovery. Do not use this file for
ephemeral task state — that belongs in a plan or a TodoWrite list.

## Decisions

### 2026-06-09 — e2e-hardening track: foundation (EH1–EH4)

- Plan [e2e-hardening/README.md](docs/plans/e2e-hardening/README.md) (EH1–EH14,
  conductor waves) closes the 2026-06-08 e2e/coverage-gap analysis. Wave 1 merged.
- **EH2 coverage ratchet:** per-package floors in `test/coverage-targets.yaml`;
  `make coverage-check` (in `verify`) fails below floor — lowering one is a
  reviewable diff. Controller floors are low because their behavioural tests are
  `integration`-tagged (`-short` only hits helpers); `podexec`/`adkgo`/`adkpython`/
  `wflauncher` pinned at 0.0 (EH13 raises). CI wiring pending in EH1.
- **EH4 live ReBAC e2e** ([tests/e2e/rebac-decision/](tests/e2e/rebac-decision/)):
  ext_authz allow→200, deny→403 (empty `egress.allowedTools` = fail-closed),
  token-free deny audit (rule 05.10), allow→deny revoke flip within cache TTL.
- **Local-only gotcha:** macOS Go 1.26 + nix `make test` fails to link CGO
  controller test binaries (`_SecTrustCopyCertificateChain`) — use `CGO_ENABLED=0`
  locally; Linux CI unaffected. Go `test/e2e` BeforeSuite needs docker buildx.
- **Conductor gotcha:** Agent-tool worktrees surfaced ~62 spurious `make`-regen
  uncommitted files (pool seeding); committed diffs were clean, so
  `git checkout -- .` in the worktree before `worktree-merge.sh --no-verify-green`.
- **EH3 SIGTERM wiring:** `make sigterm-drain-test` enforces rule 06 §10 across the
  long-running `cmd` pods; `keese-drain` got a Go SIGTERM test via a behavior-neutral
  `run()` seam. Per-binary Go tests for the operator/authz/cosign-webhook/wf-launcher
  are a follow-up (outside EH3's footprint).
- **EH1 CI wiring (foundation done):** `e2e.yaml` installs kuttl (pinned v0.15.0 —
  the nightly was silently broken on the bare runner) + runs `make sigterm-drain-test`;
  `test.yaml` gates `make coverage-check` on PRs. Optional skip-hardening + PR-smoke
  deferred.

### 2026-06-09 — e2e-hardening wave 2 (EH5–EH7): authz/policy e2e + impl gaps

- Suites added, all **`shipped-with-stubs`** (live CRD/tuple/projection layers tested;
  not-yet-wired paths gated behind env flags): `tests/e2e/authz-guardrails/` (EH5),
  `cross-tenant/` (EH6), `token-budget/` (EH7). Request-firing primitives now live in
  `tests/e2e/lib/fire-request.sh` (sourced, not copied; EH4's inline copy untouched).
- **CTA revocation is expiry-driven, not delete-driven:** the controller removes the
  trust tuple via `transitionToExpired`→`Rebac.Delete`; `cleanup()` on delete only
  drops the NATS stream + finalizer. Tests drive revocation through expiry.
- **Controller follow-ups these e2e tests surfaced** (impl gaps, NOT test defects):
  1. `internal/authz/extauth/resolver.go` resolves `can_call` but not `messageable_from`
     → cross-tenant request decisions not live (EH6 gate `CROSS_TENANT_DECISION_LIVE=1`).
  2. No OTEL token-cost metering processor feeding consumed tokens back to the
     rate-limiter → over-budget 429 can't fire (EH7; the rate-limit projection IS live).
  3. Guardrail ext_proc (Presidio/LlamaGuard) absent from the bootstrap (EH5 gate
     `GUARDRAIL_EXTPROC=1`).
  4. Only `GuardrailBinding` has a reconciler; `ToolBinding`/`WorkspaceTool` are
     request-time catalogue objects — the `tool#allowed_in@workspace` tuple is written
     by the **Workspace** controller from `egress.allowedTools`.
  5. Bootstrap default GuardrailBinding ships `keese-default` but the controller's
     `defaultBindingName` const is `keese.ai-default` → non-fatal `DefaultBindingMissing`.

### 2026-06-09 — e2e-hardening wave 3 (EH8–EH10): featuregate/workflow/real-drain

- Suites (all `shipped-with-stubs`): `tests/e2e/feature-gate/` (EH8 — gate flip via
  the `keese-features` projection ConfigMap), `workflow/` (EH9 — real Workflow/
  WorkflowRun → live Argo `argosay` → Succeeded + concurrency + cascade GC), and the
  reworked `agentruntime-drain/` (EH10 — real `keese-drain` under SIGTERM, orphan
  prereq fixed, self-contained fixture).
- **More controller/spec follow-ups surfaced:**
  - `Workflow.status.runCount` is defined + printed but **never written** by the
    reconciler (EH9 skips the increment assert; `revisit_when_workflow_run_count_live`).
  - **Fixed** the Workflow finalizer spec drift: `keese.ai-v1alpha1-workflow.md` now
    reads `finalizers.workflow.keese.ai/cascade` (matching the controller), not
    `workflowtemplate-gc`.
  - FeatureGate admission-outcome flip + real-drain in-cluster run need bootstrap
    wiring (OLM + cosign-webhook overlay; `make goose-runtime-load`) — both gated.

### 2026-06-10 — e2e-hardening final wave (EH11–EH14): track complete (14/14)

- `tests/e2e/recipe-source/` + `runtime-extension/` (EH11, shipped-with-stubs);
  retired the kubebuilder scaffold `test/e2e/` + deleted the now-orphaned
  `test/utils/` (EH12 — kuttl `tests/e2e/` is the only e2e); unit tests for
  `adkgo`/`adkpython`/`podexec`, EH2 floors ratcheted 0.0→100/100/91 (EH13);
  FeatureGate envtest idempotency, policy suite 25/25 (EH14).
- **More follow-ups (impl/bugs, NOT test defects):**
  - `RuntimeExtension` reconciler never calls `WriteExtensionEnabledIn` (defined in
    `runtime_rebac.go`, only tests call it) → the extension→workspace `enabled_in`
    tuple is **unwired**; the reconciler writes only the `owner` tuple.
  - `internal/runtime/podexec/podexec.go:65` **data race** on the context-timeout
    path (background copy goroutine writes the buffers `Exec` reads); `-race` flags
    it. Low severity; fix with errgroup/WaitGroup.
  - **Pre-existing** `FakeNatsSignaler` `-race` in the tokenbudget test harness
    (`internal/controller/policy/nats.go` unguarded fields) — on `main`, keeps
    `-race -tags=integration ./internal/controller/policy/...` red until a mutex lands.
- **Track complete (14/14).** e2e grew from 8 suites (no authz/policy, no live
  ReBAC, busybox drain) to 14 suites + unit/envtest coverage; CI installs kuttl
  (nightly fixed), gates the coverage ratchet on PRs, runs the sigterm-drain
  contract. The consolidated controller-gap list across the wave entries is the
  natural next controller-author track.

### 2026-06-08 — Conductor: parallel-build orchestrator adopted

- Ported the `conductor/` wave orchestrator (scheduler · footprint-coloring ·
  run ledger · budget guard · review-fix · worktree refresh · status dashboard)
  into keese; drive it from chat with `/conduct` (`/workflows` to watch). Design:
  [29-conductor-orchestration.md](docs/designs/29-conductor-orchestration.md)
  (status `draft` — score to ≥90 before promoting); how-to:
  [conductor/README.md](conductor/README.md); autonomy + protected paths:
  [.claude/rules/07-autonomy.md](.claude/rules/07-autonomy.md).
- keese-specific adaptation: footprint predictor tuned for `api:<group>/<kind>`,
  `ctrl:<group>/<kind>`, and HOT shared paths (`go.mod`/`PROJECT`, generated
  deepcopy `HOT:gen:<group>`, OLM CSV `HOT:olm`, OpenFGA model `HOT:rebac`) so
  CRD/OLM/ReBAC phases serialize correctly; green gate is `make lint && make test`;
  phases route to SPECIALIZED personas via a phase-doc `agent:` field
  (crd-author / controller-author / olm-author / rebac-modeler / …).
- Migrated the **expansion** track (`docs/plans/expansion/E*.md`) to the conductor
  frontmatter schema (`phase`/`model_tier`/`depends_on`/`agent`/`outputs`); the
  demo track + historical P-phases were intentionally NOT migrated (shipped /
  in-progress / deferred — not the parallel-build target). All 13 agents gained
  `effort:` frontmatter + a "Conductor participation" section.
- Retired `scripts/{agent-dispatch,worktree-merge}.sh` → superseded by
  `conductor/` versions; `CLAUDE_CODE_SUBAGENT_MODEL` flipped `sonnet`→`inherit`
  so per-agent model/effort is honored. `make conductor-test` gates the orchestrator.
  Follow-up remaining: score ADR 29 to `current` (still `draft`).
- 2026-06-08 staleness sweep (after the conductor adoption): repointed all live
  `scripts/{agent-dispatch,worktree-merge}.sh` references to `conductor/` in
  `book/docs/development/multi-agent.md` (+ conductor-scope note & protected-path
  list), `dev-environment.md`, `.env.local.example`, `.gitignore`, plus the
  `scaffolding-plan.md` + this file's 2026-05-06 references (current paths only —
  no historical-record carve-outs).
  Status fixes: E0 `planned`→`shipped` (ADK skeletons + CRD variants landed,
  verified in `agentruntime_controller.go` detectProvider + tests); RAG spec index
  row `draft`→`current`; runtime spec gained `adkPython`/`adkGo` + 5-way CEL +
  `cmd/main.go`; `last_verified` bumped on plans/specs READMEs, memory + runtime
  specs. Fixed LICENSE copyright `Aviz Networks, Inc.`→`keese-ai` (rule 01).
- Designs `20a`/`20b` describe the per-kind status convention as built: 3-group
  layout (`keese.ai`/`authz.keese.ai`/`policy.keese.ai`, packages
  `api/{keese,authz,policy}/v1alpha1`); each kind declares status inline with its
  own `<Kind>Phase` enum (no shared status base); the only shared types are
  `LocalObjectReference` + `ConcurrencyPolicy` in
  `api/keese/v1alpha1/common_types.go`.

### 2026-05-29 — Documentation overhaul: `book/` site + `docs/features/` tree

- Added the user-facing **mkdocs-material site** at `book/` (79 pages: Home, Getting
  Started, Concepts, Guides, Reference, Scenarios, Development; 150+ inline Mermaid
  diagrams). `cd book && mkdocs build --strict` passes (0 warnings); CI `docs.yaml`
  deploys it. **Local build gotcha:** the system `mkdocs` is a py3.12 nix build that
  breaks under py3.14 (`regex._regex`); use a fresh `python3.12 -m venv` +
  `pip install mkdocs mkdocs-material`. Book pages use inline ` ```mermaid ` fences
  (NOT committed `.mmd` source) so they render natively and aren't coupled to
  `check-diagram-freshness`. Authoring agents emit `\n` line breaks in Mermaid labels —
  must be normalized to `<br/>` (a deterministic post-pass did this).
- Populated the previously-empty `docs/features/` tree: 15 source-linked "WHAT IS
  BUILT" docs + index; honest about limitations (unauthenticated in-cluster memory
  backends, WorkflowRun NATS-delete bug, ADK stubs, RAG planned, supervision planned).
- New [documentation-rubric.md](docs/plans/documentation-rubric.md); 3 adversarial
  scoring iterations (54.3 → 60.5 → 64.5) + deterministic accuracy sweep — full log in
  [documentation-iterations.md](docs/plans/documentation-iterations.md). Adversarial
  grading caps Accuracy at 0.5 on any high-severity factual error, so the headline
  stayed low while per-claim accuracy is >95%; all *identified* high-severity errors
  were corrected against source.
- Fixed stale gate status across README, `docs/{designs,plans}/README`, CLAUDE.md
  (gate is **OPEN** since 2026-04-22, not CLOSED; **27 specs** not 13; **20 CRD kinds**,
  21 *_types.go files). Promoted RAG spec draft→current; fixed goose `doc.go` +
  `agent-runtime-spi.md` `Drain(ctx, session)` signature.

### 2026-05-07 — TD-P2-12 closed: 6 Memory backends wired

- Six new `memory_<backend>_backend.go` files + `memory_multi_backend.go` dispatcher implement `BackendProvisioner` for redis/qdrant/pgvector/neo4j/mem0/zep. `MemoryReconciler.SetupWithManager` now wires `NewMultiBackendProvisioner` instead of `NewSQLiteBackend`. External-endpoint/credential-ref config fields bypass in-cluster provisioning; in-cluster fallbacks use `apps/v1.StatefulSet` for redis/neo4j/pgvector/zep and `external-secrets.io/v1.ExternalSecret` (unstructured) for mem0/zep-cloud. Operator CRDs (QdrantCluster, CNPG Cluster, ExternalSecret) all use `unstructured.Unstructured` SSA — zero go.mod churn. Credentials mounted as projected files, never env vars (rule 05.7). `memory_backends_test.go` adds 50+ unit tests. `go test -short -race ./internal/controller/keese/...` green.
- **fake-client SSA gotcha**: `controller-runtime@v0.21` fake client `Patch(client.Apply)` returns "not found" for resources not in the tracker, even if the scheme is registered. Tests that need SSA creation must use envtest. Unit tests restructured to cover external/no-op modes + pure builder functions only; full SSA path covered by the existing integration suite (`FakeBackendProvisioner`).

### 2026-05-06 — Wave-0/Wave-1 partial: D5 retarget, infra hardening, AgentRuntime SPI

- **D5 retargeted to local kind T1+T2 only.** Cloud deploy (D4) deferred. New [scripts/dev/d5-anthropic-smoke.sh](scripts/dev/d5-anthropic-smoke.sh) runs the Anthropic round-trip + memory-persistence checks; `make d5-smoke` wraps it. T2 carries soft-fail semantics (exit 2) until full Drain/Resume SPI lands per TD-P1-02 (now closed).
- **OpenBao dev-mode divergence documented.** `dev/bootstrap/values/openbao.yaml` runs in dev mode (`server.dev.enabled: true`, in-memory, root-token); the bootstrap README previously claimed "manual unseal for dev parity with prod" but that was aspirational. Replaced with the actual divergence + a clear pointer at `values/openbao-prod.yaml.example` for the prod template (Shamir or KMS auto-unseal on PVC-backed storage).
- **Closed: TD-P1-02 (AgentRuntime SPI Bootstrap/Drain/Resume), TD-P1-08 (helmfile chart pinning), TD-P2-09 (config/overlays/prod), TD-P2-17 (90s grace + probe alignment).** New SPI surface lives at [internal/runtime/spi/v1alpha1/](internal/runtime/spi/v1alpha1/) with goose provider at [internal/runtime/providers/goose/](internal/runtime/providers/goose/) and the new `keese-drain` preStop sidecar at [cmd/keese-drain/main.go](cmd/keese-drain/main.go). 9-file kuttl suite at [tests/e2e/agentruntime-drain/](tests/e2e/agentruntime-drain/).
- **Pre-commit blockers fixed:** [scripts/check-signal-handling.sh](scripts/check-signal-handling.sh) regex was choking on `signal.NotifyContext(context.Background(), syscall.SIGTERM, ...)` because `[^)]*` stops at the first `)` of `Background()` and the same regex couldn't see SIGTERM if the call spanned two lines. Replaced with two-grep pass requiring `signal.Notify(Context)?` AND `syscall.SIGTERM` in the same file. Also added 6 `+keese:rebac-tuple` markers that were missing on `api/{keese,authz}/v1alpha1/` after the group-rename commit `ce2436e`.
- **Still open from Wave 1: TD-P1-01 (real OpenFGA SDK across packages).** First dispatch attempt produced a worktree from stale base (see Gotchas) and was abandoned without merge. Re-dispatch needed.

### 2026-05-06 — Feature gates (D27 OpenFeature) + cosign webhook retrofit

- New design [27-feature-gates-openfeature.md](docs/designs/27-feature-gates-openfeature.md) (rubric 100/100, iter 2): every keese capability ships behind a `policy.keese.ai/v1alpha1.FeatureGate` cluster-scoped CR. The operator's `FeatureGateController` projects effective values into ConfigMap `keese-system/keese-features`; every binary mounts that CM (projected volume + fsnotify) and reads via `internal/featuregate.Enabled(ctx, gate)`. OpenFeature Go SDK is the public API surface; in-process provider over `atomic.Value[map[string]bool]` is the impl.
- **Why CRD → CM not CRD-direct**: pods already mount ConfigMaps, projected volumes survive apiserver outages, one CM means one watch per binary. CRD owns schema + audit; CM is the boring delivery vehicle.
- **Stage rules cribbed from k8s**: alpha=off, beta=on, ga=code unconditional + CR set to deprecated for one minor, deprecated=frozen + Warning event on read.
- **Cosign as first consumer (TD-P1-04 retrofit)**: two gates land — `cosign.installplan.verify` (alpha, override:true in prod) wraps the whole `Handle` short-circuit, and `cosign.installplan.failClosed` (alpha, default-off) downgrades verify failures to Allowed+Warning+`Event(BundleUnsignedAdmittedDryRun)` for staged rollouts.
- **Tamper resistance**: kyverno `ClusterPolicy` denies CM writes from any SA other than `keese-controller-manager`. Cosign-signed operator image (rule 05.12 + TD-P1-04) is the trust root — only the signed controller's SA can write the projection.

### 2026-05-06 — Five more P1 items closed (P1-06, P1-07, P1-09, P1-10, P1-11) + post-rename regression fixed

- **TD-P1-06** (Workspace predicate ADR): [docs/designs/26-workspace-managed-predicate-adr.md](docs/designs/26-workspace-managed-predicate-adr.md) commits to predicate-free reconcile permanently. Reasoning: keese owns its API groups (no shadow consumer), label-stamping is a footgun, RBAC + break-glass already cover the legitimate escape hatches.
- **TD-P1-07** (kuttl progression case): [tests/e2e/workspace-progression/](tests/e2e/workspace-progression/) asserts `Tenant=Active`, `Workspace=Ready`, `WorkspaceSession=Active` via `kubectl wait` (NOT native kuttl resource matching — its slice matcher is exact-length, too strict for status conditions). `kuttl` added to flake.nix (nixpkgs attr `kuttl`; binary `kubectl-kuttl`). Both kuttl tests (`workspace-progression` + `aigw-defense`) pass in 18.5s.
- **TD-P1-09** (sqlite invariant): `keese.ai-v1alpha1-memory.md` documents the single-pod-per-Memory invariant — three controller-side enforcements (RWO PVC, per-Memory UID-named PVC, single session pod per workspace+subject) + production guidance to switch to network-attached providers for multi-replica. VAP `SqliteSingleConsumer` for admission-time enforcement deferred to TD-P2-08.
- **TD-P1-10** (chart CRD pre-apply): [dev/bootstrap/install-crds.sh](dev/bootstrap/install-crds.sh) pulls each chart's `crds/*.yaml` and SSA-applies before helmfile sync. Wired into `make bootstrap-infra`. Currently lists `envoyproxy/gateway-helm` (the chart that bit us twice — v1.4→v1.6 BackendTLSPolicy GA promotion, v1.6→v1.7 churn).
- **TD-P1-11** (WorkspaceSession watches): [internal/controller/keese/workspacesession_controller.go](internal/controller/keese/workspacesession_controller.go) `SetupWithManager` now does `Owns(&corev1.Pod{})` + `Owns(&corev1.PersistentVolumeClaim{})` and a poke-friendly predicate. Annotations matching `keese.ai/poke*` trigger reconcile without bumping spec generation. Pod deletion / PVC deletion now requeues the parent (was the "delete pod, watch status drift to Ready=True forever" pain).
- **TD-P1-07-followon** (rename regression): the group-rename agent merge dropped the gateway-CA + recipe ConfigMap mounts on the session pod. Restored via extracted `sessionPodVolumes` / `sessionPodVolumeMounts` / `sessionPodEnv` helpers. Env now includes `SSL_CERT_FILE`, path-prefixed `ANTHROPIC_BASE_URL`/`ANTHROPIC_HOST`, optional `KEESE_RECIPE_PATH`.
- **aigw-defense test reframed**: pre-TD-P1-03 the test asserted that garbage Bearer tokens get stripped+replaced; that contract is gone now that ext_authz treats `Authorization: Bearer <SA-token>` as the identity claim. New test surface: `no-auth → 403`, `garbage Bearer → 403`, `valid SA token + hostile x-api-key → 200` (Lua strips the vendor header before BSP injection), `valid SA token + hostile anthropic-api-key → 200`, `valid SA token + both vendor headers → 200`. 5 cases, all pass.
- **Eight P1 items closed across the two sessions**: P1-01 (OpenFGA SDK), P1-02 (AgentRuntime SPI), P1-03 (ext_authz), P1-06, P1-07, P1-09, P1-10, P1-11. Two remain (P1-04 cosign webhook, P1-05 signed bundle) — both heavy CI/release-pipeline work that needs GitHub Actions OIDC + OLM on the demo cluster to fully verify.

### 2026-05-06 — TD-P1-03 closed: keese-authz Envoy ext_authz wired against OpenFGA

- New service [cmd/keese-authz/](cmd/keese-authz/main.go) implements
  `envoy.service.auth.v3.Authorization` on `:9001`. Per request:
  match against in-memory trie → extract subject from SA-token JWT
  → `OpenFGA.Check(user, can_call, tool:<name>)` → ALLOW + injected
  `x-keese-tool` / `x-keese-workspace` headers, or DENY with audit
  log line.
- Trie compilation lives in [internal/authz/extauth/](internal/authz/extauth/):
  `resolver.go` (atomic.Value snapshot, cluster-first then
  namespace-scoped), `match.go` (Gateway API HTTPRouteMatch subset
  + restricted JSONPath body discriminator), `subject.go` (SA-token
  sub-claim parse OR named JWTClaim), `check.go` (Authorize
  orchestration), `audit.go` (strict-allowlist redacted log).
- Two new CRDs in `authz.keese.ai/v1alpha1`: `ToolBinding`
  (cluster, platform-admin catalogue) + `WorkspaceTool` (namespaced,
  tenant-admin per-workspace tools). See
  [docs/designs/22-egress-toolbinding.md](docs/designs/22-egress-toolbinding.md).
- `Workspace.spec.egress.allowedTools[]` writes one
  `tool:<n>#allowed_in@workspace:<wsname>` ReBAC tuple per element.
  Closes the orphan-tuple gap from TD-P1-01 (the FGA `tool` type
  was declared but no controller wrote tuples).
- Bootstrap manifest [dev/bootstrap/aigateway/keese-authz.yaml](dev/bootstrap/aigateway/keese-authz.yaml)
  ships Deployment + Service + ClusterRoleBinding + Envoy Gateway
  `SecurityPolicy.spec.extAuth.grpc` wiring. Image
  `keese-authz:demo` built from [Dockerfile.keese-authz](Dockerfile.keese-authz)
  (distroless static, multi-arch).
- **End-to-end verified on demo cluster (2026-05-06)**: with
  `tool:anthropic.messages#allowed_in@workspace:my-ws` tuple
  present + valid SA token, gateway returns HTTP 200 from Anthropic
  with audit log `decision: allow, duration_ms: 18`. Without tuple:
  HTTP 403 `permission_denied`, audit log `decision: deny, reason:
  openfga_denied`. Without SA token: HTTP 403, audit log
  `subject_extraction_failed`.
- **Subject-format gotcha**: SA-token JWT `sub` shape is
  `system:serviceaccount:<ns>:<sa>` — naively prefixing with
  `service_account:` produces 4 colons which OpenFGA rejects as
  "user field malformed". The fix in `subject.go` extracts just the
  SA name (last colon segment) so the FGA user-id is
  `service_account:ksa-<wsuid>` — matching the keese Workspace
  controller's tuple shape.
- **Body-discriminator gotcha**: Envoy doesn't buffer the request
  body for ext_authz by default. The bodyDiscriminator's sub-tool
  resolution (`model=opus → .opus-4`) therefore never fires
  end-to-end today; the bare `tool:anthropic.messages` is what
  reaches keese-authz. Workaround in demo:
  `Workspace.spec.egress.allowedTools` includes both the bare and
  per-model entries. Production fix: configure
  `with_request_body` on `SecurityPolicy.spec.extAuth.grpc` —
  tracked as a TD-P1-03 follow-on.
- **OpenFGA config CM mirror**: keese-authz reads `OPENFGA_STORE_ID`
  and `OPENFGA_AUTHORIZATION_MODEL_ID` from a ConfigMap in
  `keese-system`, but the seed Job populates the canonical CM in
  `openfga` namespace. Today it's a manual
  `kubectl get cm -n openfga | sed | apply -n keese-system` step.
  Tracked as a TD-P1-03 follow-on alongside the existing TD-P1-01
  follow-on for the seed image.

### 2026-05-06 — A9 doc sweep: 10-group → 3-group rename complete

- 21 spec files renamed (`git mv`) to `keese.ai-v1alpha1-<kind>.md`, `authz.keese.ai-v1alpha1*.md`, `policy.keese.ai-v1alpha1*.md`; ~55 other files had text rewritten; CLAUDE.md, MEMORY.md, `.claude/agents/`, `.claude/skills/` updated.
- Tenancy split judgment: `CrossTenantAgreement` spec file kept under `keese.ai-v1alpha1-tenancy-ii-cra.md` (CSV-predicted name), but noted in README and spec frontmatter that CTA runs in `authz.keese.ai` at runtime — follow-on to fully split is deferred.
- Known discrepancy: `config/manifests/bases/keese.clusterserviceversion.yaml` references `keese.ai-v1alpha1-ii-session.md` while bundle CSV references `keese.ai-v1alpha1-workspace-ii-session.md`; file created per bundle CSV. The config manifests CSV needs a follow-on fix (out of A9 scope per constraint on `config/`).
- See [td-p1-03-extauth-and-group-rename.md](docs/plans/td-p1-03-extauth-and-group-rename.md) for full context.

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
- [workspace spec](docs/specs/keese.ai-v1alpha1-workspace.md) — split into 5 files (primary + ii-workspace + ii-share + ii-session + ii-iter-log) to honor 200-line cap; covers Workspace + WorkspaceShare + WorkspaceSession (D27).
- [workflow spec](docs/specs/keese.ai-v1alpha1-workflow.md) — split a/b for Workflow vs WorkflowRun + cross-tenant admission. Q2(b) decision recorded: cross-tenant peers derived from `transportRef`s with `scope: cross-tenant` (NO new participants[] field).
- **Two NEW spec stubs added** for D26/D28/D29 kinds (gap discovered during dispatch): [tenancy spec](docs/specs/keese.ai-v1alpha1-tenancy.md) (Tenant + CrossTenantAgreement) and [authz spec](docs/specs/authz.keese.ai-v1alpha1.md) (OIDCProvider). Both held at draft pending follow-up architect dispatch.
- (superseded 2026-05-06 — spec file names above updated to new 3-group layout; see [td-p1-03-extauth-and-group-rename.md](docs/plans/td-p1-03-extauth-and-group-rename.md))
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

- [D29 ratified](docs/plans/scaffolding-plan.md) — `CrossTenantAgreement` CRD (`authz.keese.ai/v1alpha1`, cluster-scoped, cert-manager-style bilateral handshake). Kind count 16 → 17. Amends D23.
- [04a iter-5](docs/designs/04a-openfga-authz-model.md) — added `tenant.allows_messaging` + `workspace.messageable_from` ReBAC relations; old proposed `workspace#can_message` dropped. Cross-tenant a2a authz is workspace-pair-scoped.
- [04b iter-3](docs/designs/04b-projected-sa-identity.md) — `audienceTemplates` (`egress`, `workflowRun`, `supervisor`); agent pods now mount three projected SA tokens at `/var/run/keese/tokens/{egress,workflowRun,supervisor}`.
- [09 iter-3](docs/designs/09-transport-crd.md) — a2a peer-auth modes 4 → 2 (`workspace-sa`, `mutual-tls`); dropped `user-oidc` + `none`; new `spec.a2a.scope: intra-tenant | cross-tenant`. NATS is the primary intra-tenant transport.
- [03 iter-3 + 03c](docs/designs/03c-workflow-messaging-plane.md) — Workflow controller owns NATS topic provisioning (`keese.tenant.<t>.wf.<r>.*`), `workflowRun` audience injection, CRA admission, stream teardown.
- [Q2(b) decision](docs/designs/03c-workflow-messaging-plane.md) — cross-tenant peers derived implicitly from `transportRef`s with `scope: cross-tenant`. NO new `WorkflowRun.spec.participants[]` field.
- [Design 25 stub](docs/designs/25-cross-tenant-agreement.md) — CRD spec authoring deferred; full design pending (held at draft).
- Design count 48 → 53 (added `02-ii`, `04a-iii`, `09-ii`, `03c`, `25` to index).

### 2026-04-20 — initial scaffolding (P0–P8)

- [Scaffolding plan + 26 decisions](docs/plans/scaffolding-plan.md) —
  license Apache-2.0; API groups `keese.ai` / `authz.keese.ai` / `policy.keese.ai`; Capsule opt-in;
  GuardrailBinding composition (not Constitution + Policy +
  ToolAllowList); 17 kinds across 3 groups (D26 added keese `Tenant`
  CRD); Envoy AI Gateway + MCPRoute; Argo delegation; OpenTofu cloud;
  GoLand primary IDE; SIGTERM drain; SSA fieldOwner; durable agent
  identity (D24) + GUPP resume contract (D25) added 2026-04-20 after
  Gas Town review; D26 keese Tenant CRD amends D23 for ReBAC backing.
- [Session handoff summary](docs/plans/scaffolding-summary.md) —
  state after P0–P8; next-phase instructions; resume commands after
  clone/move.

## Gotchas

### 2026-05-07 — Parallel inline agents can stomp the git index

- When dispatching agents WITHOUT `isolation: "worktree"` they share the
  parent's git index. Agent A's `git add` in flight + parent's
  `git commit` produces a commit that includes A's staged-but-not-yet-
  committed work under the parent's commit message.
- Concrete example this session: [`a5d0082`](https://github.com/keese-ai/keese/commit/a5d0082)
  ("refactor(api): finish post-rename cleanup in authz + policy
  controllers"). The message describes only the post-rename string
  rewrites in `internal/controller/authz/oidcprovider_controller.go` +
  `internal/controller/policy/{tokenbudget_controller.go,suite_test.go}`,
  but the diff also includes the entire TD-P2-07 Recipe webhook
  scaffolding (`config/{certmanager,webhook,default}/...`,
  `internal/controller/keese/recipe_webhook{,_test}.go`,
  `cmd/main.go`'s `SetupRecipeWebhookWithManager` call) **and** the
  D27 `featuregate_controller{,_test}.go` from a different concurrent
  session. The work is correct, the message is incomplete. Diff stat:
  16 files / 992 insertions (vs. ~30 the message implies).
- Closure summaries in [docs/plans/demo/tech-debt.md](docs/plans/demo/tech-debt.md)
  are accurate (TD-P2-07 row points at the right files); only the
  commit-log reader is misled.
- **Recommendation for next session:** when dispatching parallel inline
  agents without worktree isolation, either (a) issue `git add <exact
  paths>` not `git add <directory>` so other agents' staged files
  don't get swept, or (b) `git stash --keep-index --include-untracked`
  before any commit to capture only what you intentionally staged.

### 2026-05-06 — Agent-tool worktree pool returns stale-base branches

- The Claude Agent tool's `isolation: "worktree"` parameter creates worktrees from a pool/cache, NOT from current `main` HEAD. A Wave-1 dispatch on 2026-05-06 produced three worktrees branched from `2994872` — two commits before the API-group rename `ce2436e` (which collapsed `api/{transport,memory,tenancy,guardrail,…}/v1alpha1/` into `api/{keese,authz,policy}/v1alpha1/` and similarly for `internal/controller/`).
- Symptom: every modified file in the worktree references the **old** path layout. Merge-back via `conductor/worktree-merge.sh` produces a 7000+/12000− diff because git sees the rename as "create the old paths, delete the new paths."
- Workaround: cherry-pick **new files** from the worktree (paths the rename didn't touch), and **manually re-apply** in-place edits at the new file locations. Worked for TD-P1-08+P2-09+P2-17 (helmfile.yaml, manager.yaml, config/overlays/prod/* — all path-stable) and TD-P1-02 (cmd/keese-drain/, internal/runtime/spi/, internal/runtime/providers/goose/, tests/e2e/agentruntime-drain/ all clean; only workspacesession_controller.go preStop + workspace_events.go reasons needed in-place re-apply at the new `internal/controller/keese/` path).
- Did NOT work for TD-P1-01 (OpenFGA SDK swap touched 22 *_rebac_openfga.go files, all in old paths) — abandoned.
- **Recommendation for next session:** dispatch agents inline-sequentially without `isolation: "worktree"` (work on `main` directly), or pre-create a fresh worktree from current HEAD via `git worktree add <path> main` and pass that path to the agent's prompt.

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
