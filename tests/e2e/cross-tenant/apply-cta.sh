#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Apply the EH6 CrossTenantAgreement with a dynamically-computed expiresAt.
#
# WHY a script instead of a static manifest: the SHIPPED tuple-REMOVAL path
# in crosstenanagreement_controller.go is EXPIRY (transitionToExpired →
# Rebac.Delete), NOT plain deletion — cleanup() on delete only removes the
# NATS stream + finalizer, it does not touch the trust tuple. expiresAt is
# immutable after create (VAP) and must be in the future, so we compute
# `now + EXPIRY_WINDOW_S` at apply time. The suite then asserts the tuple
# present (02), waits out the window, and asserts the controller drove
# Approved → Expired and DELETED the tuple (04) — the genuine
# revocation/deny-flip the plan calls for, against shipped behavior.
#
# beta2 (from) requests scoped cross-tenant messaging into alpha2 (to). On
# both-tenant approval the controller writes
#   tenant:alpha2#allows_messaging@tenant:beta2
# and sets Approved/Ready; on expiry it removes that tuple and sets Expired.
#
# Usage:
#   EXPIRY_WINDOW_S=150 bash apply-cta.sh
#
# Override KUBECTL_CONTEXT to target a specific cluster.

set -euo pipefail
IFS=$'\n\t'

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"

# Expiry window: long enough for both approvals + tuple sync + the tuple
# PRESENT assertion (02) to settle before expiry fires, short enough to
# keep the suite under the kuttl per-test budget. Recorded in the README.
EXPIRY_WINDOW_S="${EXPIRY_WINDOW_S:-150}"

# RFC3339 (UTC, second precision, trailing Z) — matches the CRD pattern
# ^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$ . GNU and BSD date differ on
# relative-time flags, so try both.
expires_at=""
if expires_at="$(date -u -d "+${EXPIRY_WINDOW_S} seconds" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null)"; then
  :
elif expires_at="$(date -u -v "+${EXPIRY_WINDOW_S}S" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null)"; then
  :
else
  echo "[apply-cta] FAIL: could not compute expiresAt (neither GNU nor BSD date worked)" >&2
  exit 1
fi

ctx_args=()
[[ -n "${CONTEXT}" ]] && ctx_args=(--context="${CONTEXT}")

echo "[apply-cta] applying beta2-to-alpha2 CTA with expiresAt=${expires_at} (window ${EXPIRY_WINDOW_S}s)"

kubectl "${ctx_args[@]}" apply -f - <<EOF
apiVersion: authz.keese.ai/v1alpha1
kind: CrossTenantAgreement
metadata:
  name: beta2-to-alpha2
spec:
  from:
    tenantRef:
      name: beta2
  to:
    tenantRef:
      name: alpha2
  expiresAt: "${expires_at}"
  scope:
    a2aRoles:
      - bidirectional
    natsSubjects:
      - keese.cta.beta2.alpha2.>
EOF

echo "[apply-cta] applied"
