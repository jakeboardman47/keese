#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Live OpenFGA tuple assertion for the EH6 CrossTenantAgreement suite.
# Reads the cross-tenant trust tuple the CrossTenantAgreement controller
# writes on approval and asserts its PRESENCE (mode=present) or ABSENCE
# (mode=absent, the revocation/finalizer case).
#
#   tenant:<to>#allows_messaging@tenant:<from>
#
# (see internal/controller/authz/crosstenanagreement_rebac.go
#  craAllowsMessagingTuple). This is the live-store complement to the
# CR-status assertion in 0N-assert.yaml: the controller only sets
# Approved/Ready AFTER Rebac.Sync succeeds, so a Ready CTA already proves
# the tuple was written; this helper additionally proves it landed in the
# REAL store (not the NoopRebacWriter fallback) and — on revoke — that the
# finalizer/expiry path actually removed it.
#
# It reuses the seeded store_id from keese-system/openfga-config (the same
# mirror check-prereqs.sh gates on) and queries the in-cluster OpenFGA HTTP
# API with a one-shot `fga` CLI pod (ghcr.io/openfga/cli, the image the
# bootstrap seed Job already uses). No `fga` binary is required on the
# runner.
#
# Exit-code convention (fail-closed, mirrors check-prereqs.sh):
#   0  no kubectl context        → skip (suite surfaces its own failure)
#   0  store_id unseeded          → skip (placeholder infra; CR-status layer
#                                   still asserted natively by kuttl)
#   0  assertion holds            → OK
#   1  assertion violated         → fail the gate
#
# Usage:
#   FROM_TENANT=beta2 TO_TENANT=alpha2 MODE=present  bash check-cta-tuple.sh
#   FROM_TENANT=beta2 TO_TENANT=alpha2 MODE=absent   bash check-cta-tuple.sh
#
# Overrides: KUBECTL_CONTEXT, FGA_NS, FGA_API_URL, FGA_CLI_IMAGE,
#            TUPLE_TIMEOUT_S (poll budget for the present/absent flip).

set -euo pipefail
IFS=$'\n\t'

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
FGA_NS="${FGA_NS:-openfga}"
FGA_API_URL="${FGA_API_URL:-http://openfga.openfga.svc.cluster.local:8080}"
FGA_CLI_IMAGE="${FGA_CLI_IMAGE:-ghcr.io/openfga/cli:v0.6.2}"

FROM_TENANT="${FROM_TENANT:-beta2}"
TO_TENANT="${TO_TENANT:-alpha2}"
MODE="${MODE:-present}"

# Poll budget: tuple sync (present) is fast once Ready; finalizer tuple
# deletion (absent) waits on the CTA delete + Delete() round-trip. No
# sleep-as-assertion: the loop re-reads the live store each iteration.
TUPLE_TIMEOUT_S="${TUPLE_TIMEOUT_S:-60}"

if [[ -z "${CONTEXT}" ]]; then
  echo "[cta-tuple] no kubectl context — skipping (suite will surface failure later)" >&2
  exit 0
fi

STORE_ID="$(kubectl --context="${CONTEXT}" -n keese-system \
  get cm openfga-config -o jsonpath='{.data.store_id}' 2>/dev/null || true)"
if [[ -z "${STORE_ID}" ]]; then
  echo "[cta-tuple] SKIP: openfga store_id unseeded — cross-tenant tuple read not" >&2
  echo "  possible against placeholder infra. CR-status (Ready/finalizer) layer is" >&2
  echo "  still asserted natively by the kuttl step. Fix: apply dev/bootstrap/openfga/seed.yaml" >&2
  exit 0
fi

OBJECT="tenant:${TO_TENANT}"
RELATION="allows_messaging"
USER="tenant:${FROM_TENANT}"

# read_tuple runs a one-shot fga CLI pod that lists tuples matching
# (object, relation) and greps for the from-tenant user. Echoes "1" when
# the tuple is present, "0" when absent. Never echoes the raw store
# contents (kept tight; only the boolean is surfaced).
read_tuple() {
  local pod="cta-tuple-read-$$-${RANDOM}"
  local out
  out="$(kubectl --context="${CONTEXT}" -n "${FGA_NS}" run "${pod}" \
    --image="${FGA_CLI_IMAGE}" --restart=Never --rm -i --quiet \
    --command -- \
    fga tuple read --api-url "${FGA_API_URL}" --store-id "${STORE_ID}" \
    --object "${OBJECT}" --relation "${RELATION}" 2>/dev/null || true)"
  if grep -qF "${USER}" <<<"${out}"; then
    printf '1'
  else
    printf '0'
  fi
}

# Poll until the live store matches the desired present/absent state.
deadline=$(($(date +%s) + TUPLE_TIMEOUT_S))
want="1"
[[ "${MODE}" == "absent" ]] && want="0"

got=""
while [[ $(date +%s) -lt ${deadline} ]]; do
  got="$(read_tuple)"
  if [[ "${got}" == "${want}" ]]; then
    if [[ "${MODE}" == "absent" ]]; then
      echo "[cta-tuple] OK: ${OBJECT}#${RELATION}@${USER} ABSENT (finalizer removed it)"
    else
      echo "[cta-tuple] OK: ${OBJECT}#${RELATION}@${USER} PRESENT in live store ${STORE_ID:0:8}…"
    fi
    exit 0
  fi
done

echo "[cta-tuple] FAIL: ${OBJECT}#${RELATION}@${USER} expected ${MODE} within ${TUPLE_TIMEOUT_S}s (last present=${got})" >&2
exit 1
