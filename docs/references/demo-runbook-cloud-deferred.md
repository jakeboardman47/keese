<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: runbook
depends:
  - demo-runbook-kind.md
  - ../plans/demo/D4-cloud-deploy.md
  - ../plans/demo/tech-debt.md
related_skills: []
status: current
last_verified: 2026-05-06
---

# Cloud deployment — deferred items

The 2026-04-27 demo runs entirely on a local kind cluster
([demo-runbook-kind.md](demo-runbook-kind.md)). The operator,
17 CRDs, and the Workspace + Session lifecycle are validated locally;
LLM-round-trip and managed-cluster artifacts are deferred. This doc
enumerates exactly what is left to land for a production cloud
deployment.

## 1. Cluster + IaC

| Item | Status | Where it lands |
|---|---|---|
| OpenTofu modules for EKS / GKE / AKS | Designed only ([21-opentofu-cloud-deployment.md](../designs/21-opentofu-cloud-deployment.md)); no code | `deploy/opentofu/` (does not exist) |
| Conftest Rego policies for IaC | Designed; no rules written | `deploy/opentofu/policies/` |
| Per-cloud state backends (S3+DynamoDB / GCS / Azure Blob) | Designed; not provisioned | OpenTofu `backend "s3" \| "gcs" \| "azurerm"` blocks |
| Cluster autoscaler / node pool tuning | Defaults only | per-cloud module variables |
| Multi-AZ HA for stateful sets (NATS, OpenFGA, OpenBao) | Defaults only | helmfile values per environment |

## 2. Image distribution

| Item | Status | Where it lands |
|---|---|---|
| Tag-triggered image push via [.github/workflows/image.yaml](../../.github/workflows/image.yaml) | Wired but unrun | push a `vX.Y.Z` tag |
| cosign keyless OIDC sign of `ghcr.io/keese-ai/keese` and `…-bundle` | Wired in workflow | runs on tag push |
| syft SBOM attestation | Wired in workflow | runs on tag push |
| `cosign verify` ValidatingWebhookConfiguration on InstallPlans | **Implemented 2026-05-06 (TD-P1-04)** — `cmd/keese-cosign-webhook/`, `internal/admission/cosign/`, `config/cosign-webhook/` | applied via `kubectl apply -k config/cosign-webhook/` |
| Local `make docker-build/push` fallback | **Removed 2026-05-06 (TD-P1-05)** — CI is the only signing path | see [csv-rotate-to-signed-bundle.md](csv-rotate-to-signed-bundle.md) |

## 3. OLM-based install

| Item | Status | Where it lands |
|---|---|---|
| `operator-sdk run bundle ghcr.io/keese-ai/keese-bundle@sha256:…` | Untested in cloud; bundle validates locally | run after image push |
| Subscription with `installPlanApproval: Manual` | No manifest committed (TD-P2-16) | `config/olm/subscription.yaml` |
| `replaces` chain across CSVs (`scripts/set-csv-replaces.sh`) | Designed (14a §2); no script | `scripts/` |
| Custom catalog at `ghcr.io/keese-ai/keese-catalog` | Designed (14b); no catalog index | `olm-catalog-publish.yaml` workflow |
| OperatorHub alpha channel listing | Declared in CSV; not submitted | OperatorHub PR |

## 4. Bootstrap stack (helmfile)

[dev/bootstrap/helmfile.yaml](../../dev/bootstrap/helmfile.yaml) lists 13
charts. None were verified live during the kind smoke (deferred).

| Chart | Pinned version | Status |
|---|---|---|
| cert-manager | v1.15.3 | likely OK (well-maintained chart) |
| capsule | 0.7.2 | `# unverified-2026` |
| envoy-gateway | v1.3.2 | `# unverified-2026` |
| envoy-ai-gateway | v0.2.0 | **highest risk** — repo confirms v0.2.1 is latest; pin may need bump |
| openfga | 0.2.27 | `# unverified-2026` |
| kyverno | 3.2.6 | `# unverified-2026` |
| nack | 0.26.0 | `# unverified-2026` |
| nats | 1.2.6 | `# unverified-2026` |
| eck-operator | 2.13.0 | `# unverified-2026` |
| openbao | 0.4.0 | `# unverified-2026`; chart repo URL also flagged |
| external-secrets | 0.10.5 | `# unverified-2026` |
| argo-workflows | 0.42.5 | `# unverified-2026` |
| qdrant | 1.13.0 | `# unverified-2026` |
| otel-collector | 0.112.0 | `# unverified-2026` |

Action: run `helmfile -f dev/bootstrap/helmfile.yaml deps` against the
cloud cluster and capture a `helmfile.lock` (currently gitignored).
Replace any `# unverified-2026` charts whose versions don't resolve.

## 5. LLM credential plumbing (Anthropic path)

The static-API-key BSP stack is authored but never exercised against a
live gateway:

- [dev/bootstrap/aigateway/anthropic-llm-stack.yaml](../../dev/bootstrap/aigateway/anthropic-llm-stack.yaml)
- [dev/bootstrap/aigateway/cluster-secret-store.yaml](../../dev/bootstrap/aigateway/cluster-secret-store.yaml)
- [dev/bootstrap/aigateway/service-alias.yaml](../../dev/bootstrap/aigateway/service-alias.yaml)

Pre-flight before enabling on the cloud:

1. Confirm Envoy AI Gateway chart's pod selector labels match the
   `service-alias.yaml` selector
   (`gateway.envoyproxy.io/owning-gateway-{namespace,name}`).
2. Populate OpenBao at `keese/tenants/tenant-a/anthropic` with the real
   key (or export `ANTHROPIC_API_KEY` and re-run `dev/bootstrap/openbao/seed.sh`).
3. **Known caveat (TD-P2-13).** The BSP `APIKey` type injects
   `Authorization: Bearer …`. Anthropic expects `x-api-key: …`. The
   demo proves the routing path; production needs an HTTPRouteFilter
   or Lua filter that copies Authorization → x-api-key on egress.

Switch the agent pod from the `sleep infinity` long-lived stub
(`internal/controller/workspace/workspacesession_controller.go`) back
to `["/usr/local/bin/goose","session","--resume"]` once the gateway is
proven reachable.

## 6. Authorization and ReBAC

| Item | Status |
|---|---|
| Real OpenFGA SDK in `go.mod`, replacing `FakeRebacWriter` | TD-P1-01 — required before second tenant |
| ext_authz on Envoy AI Gateway against OpenFGA | TD-P1-03 — currently permit-all |
| OpenFGA model load + tuple seeding | scripts exist in `dev/bootstrap/openfga/`; not exercised in kind demo |

## 7. Upgrade lifecycle (D2-T5 follow-up)

| Item | Status |
|---|---|
| `AgentRuntime.Bootstrap / Drain / Resume` SPI | Designed (`docs/designs/07-agent-runtime-spi.md`); not implemented (TD-P1-02) |
| Operator rolling upgrade smoke (kind + cloud) | Untested |
| Helmfile chart upgrade smoke | Untested |

## 8. Observability

| Item | Status |
|---|---|
| OTEL exporter wiring on operator pod | Defaults only |
| Elastic APM destination | Designed; never wired live |
| Grafana dashboards | None committed |

## 9. Multi-tenant + multi-LLM

The demo runs **one Tenant, one LLM provider (Anthropic, deferred), one
user**. Multi-tenant + cross-tenant flows (`CrossTenantAgreement`) and
non-Anthropic providers (Bedrock, Vertex, Azure OpenAI) are entirely
deferred — see TD-P2-13 / TD-P3-07 in
[../plans/demo/tech-debt.md](../plans/demo/tech-debt.md).

## 10. Test coverage

| Item | Status |
|---|---|
| `tests/e2e/kuttl-config.yaml` + kuttl test cases | Missing — `make test-e2e` errors immediately (TD-P1-07) |
| OLM upgrade test suite under `test/e2e/olm-upgrade/` | Designed; not authored (TD-P2-10) |
| Cloud chaos / network-partition tests | None (TD-P3-08) |

## Suggested cloud-readiness sequence

1. Land **TD-P1-01** (real OpenFGA writer) and **TD-P1-02** (AgentRuntime
   SPI). These are the two gaps that make the cloud demo qualitatively
   different from the kind demo.
2. Spend one focused session on the helmfile chart-version sweep
   (TD-P1-08); commit `helmfile.lock`.
3. Author `tests/e2e/kuttl-config.yaml` + 3 cases (TD-P1-07) so cloud
   regressions are caught in CI.
4. Push first signed image tag (`v0.0.1-rc1`) to ghcr; verify cosign.
5. Provision a single cloud cluster (GKE Autopilot recommended for
   speed). Run `make bootstrap-infra` end-to-end. Capture failures
   verbatim — most likely site of issues is helmfile chart pins.
6. Apply `dev/demo/hello-keese.yaml`. Confirm the same green smoke as
   kind.
7. Switch the agent pod's command back to `goose session --resume` and
   exercise an actual LLM round-trip through the gateway (after the
   x-api-key header rewrite from TD-P2-13).
