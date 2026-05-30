<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: runbook
depends:
  - ../plans/demo/README.md
  - ../../dev/demo/hello-keese.yaml
  - ../../Makefile
related_skills: []
status: current
last_verified: 2026-04-27
---

# Demo runbook — local kind path

End-to-end keese demo on a local kind cluster. Verified 2026-04-27.
Cloud variant lives in [demo-runbook-cloud-deferred.md](demo-runbook-cloud-deferred.md).

## Pre-flight

```sh
kind --version           # v0.20+
docker info >/dev/null   # daemon up (orbstack works on macOS)
nix --version            # the dev shell needs nix; or have go 1.24, controller-gen, kustomize on PATH
```

The flake provides `make`, `go`, `kubectl`, `kustomize`, `controller-gen`,
and `helmfile` — everything the runbook calls except `docker` and `kind`.
Prefix every command below with `nix develop --command bash -c "..."` if
you do not have those tools on PATH directly.

## 1. Bring up the cluster

```sh
kind create cluster --name=keese-demo \
  --config=dev/kind/kind-config-demo.yaml --wait=120s
kubectl config use-context kind-keese-demo
```

A single-control-plane + single-worker kind cluster is sufficient. The
heavier 4-node `dev/kind/kind-config.yaml` is for the full bootstrap
stack and is not required for this demo.

## 2. Build + load operator image

```sh
docker buildx build --platform linux/arm64 -t keese:demo --load -f Dockerfile .
kind load docker-image keese:demo --name keese-demo
```

Adjust `--platform` to `linux/amd64` on Intel.

## 3. Install CRDs + operator

```sh
make install                       # 17 CRDs
make deploy IMG=keese:demo         # operator + bootstrap CRs
kubectl -n keese-system rollout status deploy/keese-controller-manager --timeout=60s
```

Verify all controllers started:

```sh
kubectl -n keese-system logs deploy/keese-controller-manager | grep "Starting Controller"
# expect 17 Starting Controller lines
```

## 4. Apply the demo manifest

```sh
kubectl create namespace alpha
kubectl apply -f dev/demo/hello-keese.yaml
```

Wait ~30 seconds, then:

```sh
kubectl get tenant,agentruntime,workspace,memory,workspacesession -A
```

Expected:

```
NAME                                     READY   PHASE
tenant/alpha                             True    Active
agentruntime/goose-default               True    Ready  (Provider: goose)

NAMESPACE  NAME            READY   PHASE     RUNTIME
alpha      workspace/my-ws True    Running   goose-default

NAMESPACE  NAME                  READY   PHASE     SUBJECT
alpha      workspacesession/    True    Active    user:alice@example.com
```

The session pod is named after a hash of `(workspace UID, subject)`:

```sh
kubectl -n alpha get pod -l keese.ai/session=my-session
# pod/ws-XXXXXXXX-sess-YYYYYYYY  1/1  Running
```

Confirm the session reaches `Active`. If it hangs at `Attaching`, poke
the spec to bump the generation (the session reconciler does not yet
watch Pod status — TD-P1):

```sh
kubectl patch workspacesession my-session -n alpha --type=merge \
  -p '{"spec":{"attachGraceSeconds":3600}}'
```

## 5. Verify the pod environment

```sh
SESSION_POD=$(kubectl get pod -n alpha -l keese.ai/session=my-session -o name | head -1)
kubectl -n alpha exec $SESSION_POD -- sh -c '
  id; echo
  env | grep -E "GOOSE|KEESE|ANTHROPIC|HOME"; echo
  ls -la /var/run/keese/session /var/run/keese/memory /var/run/keese/tokens
  /usr/local/bin/goose --version
'
```

Expected: uid=1000(goose), gid=1000(goose); env vars `GOOSE_PROVIDER=anthropic`,
`GOOSE_MODEL=claude-opus-4-7`, `ANTHROPIC_BASE_URL=https://envoy-ai-gateway.keese-system.svc:443`,
`KEESE_SESSION_ID=<pod-name>`, `KEESE_TENANT=alpha`, `KEESE_WORKSPACE=my-ws`,
`HOME=/var/run/keese/session/home`. Volumes mounted at the standard paths.
goose binary `1.30.0`+ at `/usr/local/bin/goose`.

The projected SA token is at `/var/run/keese/tokens/egress` with audience
`keese-egress-alpha` (TTL 600s, read-only tmpfs, RFC 7519 JWT).

## 6. Inspect the network isolation

```sh
kubectl get networkpolicy -n alpha
```

Expected:

- `keese-workspace-<UID>-default-deny` — fail-closed Ingress + Egress
- `keese-workspace-<UID>-egress` — allows only :443 to the AI Gateway and :4222 to NATS in `nats`

## 7. Tear down

```sh
kubectl delete -f dev/demo/hello-keese.yaml
make undeploy
make uninstall
kind delete cluster --name=keese-demo
```

## What this runbook proves

- Operator deploys cleanly with all 17 CRDs and 17 reconcilers.
- Tenant + AgentRuntime cluster scope.
- Workspace controller produces SA + 2 NetworkPolicies + PVC, advances to
  Running once the PVC is Bound.
- WorkspaceSession produces a deterministic per-user pod with the right
  goose image, projected SA token, restricted SecurityContext (non-root,
  numeric UID 1000, readOnlyRootFilesystem, dropped capabilities).
- ReBAC tuples are written to the `FakeRebacWriter` (TD-P1-01 to swap for
  real OpenFGA).
- Memory CR provisions a ReadWriteOnce PVC sized per
  `spec.provider.sqlite.storageSize`.

## What is intentionally not exercised

- LLM round-trip through Envoy AI Gateway (helmfile bootstrap deferred).
- ExternalSecrets / OpenBao seeding (deferred with helmfile).
- Multi-tenant + cross-tenant agreement flows.
- OLM-based install (the bundle is built but installed via plain
  `make deploy`, not `operator-sdk run bundle`).

These items are tracked in
[demo-runbook-cloud-deferred.md](demo-runbook-cloud-deferred.md) and
[../plans/demo/tech-debt.md](../plans/demo/tech-debt.md).
