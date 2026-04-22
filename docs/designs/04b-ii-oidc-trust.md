<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: authz
depends: [04b-projected-sa-identity.md]
related_skills: []
status: current
last_verified: 2026-04-21
rollback: |
  Revert the OIDC provider configuration in the relevant OpenTofu module
  (deploy/opentofu/{aws,gcp,azure}/); re-apply via tofu apply. Cloud IAM changes
  take up to 15 minutes to propagate; old trust policies remain in effect during
  that window. Document in docs/plans/migration-sa-<slug>.md.
---

# 04b-ii — OIDC Trust Anchoring Per Cloud

Split from [04b-projected-sa-identity.md](04b-projected-sa-identity.md) per
200-line rule. This doc holds the cloud-specific trust policy detail that 04b
references by pointer.

## Audience scope (04b iter-3)

Only the **`egress`** audience template (`keese-egress-<tenant>`) is federated
to cloud IAM. The other two named templates introduced in 04b iter-3 — **`workflowRun`**
(`keese-wf-<workflow-run-uid>`) and **`supervisor`** (`keese-supervisor-<workspace-uid>`) —
are consumed only by in-cluster services (the 09 NATS bridge and the 08b ACP bridge,
respectively) and are NEVER configured as accepted audiences in any cloud-IAM trust
policy. This separation is structural: a stolen `workflowRun` token cannot satisfy a
cloud IAM trust policy because no cloud trust policy lists `keese-wf-*` as an allowed
`aud`. The per-cloud configurations in the rest of this doc apply ONLY to the `egress`
audience.

## OIDC Issuer

The Kubernetes API server is the OIDC issuer. Discovery endpoint:
`https://<apiserver>/.well-known/openid-configuration`. JWKS endpoint:
`https://<apiserver>/openid/v1/jwks`.

For clusters where the API server is not publicly reachable, operators MUST
expose the JWKS endpoint via a stable public URL. Supported options:

| Option | Complexity | Notes |
|---|---|---|
| Cloud-managed OIDC (EKS/GKE/AKS) | Low | Cloud provider publishes JWKS automatically. Preferred. |
| Dex frontend | Medium | Federated OIDC proxy; adds HA. Requires cert-manager. |
| Pinniped | Medium | Kubernetes-native; integrates with upstream IDPs. |
| Custom reverse proxy | High | Discouraged unless air-gapped. Must rotate CA certificates. |

Operator env var `OIDC_ISSUER_URL` overrides the auto-detected issuer. If unset
and the API server URL is a private CIDR (`10/8`, `172.16/12`, `192.168/16`),
the operator emits a startup warning `OIDCIssuerPrivate` and degrades
`BackendSecurityPolicy` conditions for OIDC-backed upstreams.

## AWS: STS AssumeRoleWithWebIdentity

**OIDC provider registration (once per cluster):** Provisioned by
`deploy/opentofu/aws/iam.tf`. The provider ARN is stored in
`Workspace.status.cloudRefs.awsOIDCProviderARN` (read-only, set by controller).

**Per-tenant IAM role trust policy (per tenant):**

```json
{
  "Condition": {
    "StringEquals": {
      "<oidc-provider-url>:aud": "keese-egress-<tenant>",
      "<oidc-provider-url>:sub": "system:serviceaccount:<ns>:keese-ws-<name>"
    }
  }
}
```

Both `aud` and `sub` are required. Using only `aud` allows any workspace in
the tenant to assume the role; using only `sub` allows the workspace to assume
any role that trusts the cluster. The per-tenant IAM role ARN is referenced in
`BackendSecurityPolicy.spec.targetRef.aws.roleArn`.

## GCP: Workload Identity Federation

**Pool + provider (once per cluster):** Provisioned by
`deploy/opentofu/gcp/iam.tf`. Provider type: `oidc`; issuer URI: cluster OIDC
issuer URL. Attribute mapping:
`google.subject = assertion.sub; attribute.audience = assertion.aud`.

**Per-tenant binding (per tenant):**

Attribute condition on the provider: `assertion.aud == "keese-egress-<tenant>"`.

Service account impersonation IAM binding:
```
roles/iam.workloadIdentityUser
  member: principal://iam.googleapis.com/projects/<proj>/locations/global/
           workloadIdentityPools/<pool>/subject/
           system:serviceaccount:<ns>:keese-ws-<name>
```

The GCP service account email is referenced in
`BackendSecurityPolicy.spec.targetRef.gcp.serviceAccountEmail`.

## Azure: Entra Federated Identity Credential

**Managed identity (per tenant):** Provisioned by
`deploy/opentofu/azure/identity.tf`. Each tenant gets one user-assigned managed
identity; one federated credential per workspace SA:

| Field | Value |
|---|---|
| Issuer | Cluster OIDC discovery URL |
| Subject | `system:serviceaccount:<ns>:keese-ws-<name>` |
| Audience | `keese-egress-<tenant>` |

Azure validates all three fields; subject must be an exact match (no wildcard
support as of API version 2022-01-31). The managed identity client ID is
referenced in `BackendSecurityPolicy.spec.targetRef.azure.clientId`.

CLI equivalent (for manual provisioning):
```
az identity federated-credential create \
  --name keese-ws-<workspace-name> \
  --identity-name keese-<tenant> \
  --resource-group <rg> \
  --issuer <oidc-issuer-url> \
  --subject system:serviceaccount:<ns>:keese-ws-<name> \
  --audiences keese-egress-<tenant>
```

## JWKS Caching at Gateway

The Envoy AI Gateway caches the JWKS response for 5 minutes (fail-open window).
After the cache expires and a fresh JWKS fetch fails, the gateway fails closed:
new tokens cannot be verified and requests are rejected with 401. This is
consistent with rule 05 fail-closed network policy.

Alert threshold: `keese_gateway_jwks_fetch_failures_total > 0 for 5m` → P2.

## Refs

- [04b-projected-sa-identity.md](04b-projected-sa-identity.md) — parent design
- [05b-credential-injection-patterns.md](05b-credential-injection-patterns.md) — BackendSecurityPolicy refs
- [deploy/opentofu/aws/iam.tf](../../deploy/opentofu/aws/) — AWS OIDC provider
- [deploy/opentofu/gcp/iam.tf](../../deploy/opentofu/gcp/) — GCP WIF pool + provider
- [deploy/opentofu/azure/identity.tf](../../deploy/opentofu/azure/) — Azure managed identity
- [../plans/rubric.md](../plans/rubric.md)
- [AWS IRSA docs](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)
- [GCP WIF docs](https://cloud.google.com/iam/docs/workload-identity-federation)
- [Azure Federated Identity docs](https://learn.microsoft.com/en-us/azure/active-directory/develop/workload-identity-federation)
