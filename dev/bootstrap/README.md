<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: developer-experience
status: current
last_verified: 2026-05-06
---

# Local Dev Infra Bootstrap

One-page reference for `dev/bootstrap/`. Full context: plan Phase 7 in
`/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md`.

## Boot order

```
make kind-up          # ctlptl apply → kind cluster keese-dev + local registry :5005
make bootstrap-infra  # helmfile sync → installs 14 releases in dependency order
tilt up               # (or make tilt-up) → Tiltfile drives operator hot-reload + seeds
```

Dependency DAG (enforced by helmfile `needs:`):

```
cert-manager
  ├── capsule
  ├── envoy-gateway → envoy-ai-gateway
  ├── openfga
  ├── kyverno
  ├── nack → nats
  ├── eck-operator  ← otel-collector depends on this
  ├── openbao → external-secrets
  ├── argo-workflows
  └── qdrant
       ↓
    otel-collector ──(token-usage POST /ingest)──▶ keese-token-meter ──▶ prometheus (dev)
       ↓                                                                      ↑
    keese-operator (Tilt live-reload) ──(TokenBudget PromQL, CH5c)───────────┘
```

## Component matrix

| Release | Namespace | Purpose |
|---|---|---|
| cert-manager | cert-manager | TLS issuance |
| capsule | capsule-system | Multi-tenant Tenant isolation |
| envoy-gateway | envoy-gateway-system | Gateway API implementation |
| envoy-ai-gateway | envoy-gateway-system | MCPRoute / AIGatewayRoute / BackendSecurityPolicy |
| openfga | openfga | ReBAC authorization |
| kyverno | kyverno | Policy engine (GuardrailBinding) |
| nack | nats | NATS JetStream CRD controllers |
| nats | nats | JetStream messaging broker |
| eck-operator | elastic-system | ECK → ES + Kibana + APM Server |
| openbao | openbao | Secrets store (in-memory dev mode; prod overlay uses PVC + manual unseal) |
| external-secrets | external-secrets | OpenBao → K8s Secret bridge |
| argo-workflows | argo | Workflow execution engine |
| qdrant | qdrant | Vector memory backend |
| otel-collector | observability | OTLP receive → ES/APM export + token-usage → token-meter /ingest (CH5b) |
| keese-token-meter | monitoring | Relabel gateway token-usage → `keese_token_budget_consumed_total` (ADR 30 / CH5b) |
| prometheus (dev) | monitoring | Scrapes the meter; authoritative consumed-token store the TokenBudget reconciler reads (10b) |

## OpenBao (dev mode → automatic; prod → manual unseal)

> **Dev divergence.** Local kind runs OpenBao in **dev mode**
> (`server.dev.enabled: true` in
> [values/openbao.yaml](values/openbao.yaml)) — auto-unsealed on every
> restart with the well-known root token `root` and **in-memory**
> storage. This is the demo-fast path documented in
> [docs/plans/demo/README.md](../../docs/plans/demo/README.md) DD-2.
> Tilt + `seed.sh` re-populate the kv-v2 paths every loop, so data loss
> on restart is acceptable.
>
> **Production uses Shamir manual unseal (or cloud KMS auto-unseal) on
> PVC-backed storage.** See
> [values/openbao-prod.yaml.example](values/openbao-prod.yaml.example).
> The prod overlay (TD-P2-09) wires the manual-init + unseal flow; the
> seed script's `_enable_kv` step then runs once `BAO_TOKEN` is set.

### Dev — nothing to do

```bash
# After `make bootstrap-infra` completes, OpenBao is auto-unsealed.
# Tilt runs scripts/dev/seed-openbao.sh automatically. To reseed manually:
export BAO_ADDR=http://localhost:8200
export BAO_TOKEN=root
scripts/dev/seed-openbao.sh
```

If `ANTHROPIC_API_KEY` is set in `.env.local`, the seed script writes
the live key at `keese/tenants/tenant-a/anthropic`. Otherwise it writes
empty placeholders that the operator fills later.

### Prod — copy the example, init, unseal

```bash
# Copy the gitignored prod-values overlay into place.
cp dev/bootstrap/values/openbao-prod.yaml.example \
   dev/bootstrap/values/openbao-prod.yaml          # gitignored
# Edit to point seal stanza at your KMS.

# After install, init + capture unseal keys + root token (first time only).
kubectl exec -n openbao openbao-0 -- bao operator init

# Shamir unseal — repeat with 3 of the 5 keys printed by `init`.
kubectl exec -n openbao openbao-0 -- bao operator unseal <key-1>
kubectl exec -n openbao openbao-0 -- bao operator unseal <key-2>
kubectl exec -n openbao openbao-0 -- bao operator unseal <key-3>

# Export the captured root token and seed.
export BAO_ADDR=http://openbao.openbao.svc.cluster.local:8200
export BAO_TOKEN=<root-token>
scripts/dev/seed-openbao.sh
```

For HA prod, the prod-values example also documents `server.ha.enabled:
true` with a 3-node Raft quorum and a `seal "awskms"` /
`seal "gcpckms"` / `seal "azurekeyvault"` stanza so the cluster
auto-unseals on rolling restart.

## OpenFGA seed (automated via Tilt)

The Tiltfile triggers the `openfga-seed` Job automatically after `bootstrap-infra`.
To reseed manually:

```bash
kubectl delete job openfga-seed -n openfga --ignore-not-found
kubectl apply -k dev/bootstrap/openfga/
```

## NATS streams (automated via Tilt)

```bash
# Reapply streams and consumers.
kubectl apply -k dev/bootstrap/nats/
```

## Cosign webhook + FeatureGate seeds (CH7)

`make bootstrap-infra` applies `dev/bootstrap/cosign-webhook/` after helmfile
sync. The overlay bundles `config/cosign-webhook/` (Deployment + fail-closed
`ValidatingWebhookConfiguration` + cert-manager serving cert) and
`config/featuregates/` (the `cosign-installplan-verify` / `-failclosed` seed
`FeatureGate` CRs + the kyverno ClusterPolicy guarding `keese-features`). The
FeatureGate CRD is SSA-applied just before the overlay so the seeds land even
before Tilt installs the operator overlay.

This satisfies two of EH8's three admission-flip preconditions (webhook
config + seed gate exist). The webhook **Deployment** only goes `Available`
once its image is in the cluster — the `:dev` tag is never pushed (rule
05.15), so load it locally; until then `bootstrap-infra` prints a non-fatal
notice and EH8's admission step self-skips:

```bash
make cosign-webhook-load   # docker build + kind load keese-cosign-webhook:dev
```

> **OLM not in the local loop.** EH8's *full* proof (unsigned `InstallPlan` →
> DENY → flip gate → ADMITTED) also needs the OLM `InstallPlan` CRD. The
> bootstrap does not install OLM (too heavy) — a documented follow-up. EH8
> steps 0–2 (CR-reconcile + projection flip) run today.

## Goose-runtime image (EH10 real drain)

The `agentruntime-drain` suite (EH10) runs the real `keese-drain` baked into
`goose-runtime`; its `check-drain-image.sh` gate self-skips when the image is
absent. `make test-e2e-extended` does **not** auto-build it — load it first
(the other six suites skip the docker-build cost):

```bash
make goose-runtime-load    # docker build + kind load goose-runtime:dev
make e2e-images-load       # or load both EH8 + EH10 images at once
```

## Token-meter metering hop (CH5b · ADR 30)

`make bootstrap-infra` SSA-applies `dev/bootstrap/token-meter/` (which pulls in
`config/token-meter/`) after the cosign webhook. It deploys, in the `monitoring`
namespace:

- **keese-token-meter** — the CH5a binary. Serves one HTTP listener on `:8080`:
  `POST /ingest` (the Tier-1 OTEL collector posts each gateway token-usage
  record here as `{request_id,tenant,workspace,model,tokens_in,tokens_out,
  final}`), `/metrics` (the relabeled
  `keese_token_budget_consumed_total{tenant,workspace,model,direction}` contract
  series), and `/healthz` + `/readyz`.
- **prometheus (dev)** — scrapes the meter's `/metrics` and serves PromQL at
  `http://prometheus.monitoring.svc:9090`, the exact endpoint the TokenBudget
  reconciler's client targets (`internal/controller/policy/prom_http.go`). CH5c
  un-stubs that reconciler against this live series.
- **fail-closed NetworkPolicies** (wildcard-free, rule 05.5): only the Tier-1
  collector → meter:8080 (`/ingest`) and Prometheus → meter:8080 (`/metrics`)
  may reach the meter; the meter egresses to DNS only.

The Tier-1 export side lives in `dev/bootstrap/values/otel-collector.yaml`
(the `metrics/tokenusage` pipeline + `otlphttp/token-meter` exporter). The
otel-collector helmfile release is still gated behind the tech-debt re-enable
(see `helmfile.yaml` Layer 2), so locally the meter + Prometheus run and the
contract series materializes the moment the collector is restored or usage is
POSTed to `/ingest` directly.

The meter image is the floating `:dev` tag (rule 05.12). `make bootstrap-infra`
does **not** build it — load it first:

```bash
make token-meter-load      # docker build cmd/token-meter/Dockerfile + kind load
make e2e-images-load       # or load goose-runtime + cosign-webhook + token-meter
```

Until the image is loaded the Deployment stays Pending and the consumed series
is empty (bootstrap-infra logs this and continues — non-fatal, fail-open).

## Reseed everything

```bash
make kind-down && make kind-up bootstrap-infra tilt-up
```

## Reset a single component

```bash
helmfile -f dev/bootstrap/helmfile.yaml destroy --selector name=openfga
helmfile -f dev/bootstrap/helmfile.yaml sync     --selector name=openfga
```

## Timing target

`make kind-up bootstrap-infra` should complete in ≤ 300 seconds on a modern laptop
with a warm Docker layer cache. Measured by `scripts/dev/time-bootstrap.sh`.

## Refs

- Plan Phase 7: `/Users/marshallmccain/.claude/plans/you-are-an-expert-iridescent-alpaca.md`
- Helmfile: `dev/bootstrap/helmfile.yaml`
- Kind config: `dev/kind/ctlptl.yaml`, `dev/kind/kind-config.yaml`
- OTEL topology: `docs/designs/10a-otel-topology.md`
- Token metering pipeline: `docs/designs/30-token-metering-pipeline.md` (CH5b hop)
- Secrets: `docs/designs/11-secrets-pluggable-vault.md`
- OpenFGA model: `docs/designs/04a-openfga-authz-model.md`
