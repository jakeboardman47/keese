#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Additive companion to check-prereqs.sh, used by the EH4 ReBAC decision
# suite (and any future authz suite). Fails non-zero with a clear message
# when the keese-authz ext_authz service isn't deployed + Ready — so the
# suite never silently passes when the decision path that ext_authz makes
# (internal/authz/extauth + internal/rebac) isn't actually running.
#
# This does NOT replace or modify check-prereqs.sh; run BOTH. check-prereqs
# gates the OpenFGA store + OpenBao key; this gates the ext_authz Deployment
# that consumes them.
#
# Required:
#   - Deployment keese-authz in keese-system has >=1 Available replica.
#
# Usage:
#   bash tests/e2e/lib/check-extauth.sh
#
# Exit-code convention (mirrors check-prereqs.sh, fail-closed):
#   0  no kubectl context  → skip (test will surface its own failure later)
#   0  ext_authz Available → OK
#   1  ext_authz missing / not Available → fail the gate
#
# Override the cluster context with KUBECTL_CONTEXT, the namespace with
# AUTHZ_NS, and the deployment name with AUTHZ_DEPLOY.

set -euo pipefail
IFS=$'\n\t'

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
AUTHZ_NS="${AUTHZ_NS:-keese-system}"
AUTHZ_DEPLOY="${AUTHZ_DEPLOY:-keese-authz}"

if [[ -z "${CONTEXT}" ]]; then
  echo "[extauth] no kubectl context — skipping check (test will likely fail later)" >&2
  exit 0
fi

AVAIL="$(kubectl --context="${CONTEXT}" -n "${AUTHZ_NS}" \
  get deploy "${AUTHZ_DEPLOY}" \
  -o jsonpath='{.status.availableReplicas}' 2>/dev/null || true)"

if [[ -z "${AVAIL}" || "${AVAIL}" -lt 1 ]]; then
  cat <<EOF >&2
[extauth] FAIL: keese-authz ext_authz service not Available.

  Expected Deployment ${AUTHZ_NS}/${AUTHZ_DEPLOY} with >=1 available replica.
  This is the service that makes the live ReBAC allow/deny decision; without
  it the EH4 suite would assert nothing real.

  Fix:
    kubectl apply -f dev/bootstrap/aigateway/keese-authz.yaml
    kubectl rollout status deploy/${AUTHZ_DEPLOY} -n ${AUTHZ_NS} --timeout=120s
EOF
  exit 1
fi

echo "[extauth] OK: ${AUTHZ_NS}/${AUTHZ_DEPLOY} Available (${AVAIL} replica(s))"
