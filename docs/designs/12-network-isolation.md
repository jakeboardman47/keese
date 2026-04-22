<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: security
depends:
  - 01-tenancy-capsule.md
  - 02-workspace-model.md
  - 03-workflow-argo-delegation.md
  - 05a-envoy-ai-gateway-topology.md
  - 09-transport-crd.md
  - 25-cross-tenant-agreement.md
related_skills: [doc-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
rollback: |
  Workspace controller re-applies NP on every reconcile (SSA, fieldOwner:
  keese-workspace-controller). Roll back by reverting the operator image via
  OLM replaces chain; next reconcile loop re-instates prior NP shape.
  Emergency: kubectl patch NetworkPolicy <name> -n <ns> --type=merge
  (controller will re-reconcile; use only for break-glass debugging).
---

# 12 — Network Isolation

## Context

Every workspace namespace is an untrusted blast radius. A compromised agent pod
must not reach arbitrary internet endpoints, peer namespaces, or the Kubernetes
API (zero-trust rule 05.4–05.5). The Workspace controller applies two
`NetworkPolicy` objects per namespace via SSA (`fieldOwner: keese-workspace-controller`):
(1) a fail-closed default-deny for all ingress and egress, and (2) an explicit
egress-allow for the Envoy AI Gateway (`port 443`) and the NATS JetStream cluster
(`port 4222`). Argo Workflow step pods run in the same namespace (03 iter-2) and
inherit both policies automatically — no additional NP authoring is required.

## Decision: policy lifecycle owner

**Workspace controller via SSA, not Capsule TenantResource.** D-01.5 rationale:
`TenantResource` creates SSA field-ownership ambiguity; the controller applies both
NPs at workspace creation and re-asserts them on every reconcile (idempotent across
3+ passes per rule 04). Capsule Mode B enforces quota/LimitRange/RBAC at Tenant
level and does not touch workspace NetworkPolicies.

## NetworkPolicy templates

### NP-1 — fail-closed default-deny

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: keese-workspace-default-deny
  namespace: <workspace-namespace>
spec:
  podSelector: {}          # all pods in namespace
  policyTypes: [Ingress, Egress]
  # no ingress / egress rules → deny all
```

### NP-2 — egress allow: Envoy AI Gateway + NATS

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: keese-workspace-egress-allow
  namespace: <workspace-namespace>
spec:
  podSelector: {}
  policyTypes: [Egress]
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          keese.ai/component: envoy-ai-gateway
      podSelector:
        matchLabels:
          app.kubernetes.io/name: envoy-ai-gateway
    ports:
    - port: 443
      protocol: TCP
  - to:
    - namespaceSelector:
        matchLabels:
          keese.ai/component: nats
      podSelector:
        matchLabels:
          app.kubernetes.io/name: nats
    ports:
    - port: 4222
      protocol: TCP
  - to:                    # kube-dns — required for service resolution
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: kube-system
      podSelector:
        matchLabels:
          k8s-app: kube-dns
    ports:
    - port: 53
      protocol: UDP
    - port: 53
      protocol: TCP
```

No CIDR blocks. No wildcard `podSelector: {}` on the target namespace. Every
allow names an exact service via `namespaceSelector + podSelector` conjunction
(rule 05.5). Operators must apply label `keese.ai/component: envoy-ai-gateway`
to the gateway namespace and `keese.ai/component: nats` to the NATS namespace
during bootstrap (`helmfile`/OLM install step).

## Cross-tenant a2a egress

`spec.a2a scope: cross-tenant` (09 iter-3) does **not** add a direct pod-to-pod
egress rule. Cross-tenant a2a calls route through the Envoy AI Gateway (port 443,
already allowed by NP-2). The gateway validates the CrossTenantAgreement-backed
OpenFGA tuple (`workspace.messageable_from`) via ext_authz before forwarding.
No additional NetworkPolicy entry is needed; no wildcard is introduced.

`keese.ai/cross-tenant-peer-of` namespace labels are explicitly **not used** for
NetworkPolicy selectors. Direct pod-to-pod egress across tenant namespaces would
bypass ext_authz and violate zero-trust rule 05.4.

## Capsule integration

Capsule Mode B provides tenant-level quota/LimitRange/RBAC; it does not own
workspace NetworkPolicies (D-01.5). No `Tenant.spec.networkPolicies` is set by
keese. Platform-team Capsule NPs write to distinct names — no SSA conflict.
Capsule admission-webhook DNS/ICMP rules are additive and do not loosen the
fail-closed base.

## Argo Workflow pod isolation

Argo step pods run in the Workspace namespace (03 iter-2, same-namespace model)
and inherit NP-1/NP-2 automatically — no Argo-specific NP authoring required.
The Argo controller pod lives outside workspace namespaces and is unaffected.
Artifact upload egress (S3/GCS/MinIO) is a cross-dep for 21 (artifact store):
`spec.artifactEgressPolicy` on Workspace injects a third NP via SSA, also
label-scoped. Dev-mode MinIO egress is granted only to namespaces labeled
`keese.ai/dev-only: "true"`.

## Failure modes

| Failure | Detection | Mitigation |
|---|---|---|
| NP-2 namespace labels missing | Agent pod `EgressBlocked`; `NetworkPolicyGapDetected` event | Bootstrap must apply labels; `scripts/check-np-labels.sh` pre-flight |
| CNI does not enforce NP (kindnet) | Negative kuttl test fails in CI | ci.yaml matrix enforces Calico on kind; kindnet only for unit tests |
| NP drift (platform overwrites) | SSA conflict counter metric; `NetworkPolicyConflict` event | Controller re-asserts on next reconcile; alert after 3 conflicts in 5m |
| Argo artifact egress blocked | `WorkflowRun.status.phase=Failed`; `ArtifactEgressBlocked` event | Grant egress to artifact backend via NP-2 extension (21 cross-dep) |
| Cross-tenant a2a direct pod attempt | NP-1 drops packet silently; `A2APeerAuthzDenied` event at gateway | Design prevents direct path; all cross-tenant traffic transits gateway |
| Break-glass: legitimate traffic blocked | Cilium/Calico flow logs; `NetworkPolicyDropped` alert | Per-namespace `kubectl patch`; auto-log to MEMORY.md via rule 05.13 |

## Observability

- **OTEL spans:** `keese.network.np.applied`, `keese.network.np.conflict`.
- **Events** (`events.go`): `NetworkPolicyApplied`, `NetworkPolicyConflict`,
  `NetworkPolicyGapDetected`, `ArtifactEgressBlocked`.
- **Metrics:** `keese_workspace_np_apply_total{result}`,
  `keese_workspace_np_conflict_total{namespace}`.
- **Flow logs:** Cilium Hubble (`cilium_drop_total{reason="POLICY_DENIED"}`) or
  Calico flow logs → Elastic `keese-netflow-*`; alert on unexpected drops in
  workspace namespaces.
- **Negative-test CI signal:** kuttl test `test/e2e/network-isolation/` asserts
  `curl 1.1.1.1` from agent pod returns connection refused; positive test asserts
  gateway-service call succeeds.

## Verification (envtest / kuttl test names)

| Test | Kind | Assertion |
|---|---|---|
| `TestWorkspaceNPApplied` | envtest | NP-1 and NP-2 exist after workspace reconcile; idempotent over 3 passes |
| `TestWorkspaceNPFieldOwner` | envtest | SSA fieldOwner = `keese-workspace-controller` on both NPs |
| `TestNegativeEgressInternet` | kuttl | Pod in workspace ns cannot curl `1.1.1.1:80` (Calico-enforced kind) |
| `TestPositiveEgressGateway` | kuttl | Pod can reach Envoy AI Gateway service `443` |
| `TestPositiveEgressNATS` | kuttl | Pod can reach NATS service `4222` |
| `TestArgoStepPodInheritsNP` | kuttl | Argo step pod in same namespace cannot curl `1.1.1.1:80` |
| `TestCrossTenantDirectBlocked` | kuttl | Pod cannot reach peer workspace pod IP directly |

## Rollback / upgrade

In frontmatter. SSA ensures re-applying the prior operator image restores the
prior NP shape within one reconcile loop. No migration doc required at v1alpha1.

## Refs

- [01-tenancy-capsule.md](01-tenancy-capsule.md) · D-01.5
- [03-workflow-argo-delegation.md](03-workflow-argo-delegation.md) · iter-2 namespace model
- [05a-envoy-ai-gateway-topology.md](05a-envoy-ai-gateway-topology.md)
- [09-transport-crd.md](09-transport-crd.md) · iter-3 a2a cross-tenant
- [25-cross-tenant-agreement.md](25-cross-tenant-agreement.md) · D29
- [../plans/rubric.md](../plans/rubric.md)
- `.claude/rules/05-security-zero-trust.md` rules 4, 5
- `.claude/rules/04-kubernetes.md` rule 17

## Iteration log

See [12-ii-iter-log.md](12-ii-iter-log.md). Final: iter-3 **100/100 SHIP**.
