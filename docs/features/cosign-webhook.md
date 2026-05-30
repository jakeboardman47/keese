<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: feature
category: feature
depends:
  - docs/designs/14a-olm-channels-upgrades.md
  - docs/designs/27-feature-gates-openfeature.md
implements_specs: []
implements_plans:
  - docs/plans/demo/tech-debt.md
source_refs:
  - cmd/keese-cosign-webhook/main.go:1-220
  - internal/admission/cosign/handler.go:1-232
  - internal/admission/cosign/verify.go:1-142
  - internal/admission/cosign/csv.go:1-141
  - config/cosign-webhook/validatingwebhookconfiguration.yaml:1-55
  - config/cosign-webhook/deployment.yaml:1-147
related_skills: [controller-authoring]
status: implemented
implemented_in_phase: demo-TD-P1
last_verified: 2026-05-29
---

# Supply-chain admission (keese-cosign-webhook)

## Summary

`keese-cosign-webhook` is a Kubernetes ValidatingWebhook server that enforces
Sigstore cosign keyless-OIDC signature verification on OLM `InstallPlan` objects
at admission time. When an `InstallPlan` is created or updated (i.e., when OLM
flips `spec.approved=true`), the webhook resolves each referenced
`ClusterServiceVersion`, extracts every container image from
`spec.relatedImages`, deployment containers, and init containers, and shells out
to the cosign binary to verify that any image under `ghcr.io/keese-ai/` carries a
valid keyless signature anchored to the keese-ai GitHub Actions OIDC identity.
Two alpha FeatureGates (`cosign-installplan-verify`,
`cosign-installplan-failclosed`) allow graduated rollout: verification is
shipped-but-quiet by default and enabled per cluster via override. Break-glass
bypass is available when a namespace label and InstallPlan annotation are both
present (rule 05.13).

## Behavior

- **Gate check first.** If `cosign-installplan-verify=false` the webhook
  passes through unconditionally with reason `AllowedFeatureGateOff`
  (handler.go:88-94). When `Gates` is nil (no ConfigMap projection yet),
  verification runs in full fail-closed mode — defaults defined at
  main.go:149-154.
- **Decode and break-glass check.** The webhook decodes the incoming
  `InstallPlan` (unstructured) and checks for the break-glass pair: annotation
  `keese.ai/unsafe-allow-unsigned=true` on the InstallPlan AND label
  `keese.ai/break-glass=true` on the namespace. Both must be present; either
  alone is not honored (handler.go:190-199). Break-glass is admitted with reason
  `AllowedBreakGlass`.
- **CSV resolution.** `spec.clusterServiceVersionNames` is read from the
  InstallPlan; each CSV is fetched from the same namespace via the unstructured
  client. A missing CSV is a hard denial (`InstallPlanCSVUnreadable`) — without
  the manifest the supply chain cannot be evaluated (csv.go:125-141).
- **Image extraction.** Per CSV, images are deduplicated and sorted from three
  sources: `spec.relatedImages[].image`, deployment `containers[].image`, and
  `initContainers[].image` (csv.go:66-120).
- **Registry gating.** Only images matching the `AllowedRegistryPrefixes` list
  (default `ghcr.io/keese-ai/`) are verified. Images outside this prefix pass
  through; non-keese operators in the same namespace are not subject to keese's
  supply-chain checks (verify.go:91-98).
- **Digest pinning enforced pre-flight.** Any image reference that does not
  contain `@sha256:` is immediately denied with reason `BundleNotDigestPinned`
  before cosign is invoked (verify.go:119-121).
- **cosign exec.** For each gated image the verifier execs the cosign binary
  with `--certificate-identity-regexp` and `--certificate-oidc-issuer` anchored
  to the keese-ai GitHub Actions OIDC identity (verify.go:126-141). A 30-second
  per-image timeout is applied (verify.go:38).
- **Fail-closed vs. dry-run.** When `cosign-installplan-failclosed=true` (or
  Gates is nil), a verification failure denies the InstallPlan. When
  `cosign-installplan-failclosed=false`, verification failures are admitted with
  reason `BundleUnsignedAdmittedDryRun` and a `Warning` admission header so
  `kubectl apply` surfaces the problem without blocking (handler.go:144-167).
- **`failurePolicy: Fail`.** A webhook server outage blocks all InstallPlan
  admission for non-skipped namespaces. Namespaces with label
  `keese.ai/cosign-skip=true` are excluded by the `namespaceSelector`
  (validatingwebhookconfiguration.yaml:42-48).
- **HA deployment.** 2 replicas, `PodDisruptionBudget minAvailable: 1`,
  `terminationGracePeriodSeconds: 30`, liveness budget sized to outlast drain
  (deployment.yaml:7-9). TLS from cert-manager; cosign binary at `/cosign` in
  the image. SIGTERM drains via `signal.NotifyContext` and a 30-second hard
  drain bound (main.go:170-201).

## Configuration surface

Runtime configuration via env vars (env overrides flag):

| Variable | Default | Purpose |
|---|---|---|
| `WEBHOOK_PORT` | `9443` | TLS admission webhook port |
| `HEALTH_PORT` | `8081` | `/healthz` + `/readyz` |
| `METRICS_PORT` | `8082` | `/metrics` (controller-runtime) |
| `WEBHOOK_CERT_DIR` | `/etc/webhook/certs` | TLS cert directory |
| `COSIGN_BINARY` | `cosign` | Path to cosign executable |
| `COSIGN_IDENTITY_REGEX` | keese-ai workflow regexp | Override signer identity |
| `COSIGN_OIDC_ISSUER` | `token.actions.githubusercontent.com` | Override OIDC issuer |
| `COSIGN_REGISTRY_ALLOW` | `ghcr.io/keese-ai/` | Comma-separated registry prefixes |
| `KEESE_FEATURE_GATES_PATH` | `/etc/keese/features/gates.json` | FeatureGate ConfigMap projection |

FeatureGates (alpha, default-off — see docs/designs/27b-feature-gate-catalog.md):

- `cosign-installplan-verify` — enables signature checking; false → pass-through.
- `cosign-installplan-failclosed` — when true, failed verification denies;
  when false, admits with a Warning header.

Both gates are read from the `keese-features` ConfigMap (D27 projection),
mounted at `KEESE_FEATURE_GATES_PATH`. The ConfigMap is `optional: true` so the
webhook can start before the operator has reconciled seed CRs.

## Observability

**Structured log fields** on every admission exit path (handler.go:81-86):
`namespace`, `name`, `uid`, `operation`, `reason`, `gate`, `image`, `error`.
Shutdown emits `event=shutdown`, `reason=drain_complete` (main.go:199-201).

**Admission reasons** (finite const table, handler.go:36-46):

| Reason | Outcome | When |
|---|---|---|
| `AllowedFeatureGateOff` | Allow | `cosign-installplan-verify=false` |
| `AllowedBreakGlass` | Allow | Namespace label + InstallPlan annotation both set |
| `AllowedNoGatedImages` | Allow | No images matched the registry prefix list |
| `Allowed` | Allow | All gated images verified successfully |
| `BundleUnsignedAdmittedDryRun` | Allow + Warning | Verify failed, `failClosed=false` |
| `BundleUnsigned` | Deny | cosign verification failed, `failClosed=true` |
| `BundleNotDigestPinned` | Deny | Image reference lacks `@sha256:` |
| `InstallPlanCSVUnreadable` | Deny | CSV fetch or image extraction failed |
| `InstallPlanMalformed` | Deny | `spec.clusterServiceVersionNames` missing |

**Metrics:** standard controller-runtime metrics at `:8082/metrics` (request
latency, error counts). No custom metrics registered in this release.

## Known limitations

- **Both gates are alpha and default-off.** Verification does not run until a
  cluster operator explicitly sets `cosign-installplan-verify=true` via a
  FeatureGate override. The OLM bundle ships seed CRs for this purpose but they
  are not auto-applied in dev overlays.
- **Verification execs the cosign binary.** The cosign executable must be
  present in the container image at the path configured by `COSIGN_BINARY`.
  There is no pure-Go verification path; a missing or mis-pathed binary causes
  all gated-image admission to fail (deny or warn depending on `failClosed`).
- **Air-gapped Sigstore mirror support is an open follow-on.** The deployment
  uses the Sigstore public-good Rekor + Fulcio endpoints. A kustomize patch for
  air-gapped overlays pointing at an internal mirror has not been authored (noted
  in tech-debt.md TD-P1-04 follow-ons).
- **Cosign-tamper kuttl test is an open follow-on.** No e2e test under
  `test/e2e/bundle-sign/` exercises the tamper-detection path against a live
  cluster (noted in tech-debt.md TD-P1-04 follow-ons).

## Change history

- TD-P1-04 (closed 2026-05-06): initial implementation — webhook server,
  handler, verifier, CSV extractor, config manifests, CI image build + sign.
  Retrofitted with D27 feature-gate plumbing (same day); both gates ship
  alpha-default-off. See docs/plans/demo/tech-debt.md.

## References

- Design: docs/designs/14a-olm-channels-upgrades.md
- Design: docs/designs/27-feature-gates-openfeature.md
- Plan: docs/plans/demo/tech-debt.md (TD-P1-04)
- Source: cmd/keese-cosign-webhook/main.go
- Source: internal/admission/cosign/handler.go
- Source: internal/admission/cosign/verify.go
- Source: internal/admission/cosign/csv.go
- Source: config/cosign-webhook/
