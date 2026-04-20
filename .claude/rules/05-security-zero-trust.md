<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Zero-trust security (always loaded)

Keese's threat model assumes an agent runtime pod is potentially
compromised. These invariants prevent a compromised agent from
exfiltrating credentials, reaching arbitrary upstreams, or escalating
privileges. If a rule below conflicts with any other rule, **this
rule wins** (see precedence in
[`01-conventions.md`](01-conventions.md)).

## Agent identity

1. **No kubeconfigs in agent pods.** Agent runtime pods never mount a
   Kubernetes API kubeconfig. Controller/operator pods may; agents may
   not.
2. **No upstream API keys in agent pods.** Anthropic keys, OpenAI
   keys, GitHub PATs, database DSNs, vector DB tokens — none of these
   ever appear as env vars, mounted files, or image-baked secrets on
   an agent pod.
3. **Identity = projected SA token.** The only credential an agent pod
   carries is a projected ServiceAccount token with audience
   `keese-egress-<tenant>` and TTL ≤ 10m. Per-tenant audience values
   tighten upstream IAM trust policies.

## Network

4. **All egress through Envoy AI Gateway, fail-closed.** Agent pods
   open exactly one egress path — to the in-cluster Envoy AI Gateway
   `Service` on 443. Direct egress to the internet is blocked by
   NetworkPolicy.
5. **No wildcard NetworkPolicies.** `podSelector: {}` with `to: []`,
   or any policy that allows unbounded egress, is forbidden. Every
   policy enumerates specific endpoints or the gateway service.
6. **Credential swap at gateway via `BackendSecurityPolicy`.** The
   gateway terminates the agent's SA token, evaluates OpenFGA via
   `extAuth`, selects a `Backend`, and injects the upstream-specific
   credential (static key, OIDC-STS-exchanged token, or dynamic vault
   cred). Credentials are cached per gateway pod, keyed by
   `(tenant-audience, upstream role)`, refreshed at 70% TTL, fail-closed
   past 95% TTL. See `docs/designs/17-credential-broker.md`.

## Secret material

7. **Secrets as projected files, never env vars.** Any K8s Secret
   reaching a pod mounts at `/var/run/keese/secrets/<name>` via
   `projected.sources[].secret`. `envFrom.secretRef` and
   `env.valueFrom.secretKeyRef` are forbidden on keese-managed pods.
8. **OpenBao (or cloud KMS) is source of truth.** Upstream credentials
   live in OpenBao / AWS Secrets Manager / GCP Secret Manager / Azure
   Key Vault. ExternalSecrets Operator (or OpenBao Secrets Operator)
   bridges to K8s Secrets referenced by `BackendSecurityPolicy`.

## ReBAC

9. **Every authz-affecting CRD field carries
   `// +keese:rebac-tuple=<relation>`.** Absence blocks merge. Tuples
   shapes live in `docs/specs/egress-authz-protocol.md`. Any change to
   tuple shapes requires opus-tier review (agent: `rebac-modeler`).
10. **Ext_authz logging** captures `(tuple, SA, host, decision,
    upstream_status)` — never tokens, never request/response bodies.

## Pod security

11. No `hostNetwork`, `hostPID`, `hostIPC`, `privileged: true`, or
    `allowPrivilegeEscalation: true` on any keese-managed pod.
    SecurityContext is explicit per container; `readOnlyRootFilesystem:
    true` required for agent pods (writes go to the session PVC).
12. **Images pinned by digest in CSVs and production overlays.** Tags
    only in `dev/` overlays. Bundle image + operator image carry
    Sigstore cosign keyless OIDC signatures; `cosign verify` with
    identity-regexp `https://github.com/keese-ai/keese/.github/workflows/.*`
    and `oidc-issuer https://token.actions.githubusercontent.com`.

## Break-glass

13. Annotations matching `keese.ai/unsafe-*` are rejected by admission
    unless the namespace carries label
    `keese.ai/break-glass=true`. Break-glass events are logged,
    eventrecorded with reason `UnsafeAnnotationAllowed`, and auto-logged
    to `MEMORY.md`.

## Contexts

14. `kubectl` calls targeting a context matching `prod-*`,
    `*production*`, or `*prd*` are **denied** in
    `.claude/settings.json`. Production changes happen through OLM +
    CI, not local `kubectl`.
15. `docker push` is denied for local sessions; images publish only
    from GitHub Actions (OIDC-scoped).

## Supply chain

16. SBOMs generated via `syft`, attested via `cosign attest` on release.
    OpenSSF Scorecard runs weekly; high-severity failures block
    release.
17. Dependencies added only after license + maintenance audit; CVE
    scan clean on `govulncheck ./...`. No dep vendored until imported.
