<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - D1-controller-wiring.md
  - D2-runtime-spi-minimum.md
  - D3-cluster-bootstrap.md
  - ../../../Makefile
related_skills: [plan-management, infra-bootstrap]
status: planned
last_verified: 2026-04-25
---

# D4 — Cloud deploy (single-cloud, single-tenant, single-LLM)

**Refinement pass:** operational readiness.
**Effort:** 3–4 h. **Owner agent:** `infra-bootstrap`.

## Goal

Stand up a real cluster, push the operator + bundle images, install OLM,
install keese, run the bootstrap, and apply the demo manifests. End-state:
`kubectl get workspace` returns `Ready: True` against the cloud cluster.

## Decision gate

| Option | Wall time | Cost | Notes |
|---|---|---|---|
| **GKE Autopilot** *(recommended)* | ~5 min | $0 control plane, ~$0.30/hr workloads | CSI/PDs work without IAM gymnastics; nodes scale from 0 |
| EKS Auto Mode | ~12 min | $0.10/hr control plane + nodes | Acceptable fallback if no GCP access |
| AKS | ~30 min | $0 control plane + nodes | Slowest provisioning; pick only if Azure is required |

Per [DD-1](README.md#decisions-specific-to-this-track), default to GKE
Autopilot. The plan below assumes GKE; AWS/Azure variants are noted
inline.

## Tasks

### T1 — Provision cluster

```sh
gcloud container clusters create-auto keese-demo --region us-central1 \
  --release-channel rapid --enable-private-nodes=false
gcloud container clusters get-credentials keese-demo --region us-central1
kubectl config rename-context "$(kubectl config current-context)" gke-keese-demo
```

Rule 05.14 denies contexts matching `prod-*` / `*production*` / `*prd*`.
The `gke-keese-demo` name passes — confirm
[.claude/settings.json](../../../.claude/settings.json) has no overlapping
deny. EKS fallback: `eksctl create cluster --name keese-demo --region
us-east-1 --node-type m6i.xlarge --nodes 2`. AKS fallback: `az aks create
-g keese -n keese-demo --enable-managed-identity --node-count 2`.

Acceptance: `kubectl --context gke-keese-demo get nodes` returns ≥1 Ready.

### T2 — Push images via CI tag flow

`git tag v0.0.1-demo.1 && git push origin v0.0.1-demo.1` — the
[image.yaml](../../../.github/workflows/image.yaml) +
[bundle.yaml](../../../.github/workflows/bundle.yaml) workflows do
buildx multi-arch + cosign keyless OIDC + syft SBOM attest. Capture
`ghcr.io/keese-ai/keese@sha256:<digest>` and
`ghcr.io/keese-ai/keese-bundle@sha256:<digest>`.

Acceptance: both digests resolve via `crane manifest`; `cosign verify
<ref> --certificate-identity-regexp
'https://github.com/keese-ai/keese/.github/workflows/.*'
--certificate-oidc-issuer https://token.actions.githubusercontent.com`
returns 0.

**Fallback** if CI is broken: `make docker-build docker-push IMG=...`
then `make bundle bundle-build bundle-push BUNDLE_IMG=...`. Local push
misses cosign signing — log as tech-debt P1.

### T3 — Install OLM

```sh
operator-sdk olm install --version v0.31.0
```

Verify: `kubectl get pods -n olm` shows `olm-operator` and
`catalog-operator` Running.

### T4 — Install keese bundle

```sh
operator-sdk run bundle ghcr.io/keese-ai/keese-bundle@sha256:<digest> \
  --namespace keese-system \
  --timeout 5m
```

This creates a Subscription, InstallPlan, OperatorGroup, and CSV. Watch
with `kubectl get csv -n keese-system -w` until `Phase: Succeeded`.

Acceptance: `kubectl get pods -n keese-system` shows
`keese-controller-manager` Running with the image digest from T2.

### T5 — Run bootstrap

```sh
export KUBECONFIG=$HOME/.kube/config
kubectl config use-context gke-keese-demo
make bootstrap-infra
```

This runs the helmfile sync from D3 (NATS, Envoy AI Gateway, OpenFGA,
OpenBao, ExternalSecrets, cert-manager, Capsule, kyverno, eck-operator,
argo-workflows, qdrant, otel-collector) and applies the LLM-credential
manifests authored in D3-T2 + the cert-manager CA from D3-T5.

Manual step: before running bootstrap, set
`ANTHROPIC_API_KEY` in `.env.local` (gitignored). The seed.sh edit from
D3-T3 reads it.

Acceptance: `kubectl get pods -A | grep -v Running | grep -v Completed`
returns 0 rows after 5 min.

### T6 — Apply demo manifests

Apply in dependency order, waiting for each to reach Ready before the
next: `tenancy/tenant-minimal.yaml`, `runtime_v1alpha1_agentruntime.yaml`,
`workspace_v1alpha1_workspace.yaml`, `memory_v1alpha1_memory.yaml`,
`workspace/workspacesession-minimal.yaml` (all under `config/samples/`).

Acceptance: `kubectl get tenant,workspace,workspacesession,memory -A` all
show `Ready: True`.

### T7 — Health snapshot

Capture `kubectl get nodes -o wide`, `kubectl get csv -n keese-system`,
`kubectl get pods -A`, `kubectl get tenant,workspace,workspacesession,
memory,agentruntime,oidcprovider -A`, and `kubectl describe workspace
my-ws -n alpha` to `.plan-logs/D4-deploy-<timestamp>.txt`. This is the
baseline for D5 smoke.

## Out of scope (→ tech-debt §deploy)

- OpenTofu modules under `deploy/opentofu/` (designed in 21, no code).
- `config/overlays/prod/` with image digest pinning + resource tuning.
- Multi-AZ + storage-class tuning for stateful sets (NATS, OpenFGA, OpenBao).
- Cluster-level OPA/Conftest Rego policies.
- Backup + DR for OpenBao.

## Verification

- T7 health snapshot shows all kind-of resources Ready.
- `kubectl get installplan -n keese-system -o yaml` shows
  `installPlanApproval: Manual` (sets the upgrade gate per
  [docs/designs/14a-olm-channels-upgrades.md](../../designs/14a-olm-channels-upgrades.md)).
  If it landed Automatic, patch it.

## Failure modes

| Failure | Symptom | Recovery |
|---|---|---|
| GKE Autopilot rejects PDB / privileged | bootstrap pod stuck Pending | Audit each chart's helm values for Autopilot compatibility; NATS sometimes needs `securityContext` tweaks |
| OLM bundle install times out | InstallPlan stuck Installing | Check operator pod logs; common cause is missing CRD dep (cert-manager not yet installed) |
| Image pull fails | ImagePullBackOff | Make package public on GHCR or add an imagePullSecret to the operator Subscription |
| ExternalSecrets stuck | ES `SecretSyncedError` | OpenBao not unsealed yet — wait or re-run seed |
| Workspace stuck `Pending PVC` | StorageClass missing | GKE Autopilot ships `standard-rwo`; ensure default is set; otherwise add `spec.sessionPVC.storageClassName: standard-rwo` to the sample |

## Rollback

`operator-sdk cleanup keese --namespace keese-system` tears down the
operator. `make undeploy` removes any non-bundle manifests. To delete
the cluster: `gcloud container clusters delete keese-demo
--region us-central1 --quiet`.

## Iteration log

### Iteration 1 — 2026-04-25

| # | Category | Weight | Ratio | Score | Notes |
|---|---|---:|---:|---:|---|
| 1 | Scope clarity | 10 | 1.0 | 10 | One cloud, one tenant, one LLM, ordered apply |
| 2 | Architecture fit | 10 | 1.0 | 10 | Honors rule 05.14 (no prod context); 05.12 (cosign verify) |
| 3 | Security posture | 15 | 1.0 | 15 | API key only in OpenBao; cosign verify before install |
| 4 | Automatability | 10 | 0.5 | 5 | gcloud cluster creation is manual one-shot; bootstrap is `make` |
| 5 | Verifiability | 15 | 0.5 | 7.5 | Acceptance checks per task; no automated e2e in this phase (D5) |
| 6 | Failure-mode awareness | 10 | 1.0 | 10 | 5-row table covers Autopilot, bundle, image, ES, PVC |
| 7 | Context efficiency | 10 | 1.0 | 10 | <200 lines; cmd snippets only where needed |
| 8 | Docs quality | 5 | 1.0 | 5 | SPDX + frontmatter + relative links |
| 9 | Observability | 5 | 0.5 | 2.5 | T7 snapshot; no Grafana/dashboard wiring |
| 10 | Operational readiness | 10 | 1.0 | 10 | Rollback documented; manual install-plan approval verified |
| | **Total** | 100 | | **85** | |

Verdict: SHIP

Top gaps:
1. Cluster provisioning is a one-shot manual command — OpenTofu modules deferred to tech debt.
2. Image push via CI requires GitHub Actions to be green at exactly the right moment; fallback exists but skips signing.
3. No automated e2e — D5 covers it manually.

Next step: T1 first; T2 in parallel with T1; T3–T5 sequential after T1+T2; T6+T7 last.
