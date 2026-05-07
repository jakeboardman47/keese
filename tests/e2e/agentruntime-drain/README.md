<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# agentruntime-drain kuttl test

Tests the `AgentRuntime` Drain → Resume round-trip (TD-P1-02).

## What it validates

| Step | What happens |
|---|---|
| 00 | Namespace, PVC, and memory ConfigMap created |
| 01 | Writer pod writes a mock SQLite file to the session PVC |
| 02 | Drain pod writes the JSON checkpoint marker atomically (simulates preStop hook) |
| 03 | Resume pod asserts the checkpoint marker AND mock SQLite file both exist on the PVC, proving memory survived the pod-delete cycle |

## Prerequisites

- A kind cluster with `kind-keese` context (`make cluster-up` or `ctlptl apply -f dev/kind.yaml`)
- `kuttl` CLI in PATH (`brew install kuttl` or via Nix flake)
- Operator running in the cluster OR at least the CRDs installed (`make install`)

## Run locally

```sh
kubectl kuttl test tests/e2e/agentruntime-drain \
  --config tests/e2e/kuttl-config.yaml \
  --namespace drain-test
```

If a kind cluster is unavailable, the manual repro steps are:

```sh
# 1. Create namespace + PVC
kubectl create ns drain-test
kubectl apply -f tests/e2e/agentruntime-drain/00-setup.yaml

# 2. Run writer pod
kubectl apply -f tests/e2e/agentruntime-drain/01-write-memory.yaml
kubectl wait --for=condition=Ready=false --timeout=60s pod/drain-test-writer -n drain-test

# 3. Run drain pod
kubectl apply -f tests/e2e/agentruntime-drain/02-drain.yaml
kubectl wait --for=condition=Ready=false --timeout=60s pod/drain-test-drainer -n drain-test

# 4. Run resume pod — asserts memory survived
kubectl apply -f tests/e2e/agentruntime-drain/03-resume.yaml
kubectl wait --for=condition=Ready=false --timeout=60s pod/drain-test-resumer -n drain-test
kubectl logs drain-test-resumer -n drain-test
```

## Acceptance criteria

- All three pods exit with `phase: Succeeded`.
- `drain-test-resumer` logs contain `Resume complete — memory survived drain/restart cycle`.
