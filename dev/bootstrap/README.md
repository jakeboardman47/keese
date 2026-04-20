<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: developer-experience
status: current
last_verified: 2026-04-19
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
    otel-collector
       ↓
    keese-operator (Tilt live-reload)
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
| openbao | openbao | Secrets store (PVC-backed) |
| external-secrets | external-secrets | OpenBao → K8s Secret bridge |
| argo-workflows | argo | Workflow execution engine |
| qdrant | qdrant | Vector memory backend |
| otel-collector | observability | OTLP receive → ES/APM export |

## OpenBao manual steps (after bootstrap-infra)

OpenBao uses manual unseal (no auto-unseal) for dev parity with prod:

```bash
# Initialize and capture unseal keys + root token (first time only).
kubectl exec -n openbao openbao-0 -- bao operator init

# Unseal (repeat 3× with different keys if using default 5-of-3 split).
kubectl exec -n openbao openbao-0 -- bao operator unseal <key>

# Export token and run the seed script.
export BAO_ADDR=http://localhost:8200
export BAO_TOKEN=<root-token>
scripts/dev/seed-openbao.sh
```

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
- Secrets: `docs/designs/11-secrets-pluggable-vault.md`
- OpenFGA model: `docs/designs/04a-openfga-authz-model.md`
