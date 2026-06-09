#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Drive one tenant's approval of a CrossTenantAgreement by applying the
# signed approval annotations the controller's validateApprovalAnnotation
# reads (crosstenanagreement_controller.go §198). The controller verifies
# the signature (FakeSATokenHmacVerifier accepts any non-empty signature in
# the default dev/test wiring), appends the CRAApproval to status, and
# strips the annotations on the next reconcile.
#
# It processes ONE approval per reconcile, so this script is called once
# per tenant (from-tenant, then to-tenant); when both have approved the
# controller transitions Pending → Approved and writes the trust tuple.
#
# Annotations set (keys per crosstenanagreement_controller.go):
#   keese.ai/cra-approve            = "true"
#   keese.ai/cra-approving-tenant   = <tenant>
#   keese.ai/cra-approver           = <subject>
#   keese.ai/cra-signature          = <opaque non-empty token>
#   keese.ai/cra-signature-type     = sa-token  (FakeSATokenHmacVerifier)
#
# The signature is an opaque test placeholder — NOT a real credential — and
# is never logged (rule 02 / 05.10). It exists only to exercise the verify
# branch with the dev fake verifier.
#
# Usage:
#   CRA=beta2-to-alpha2 TENANT=beta2 APPROVER=user:bob@cross-tenant.example \
#     bash approve-cta.sh
#
# Override KUBECTL_CONTEXT to target a specific cluster.

set -euo pipefail
IFS=$'\n\t'

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
CRA="${CRA:?CRA (CrossTenantAgreement name) is required}"
TENANT="${TENANT:?TENANT (approving tenant) is required}"
APPROVER="${APPROVER:?APPROVER (subject) is required}"

# Opaque, non-secret placeholder accepted by FakeSATokenHmacVerifier.
SIGNATURE="${SIGNATURE:-cta-test-approval-${TENANT}}"

ctx_args=()
[[ -n "${CONTEXT}" ]] && ctx_args=(--context="${CONTEXT}")

kubectl "${ctx_args[@]}" annotate crosstenantagreement "${CRA}" --overwrite \
  "keese.ai/cra-approve=true" \
  "keese.ai/cra-approving-tenant=${TENANT}" \
  "keese.ai/cra-approver=${APPROVER}" \
  "keese.ai/cra-signature=${SIGNATURE}" \
  "keese.ai/cra-signature-type=sa-token" >/dev/null

echo "[approve-cta] applied ${TENANT} approval annotations to crosstenantagreement/${CRA}"

# Wait for the controller to consume the annotation (it strips
# keese.ai/cra-approve once the approval is recorded). Poll the live object
# rather than sleeping (rule 06 — Eventually, never sleep).
deadline=$(($(date +%s) + 60))
while [[ $(date +%s) -lt ${deadline} ]]; do
  val="$(kubectl "${ctx_args[@]}" get crosstenantagreement "${CRA}" \
    -o jsonpath='{.metadata.annotations.keese\.ai/cra-approve}' 2>/dev/null || true)"
  if [[ -z "${val}" ]]; then
    echo "[approve-cta] ${TENANT} approval consumed by controller"
    exit 0
  fi
done

echo "[approve-cta] FAIL: ${TENANT} approval annotation not consumed within 60s" >&2
exit 1
