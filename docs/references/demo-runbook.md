<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: runbook
depends:
  - ../plans/demo/D4-cloud-deploy.md
  - ../plans/demo/D5-demo-smoke.md
  - demo-runbook-kind.md
  - demo-runbook-cloud-deferred.md
  - ../../dev/demo/hello-keese.yaml
related_skills: []
status: current
last_verified: 2026-05-01
---

# Demo runbook — cloud (live LLM round-trip)

End-to-end keese demo on a managed Kubernetes cluster (GKE Autopilot
default; EKS/AKS variants in [D4-cloud-deploy.md](../plans/demo/D4-cloud-deploy.md)).
The local-only path is in [demo-runbook-kind.md](demo-runbook-kind.md).

## Pre-flight

1. `gcloud auth login && gcloud config set project <PROJECT>` — billing on.
2. `.env.local` contains `ANTHROPIC_API_KEY=…` (gitignored; loaded by `.envrc`).
3. `cosign`, `gcloud`, `kubectl`, `helmfile`, `operator-sdk` on PATH (the
   nix flake provides all but `gcloud` and `cosign`).
4. The GHCR `keese-ai` packages must be public, or you have an
   `imagePullSecret` to pass through the OLM Subscription.

## 1. Sanity

```sh
kubectl --context gke-keese-demo get nodes
```

If this fails, see §A "Provision cluster" below — the demo assumes the
cluster already exists.

## 2. Provision Tenant

```sh
kubectl apply -f config/samples/tenancy/tenant-minimal.yaml
kubectl wait --for=condition=Ready tenant/alpha --timeout=60s
```

## 3. Provision the workspace stack

```sh
kubectl create namespace alpha --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f dev/demo/hello-keese.yaml
```

Equivalent split form (use this if the live audience prefers seeing each
kind land in order):

```sh
kubectl apply -f config/samples/runtime_v1alpha1_agentruntime.yaml
kubectl apply -f config/samples/workspace_v1alpha1_workspace.yaml
kubectl apply -f config/samples/memory_v1alpha1_memory.yaml
kubectl apply -f config/samples/workspace/workspacesession-minimal.yaml
```

## 4. Block until ready

```sh
kubectl wait --for=condition=Ready -n alpha workspacesession/my-session --timeout=120s
kubectl get tenant,agentruntime,workspace,memory,workspacesession -A
```

Every row should show `Ready=True`.

## 5. The money shot — live Anthropic round-trip

```sh
SESSION=$(kubectl get pod -n alpha -l keese.ai/session=my-session -o name | head -1)
kubectl exec -n alpha "$SESSION" -- /usr/local/bin/goose run \
  --text 'Write a Python function that returns the nth Fibonacci number.' \
  --quiet
```

Expected: a Python function in stdout, plus a successful POST
`/v1/messages` in `kubectl logs -n keese-system deploy/envoy-ai-gateway`,
plus a non-empty `/var/run/keese/memory/session.db`.

## If anything breaks (top three failure modes)

| Symptom | One-line recovery |
|---|---|
| `kubectl exec` returns 401 from Anthropic | OpenBao not unsealed; re-run `dev/bootstrap/openbao/seed.sh`, then restart the gateway pod. Verify the BSP's `secretRef.key` matches the OpenBao key name. (D3-T2/T3) |
| Session pod stuck `ContainerCreating` on the memory volume | StorageClass missing or PVC unbound — `kubectl get pvc -n alpha`; on GKE Autopilot the default class is `standard-rwo`; otherwise patch `Memory.spec.provider.sqlite.storageClassName`. (D4 failure-table row 5) |
| Gateway 404 on `/anthropic/v1/messages` | EG `extensionManager.hooks.xdsTranslator` not configured — check [dev/bootstrap/values/envoy-gateway.yaml](../../dev/bootstrap/values/envoy-gateway.yaml) and that the AI Gateway controller is reachable on `:1063` (plaintext, NOT TLS). See MEMORY 2026-04-30. |

## Tear down

```sh
kubectl delete -f dev/demo/hello-keese.yaml
kubectl delete -f config/samples/tenancy/tenant-minimal.yaml
operator-sdk cleanup keese --namespace keese-system
# delete the cluster only if you own it:
gcloud container clusters delete keese-demo --region us-central1 --quiet
```

## §A. Provision the cluster (one-time)

```sh
gcloud container clusters create-auto keese-demo \
  --region us-central1 --release-channel rapid \
  --enable-private-nodes=false
gcloud container clusters get-credentials keese-demo --region us-central1
kubectl config rename-context "$(kubectl config current-context)" gke-keese-demo
```

Then push a release tag to trigger the signed image + bundle build:

```sh
git tag v0.0.1-demo.1 && git push origin v0.0.1-demo.1
# wait for image.yaml + bundle.yaml workflows to finish
# capture the resulting digests:
crane manifest ghcr.io/keese-ai/keese:v0.0.1-demo.1        | sha256sum
crane manifest ghcr.io/keese-ai/keese-bundle:v0.0.1-demo.1 | sha256sum
cosign verify ghcr.io/keese-ai/keese-bundle@sha256:<digest> \
  --certificate-identity-regexp 'https://github.com/keese-ai/keese/.github/workflows/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Install OLM and the keese bundle:

```sh
operator-sdk olm install --version v0.31.0
operator-sdk run bundle ghcr.io/keese-ai/keese-bundle@sha256:<digest> \
  --namespace keese-system --timeout 5m
kubectl get csv -n keese-system   # wait for Phase: Succeeded
```

Run bootstrap-infra (helmfile sync + AI gateway manifests):

```sh
make bootstrap-infra
kubectl get pods -A | grep -v Running | grep -v Completed   # expect 0 rows after ~5 min
```

## See also

- [demo-runbook-kind.md](demo-runbook-kind.md) — local kind path.
- [demo-runbook-cloud-deferred.md](demo-runbook-cloud-deferred.md) —
  cloud-readiness gap survey (multi-tenant, multi-LLM, OpenTofu, etc.).
- [../plans/demo/D4-cloud-deploy.md](../plans/demo/D4-cloud-deploy.md) —
  authoring plan with full failure-mode table + rollback.
- [../plans/demo/D5-demo-smoke.md](../plans/demo/D5-demo-smoke.md) —
  the smoke checks the runbook above is built from.
- [../plans/demo/tech-debt.md](../plans/demo/tech-debt.md) — every
  shortcut taken to land this runbook.
