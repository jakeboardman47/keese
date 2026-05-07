<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: testing
status: current
last_verified: 2026-05-07
---

# chaos-network kuttl test suite (TD-P3-08)

Validates fail-closed egress behavior and controller reconvergence under
network partition and controller-restart chaos conditions.

## What it validates

| Step | File | What happens |
|---|---|---|
| 00 | `00-setup.yaml` | Tenant + Workspace + WorkspaceSession provisioned in `chaos-test` namespace |
| 01 | `01-assert.yaml` | Workspace `Ready`, Session `Active` before fault injection |
| 02 | `02-partition.yaml` | Deny-all egress NetworkPolicy injected — blocks gateway on port 443 |
| 03 | `03-assert.yaml` | `EgressUnavailable=True` within 30s; egress probe exits non-zero (no hang) |
| 04 | `04-restore.yaml` | Deny-all NetworkPolicy deleted; original allow-gateway policy resumes |
| 05 | `05-assert.yaml` | `EgressUnavailable=False` within 30s; egress recovers |
| 06 | `06-controller-restart.yaml` | `kubectl rollout restart` on keese-controller-manager |
| 07 | `07-assert.yaml` | Session pod still `Running`; session still `Active`; controller `Available` within 60s |

## Network partition semantics

Step 02 adds a `NetworkPolicy` with `egress: []` (deny all egress) in the
`chaos-test` namespace. Kubernetes NetworkPolicy is additive for ingress
and union-of-allows for egress — a deny-all egress policy with no rules
blocks all egress from all pods in the namespace regardless of other
policies present.

Step 04 deletes this policy by label (`keese.ai/chaos-fault=egress-deny`),
restoring the Workspace controller's original allow-gateway policy as sole
policy.

## Controller restart semantics

Step 06 uses `kubectl rollout restart` — it patches the Deployment's pod
template annotation, triggering a rolling replacement of the manager pod.
The WorkspaceSession pod is NOT owned by the Deployment and must NOT be
deleted. Rule 06.6 requires restart idempotency within ≤ 3 reconciles;
step 07's 60s window covers that.

## Prerequisites

- A kind cluster with `kind-keese` context:
  `make kind-up && make bootstrap-infra`
- `kuttl` (`kubectl-kuttl`) on PATH (in Nix flake or `brew install kuttl`)
- Operator deployed in `keese-system` namespace (`make deploy`)
- Envoy AI Gateway Service present at
  `envoy-gateway.envoy-gateway-system.svc.cluster.local:443`

## Run

```sh
# Via the extended target (runs all suites):
make test-e2e-extended

# Or individually:
kubectl kuttl test tests/e2e/chaos-network \
  --config tests/e2e/kuttl-config.yaml
```

## Expected duration

Under 4 minutes on a warm cluster:
- Steps 00-01: ~60s (Workspace Ready + Session Active)
- Steps 02-05: ~60s (NetworkPolicy inject + condition flip + restore)
- Steps 06-07: ~90s (rollout restart + reconvergence)
