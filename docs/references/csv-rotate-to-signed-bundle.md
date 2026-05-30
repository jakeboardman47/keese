<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: runbook
depends:
  - ../designs/14a-olm-channels-upgrades.md
  - ../plans/demo/D4-cloud-deploy.md
related_skills: [validate-bundle, olm-bundle-authoring]
status: current
last_verified: 2026-05-06
---

# Rotate a running CSV to a CI-signed bundle

Use this runbook when D4-T2 (or any prior install) shipped a
locally-built unsigned image and the cluster is now running an
unsigned CSV. After TD-P1-04 the cosign-webhook will refuse new
unsigned `InstallPlan`s — but a CSV already-installed before the
webhook landed sits there until you rotate it.

## Pre-flight

1. Confirm the cosign-webhook is healthy:
   ```sh
   kubectl -n keese-system get deploy keese-cosign-webhook \
     -o jsonpath='{.status.readyReplicas}/{.spec.replicas}'
   ```
   Both replicas must be Ready.

2. Confirm a CI-built signed image exists. The
   `.github/workflows/image.yaml` and `bundle.yaml` runs on tag push
   produce digest-pinned, cosign-signed images. From a local shell:
   ```sh
   crane manifest ghcr.io/keese-ai/keese-bundle:vX.Y.Z >/dev/null
   bash scripts/bundle-sign-verify.sh \
     ghcr.io/keese-ai/keese-bundle@$(crane digest ghcr.io/keese-ai/keese-bundle:vX.Y.Z)
   ```
   Both must exit 0.

## Rotate

1. **Pin Subscription to the new signed CSV.** Capture the digest:
   ```sh
   DIGEST=$(crane digest ghcr.io/keese-ai/keese-bundle:vX.Y.Z)
   ```

2. **Approve the new InstallPlan.** With
   `installPlanApproval: Manual` (rule 04 §5 + design 14a §1) and the
   `keese.ai/cosign-skip` namespace label not set, OLM creates a
   pending InstallPlan that the cosign-webhook validates on UPDATE
   when `spec.approved` flips.
   ```sh
   kubectl -n operators get installplan -o name | while read ip; do
     kubectl -n operators patch "$ip" --type=merge \
       -p '{"spec":{"approved":true}}'
   done
   ```
   The webhook denies with `reason: BundleUnsigned` when an unsigned
   image slips through; check operator logs:
   ```sh
   kubectl -n keese-system logs deploy/keese-cosign-webhook --tail=50
   ```

3. **Watch CSV reach Succeeded.**
   ```sh
   kubectl -n operators get csv -w
   ```

4. **Delete the old unsigned CSV** if OLM didn't replace it through
   the `replaces:` chain. This is rare but happens when the old CSV
   was applied imperatively without OLM tracking. Check first:
   ```sh
   kubectl -n operators get csv -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.replaces}{"\n"}{end}'
   ```
   If the old CSV is not listed as `replaces:` anywhere, delete it
   manually.

## Verify

- Operator pod images now match the signed digest:
  ```sh
  kubectl -n keese-system get pod -l app.kubernetes.io/name=keese-controller-manager \
    -o jsonpath='{.items[0].spec.containers[0].image}'
  ```
  must end in `@sha256:…` and resolve to the digest from step 1.

- Cosign re-verify against the running image:
  ```sh
  bash scripts/bundle-sign-verify.sh \
    "$(kubectl -n keese-system get pod -l app.kubernetes.io/name=keese-controller-manager \
      -o jsonpath='{.items[0].spec.containers[0].image}')"
  ```
  Exits 0.

## Break-glass (rare)

When the cosign-webhook is down or its certificate has expired and an
emergency install is needed, follow the rule 05.13 break-glass
protocol — never bypass without it:

```sh
kubectl label namespace operators keese.ai/break-glass=true
kubectl -n operators annotate installplan/<NAME> \
  keese.ai/unsafe-allow-unsigned=true
```

The webhook honors the override only when both are present. Remove
both immediately after the emergency install completes; the operator
events recorder logs `UnsafeAnnotationAllowed` so audits flag any
unrotated namespace.

## Failure modes

| Symptom | Likely cause | Fix |
|---|---|---|
| `InstallPlan denied: BundleNotDigestPinned` | CSV references a tag, not a digest | Re-run release pipeline on the signed tag; pin via `replaces:` |
| `InstallPlan denied: BundleUnsigned` | Image was pushed bypassing CI | Push via `image.yaml` workflow on a tag; never `make docker-push` to prod |
| Webhook timeout (503) on apply | cosign-webhook pods both unhealthy | Check liveness/readiness; if the certificate is invalid, re-issue via cert-manager |
| `cosign verify` reports "no matching signatures" | OIDC issuer or identity drifted | Confirm `KEESE_COSIGN_OIDC_ISSUER` + `KEESE_COSIGN_IDENTITY_REGEX` match the workflow that signed the image |

## See also

- [docs/designs/14a-olm-channels-upgrades.md](../designs/14a-olm-channels-upgrades.md) — channel + upgrade graph
- [docs/plans/demo/D4-cloud-deploy.md](../plans/demo/D4-cloud-deploy.md) §T2
- [.claude/rules/05-security-zero-trust.md](../../.claude/rules/05-security-zero-trust.md) §12 + §13
