<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: register
depends:
  - README.md
  - D1-controller-wiring.md
  - D2-runtime-spi-minimum.md
  - D3-cluster-bootstrap.md
  - D4-cloud-deploy.md
  - D5-demo-smoke.md
related_skills: [plan-management]
status: current
last_verified: 2026-04-29
---

# Tech-debt register — post-demo cleanup

> Captures every shortcut taken in the demo track (D1–D5) and every
> stub flagged in the 2026-04-25 readiness audit. Severity tiers:
>
> - **P1** — ship within one week of demo. Blocks any second user, any
>   second tenant, any production-grade incident handling.
> - **P2** — ship within one month. Blocks v0.1.0 GA channel.
> - **P3** — ship within one quarter. Blocks v0.2.0+ feature scope.

## P1 — within one week

| ID | Item | Source | Spec / Design |
|---|---|---|---|
| TD-P1-01 | Replace `FakeRebacWriter` with real OpenFGA SDK across every package that imports it (transport, recipe, guardrail, workspace, tenant, workflow). Add `github.com/openfga/go-sdk` to `go.mod`. Without this, the second user's authorization decisions are no-ops. | audit 2026-04-25; deferred from D1 | [docs/specs/egress-authz-protocol.md](../../specs/egress-authz-protocol.md) |
| TD-P1-02 | Implement `AgentRuntime` SPI: `Bootstrap`, `Drain(90s)`, `Resume(lastCheckpoint)` in `internal/runtime/spi/v1alpha1/` and `internal/runtime/providers/goose/`. The D2-T5 preStop hook is a placeholder. | D2-T5 | [docs/designs/07-agent-runtime-spi.md](../../designs/07-agent-runtime-spi.md), [docs/specs/agent-runtime-spi.md](../../specs/agent-runtime-spi.md) |
| TD-P1-03 | Wire ext_authz on Envoy AI Gateway against OpenFGA. Demo runs permit-all. | D3-T2 | [docs/designs/05a-envoy-ai-gateway-topology.md](../../designs/05a-envoy-ai-gateway-topology.md) |
| TD-P1-04 | Promote dev OpenBao path: pre-install ValidatingWebhook for cosign on InstallPlans (designed in 14a F7, never implemented). | D4-T2 fallback | [docs/designs/14a-olm-channels-upgrades.md](../../designs/14a-olm-channels-upgrades.md) |
| TD-P1-05 | If D4-T2 used the local-build fallback, push a CI-built signed image and update the running CSV by approving a new InstallPlan. Bundle currently shipped without cosign attestation in that path. | D4-T2 fallback | rule 05.12 |
| TD-P1-06 | Workspace controller predicate that filters on `keese.ai/managed: "true"` was dropped in D1-T2 for demo simplicity. Decide: re-add the predicate (and update every sample), or commit to no-predicate semantics. Document in [docs/designs/](../../designs/). | D1-T2 | n/a (new ADR needed) |
| TD-P1-07 | Author live `tests/e2e/kuttl-config.yaml` and at least three kuttl test cases (Tenant→Workspace→WorkspaceSession). `make test-e2e` errors out today. | D5 §verification | [docs/references/envtest-kuttl-harness.md](../../references/envtest-kuttl-harness.md) |
| TD-P1-08 | Helmfile chart versions: every chart in [dev/bootstrap/helmfile.yaml](../../../dev/bootstrap/helmfile.yaml) is `# unverified-2026`. Lock and remove the comment after D3-T1 confirms working versions. | D3-T1 | n/a |
| TD-P1-09 | sqlite WAL + RWO PVC + restart can corrupt the DB mid-write. Document the single-pod-per-Memory invariant in the spec, or move to a different backend default. | D2-T2 | [docs/designs/15-memory-management.md](../../designs/15-memory-management.md) |
| TD-P1-10 | Helm OCI charts (cert-manager, envoy-gateway, envoy-ai-gateway) don't upgrade CRDs across release bumps. Helmfile sync silently leaves stale CRDs (e.g. EG v1.6 ships `gateway.networking.k8s.io/v1.BackendTLSPolicy` GA but the v1.4 chart only installed `v1alpha3`). Wire `dev/bootstrap/install-crds.sh` (or a helmfile `hooks: prepare`) that applies each chart's bundled `crds/*.yaml` before sync. See MEMORY 2026-04-29. | D3-T1; 2026-04-29 EG v1.6 debug | n/a |

## P2 — within one month

| ID | Item | Source | Spec / Design |
|---|---|---|---|
| TD-P2-01 | WorkspaceShare ReferenceGrant projection is a stub at [workspaceshare_controller.go:88-109](../../../internal/controller/workspace/workspaceshare_controller.go#L88). Wire `gateway.networking.k8s.io/v1beta1` scheme + real SSA. | audit 2026-04-25 | [docs/specs/workspace.operator.keese.ai-v1alpha1.md](../../specs/workspace.operator.keese.ai-v1alpha1.md) |
| TD-P2-02 | Workflow trigger / output projections (CronJob, KEDA ScaledObject, Knative Trigger, HTTPRoute) are `TODO(spec-followup)` returns at [workflow_controller.go:200-215](../../../internal/controller/workflow/workflow_controller.go#L200). | audit 2026-04-25 | [docs/specs/workflow.operator.keese.ai-v1alpha1.md](../../specs/workflow.operator.keese.ai-v1alpha1.md) |
| TD-P2-03 | RecipeSource git-clone path records a fake revision/digest at [recipesource_controller.go:208](../../../internal/controller/recipe/recipesource_controller.go#L208). Add go-git, real clone, real digest. | audit 2026-04-25 | [docs/designs/16-recipe-distribution.md](../../designs/16-recipe-distribution.md) |
| TD-P2-04 | GuardrailBinding Envoy SecurityPolicy CEL evaluation is a stub at [guardrailbinding_controller.go:226-234](../../../internal/controller/guardrail/guardrailbinding_controller.go#L226). | audit 2026-04-25 | [docs/designs/06-guardrailbinding.md](../../designs/06-guardrailbinding.md) |
| TD-P2-05 | Transport NATS + cert-manager projections stub. [transport/nats.go:35](../../../internal/controller/transport/nats.go#L35), [transport/certmanager.go:11](../../../internal/controller/transport/certmanager.go#L11). | audit 2026-04-25 | [docs/specs/transport.operator.keese.ai-v1alpha1.md](../../specs/) |
| TD-P2-06 | Tenant controller Capsule namespace lookup stub at [tenant_controller.go:284-340](../../../internal/controller/tenancy/tenant_controller.go#L284). | audit 2026-04-25 | [docs/designs/01-tenancy-capsule.md](../../designs/01-tenancy-capsule.md) |
| TD-P2-07 | Validating + defaulting webhooks: none wired. No `SetupWebhookWithManager` calls anywhere. `config/webhook/` doesn't exist. Recipe is the natural first candidate (admission helper code already exists). | audit 2026-04-25 | rule 04.12 |
| TD-P2-08 | VAP YAMLs (rule 04.12 says VAP first, webhook second). Author at minimum: `EmbeddingDimImmutable` (15 design), `BreakGlassAnnotation` (rule 05.13), `RegionalSensitive` (BSP enforcement). | audit 2026-04-25 | rule 04.12 |
| TD-P2-09 | `config/overlays/prod/` with image digest pinning, resource limits (operator + every helmfile chart), and namespace tuning. `config/overlays/dev/` is the only overlay today. | audit 2026-04-25; D4-T7 | rule 05.12 |
| TD-P2-10 | OLM upgrade test suite under `test/e2e/olm-upgrade/`. Designed in 14a-ii §8-step assertion, not authored. | audit 2026-04-25 | [docs/designs/14a-olm-channels-upgrades.md](../../designs/14a-olm-channels-upgrades.md) |
| TD-P2-11 | Release tooling scripts: `scripts/set-csv-replaces.sh`, `scripts/bundle-sign-verify.sh`, `scripts/check-bundle-drift.sh`, `scripts/check-dep-versions.sh`, `scripts/check-optional-deps.sh`, `olm-catalog-publish.yaml` workflow. | audit 2026-04-25 | [docs/designs/14a-olm-channels-upgrades.md](../../designs/14a-olm-channels-upgrades.md), [14b](../../designs/14b-olm-dependencies.md) |
| TD-P2-12 | Memory backends beyond sqlite: redis, qdrant, pgvector, neo4j, mem0, zep. All BackendProvisioner stubs. | D2-T2 | [docs/designs/15-memory-management.md](../../designs/15-memory-management.md) |
| TD-P2-13 | Additional LLM providers: AWS Bedrock (OIDC-STS), GCP Vertex (WIF), Azure OpenAI (Entra), credential pooling. Demo ships Anthropic-only. (Anthropic `x-api-key` injection closed 2026-04-30 via native v0.4+ BSP `AnthropicAPIKey` type + EG `extensionManager.hooks.xdsTranslator` wiring; the prior Lua header-rewrite EnvoyExtensionPolicy was removed because it shadowed the AI Gateway extProc.) | D3-T2 | [docs/designs/05b-credential-injection-patterns.md](../../designs/05b-credential-injection-patterns.md) |
| TD-P2-14 | TokenBudget enforcement on session pods. Today the controller reconciles TokenBudget but doesn't gate sessions. | audit 2026-04-25 | [docs/specs/observability.operator.keese.ai-v1alpha1.md](../../specs/) |
| TD-P2-15 | Projected SA token audiences `workflowRun` and `supervisor` (D2-T4 ships only `egress`). | D2-T4 | [docs/designs/04b-projected-sa-identity.md](../../designs/04b-projected-sa-identity.md) |
| TD-P2-16 | Operator + bundle Subscription manifest declaring `installPlanApproval: Manual` for the `stable` channel. Currently any consumer must set this themselves. | upgrade audit 2026-04-25 | [docs/designs/14a-olm-channels-upgrades.md](../../designs/14a-olm-channels-upgrades.md) |
| TD-P2-17 | Liveness probe values must align with `terminationGracePeriodSeconds: 60` per design 18. D1-T4 sets 30/10/3; verify under real load and revisit. | D1-T4 | [docs/designs/18-process-lifecycle.md](../../designs/) |

## P3 — within one quarter

| ID | Item | Source | Spec / Design |
|---|---|---|---|
| TD-P3-01 | OpenTofu modules under `deploy/opentofu/` for EKS / GKE / AKS. Designed fully in 21; zero code. | audit 2026-04-25; D4-T1 | [docs/designs/21-opentofu-cloud-deployment.md](../../designs/21-opentofu-cloud-deployment.md) |
| TD-P3-02 | Conversion webhooks for CRD `v1alpha1 → v1beta1` promotions. Rule 04.13 explicitly defers to first promotion; track when first kind hits beta. | rule 04.13 | rule 04.13 |
| TD-P3-03 | OperatorHub catalog publishing path: `ghcr.io/keese-ai/keese-catalog`. CSV `replaces:` chain automation. | upgrade audit | [docs/designs/14b-olm-dependencies.md](../../designs/14b-olm-dependencies.md) |
| TD-P3-04 | InjectPrompt SPI method (design 23 iter-2 flagged) — bumps the AgentRuntime SPI. | upgrade audit | [docs/designs/23-supervision-escalation.md](../../designs/) |
| TD-P3-05 | Real keese fork of goose (`internal/runtime/providers/goose/`). Demo uses `ghcr.io/block/goose:latest` upstream image directly. | DD-4 | [docs/designs/08a-goose-headless-modes.md](../../designs/08a-goose-headless-modes.md) |
| TD-P3-06 | OPA / Conftest Rego policies for OpenTofu cloud deployments. | audit 2026-04-25 | [docs/designs/21-opentofu-cloud-deployment.md](../../designs/21-opentofu-cloud-deployment.md) |
| TD-P3-07 | Multi-tenant + cross-tenant smoke tests. Demo is single-tenant. | D5 §verification | [docs/specs/tenancy.operator.keese.ai-v1alpha1.md](../../specs/) |
| TD-P3-08 | Chaos / network-partition / disk-full tests. None today. | D5 §verification | n/a |
| TD-P3-09 | Backup + DR runbook for OpenBao + OpenFGA + NATS streams. | D4 out-of-scope | n/a |
| TD-P3-10 | OPA Scorecard hardening past current state. | rule 02 supply chain | rule 02 |

## Tracking

- Open one GitHub issue per row when work begins, link the issue ID in
  this table.
- Update `last_verified` on this doc whenever a row moves status
  (don't let it rot post-demo).
- When a P1 ships, move its row to a "Closed" section at the bottom or
  delete it; don't keep historical rows inline.

## Refinement passes

This is a register, not a phase plan. No iteration log. Score-against-rubric
applies only to the individual fix plans authored when each row's work
starts.

## See also

- [README.md](README.md) — demo phase index.
- [docs/plans/gate-open-audit-2026-04-22.md](../gate-open-audit-2026-04-22.md) — the original honest-score audit; this register extends it post-demo.
- [MEMORY.md](../../../MEMORY.md) — running gotchas; cross-link big fixes.
