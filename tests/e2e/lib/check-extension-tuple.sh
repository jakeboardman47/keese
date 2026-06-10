#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Live OpenFGA tuple assertion for the EH11 RuntimeExtension suite.
# Reads the owner tuple the RuntimeExtension controller writes on reconcile
# and asserts its PRESENCE (mode=present) or ABSENCE (mode=absent, the
# finalizer-cleanup case).
#
#   extension:<name>#owner@tenant:<tenant>
#
# (see internal/controller/keese/runtime_rebac_openfga.go WriteExtensionOwner
#  → Client.Write(extObj(name), "owner", "tenant:"+tenant), and the
#  ExtensionTupleWritten / ExtensionTupleDeleted events in runtime_events.go).
# This is the live-store complement to the CR-status assertion in the kuttl
# step: the controller only sets Ready=True (reason ExtensionTupleWritten)
# AFTER Rebac.WriteExtensionOwner succeeds, so a Ready RuntimeExtension
# already proves the tuple was attempted; this helper additionally proves it
# landed in the REAL store (not the RuntimeNoopRebacWriter fallback that
# main.go installs when OPENFGA_API_URL is unset) and — on delete — that the
# finalizer's DeleteAllExtensionTuples actually removed it.
#
# It reuses the seeded store_id from keese-system/openfga-config (the same
# mirror check-prereqs.sh gates on) and queries the in-cluster OpenFGA HTTP
# API with a one-shot `fga` CLI pod (ghcr.io/openfga/cli, the image the
# bootstrap seed Job already uses). No `fga` binary is required on the
# runner. Modeled on tests/e2e/lib/check-cta-tuple.sh (EH6).
#
# Exit-code convention (fail-closed, mirrors check-cta-tuple.sh):
#   0  no kubectl context         → skip (suite surfaces its own failure)
#   0  store_id unseeded          → skip (placeholder infra / Noop writer; the
#                                   CR-status layer is still asserted natively
#                                   by the kuttl step)
#   0  assertion holds            → OK
#   1  assertion violated         → fail the gate
#
# Usage:
#   EXTENSION=runtimeextension-e2e TENANT=ext-tenant MODE=present bash check-extension-tuple.sh
#   EXTENSION=runtimeextension-e2e TENANT=ext-tenant MODE=absent  bash check-extension-tuple.sh
#
# Overrides: KUBECTL_CONTEXT, FGA_NS, FGA_API_URL, FGA_CLI_IMAGE,
#            TUPLE_TIMEOUT_S (poll budget for the present/absent flip).

set -euo pipefail
IFS=$'\n\t'

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
FGA_NS="${FGA_NS:-openfga}"
FGA_API_URL="${FGA_API_URL:-http://openfga.openfga.svc.cluster.local:8080}"
FGA_CLI_IMAGE="${FGA_CLI_IMAGE:-ghcr.io/openfga/cli:v0.6.2}"

EXTENSION="${EXTENSION:-runtimeextension-e2e}"
TENANT="${TENANT:-default}"
MODE="${MODE:-present}"

# Poll budget: tuple write (present) is fast once Ready; finalizer tuple
# deletion (absent) waits on the RuntimeExtension delete +
# DeleteAllExtensionTuples round-trip. No sleep-as-assertion: the loop
# re-reads the live store each iteration.
TUPLE_TIMEOUT_S="${TUPLE_TIMEOUT_S:-60}"

if [[ -z "${CONTEXT}" ]]; then
  echo "[ext-tuple] no kubectl context — skipping (suite will surface failure later)" >&2
  exit 0
fi

STORE_ID="$(kubectl --context="${CONTEXT}" -n keese-system \
  get cm openfga-config -o jsonpath='{.data.store_id}' 2>/dev/null || true)"
if [[ -z "${STORE_ID}" ]]; then
  echo "[ext-tuple] SKIP: openfga store_id unseeded — extension owner-tuple read not" >&2
  echo "  possible against placeholder infra (controller falls back to the Noop writer)." >&2
  echo "  CR-status (Ready/observedGeneration/finalizer) layer is still asserted natively" >&2
  echo "  by the kuttl step. Fix: apply dev/bootstrap/openfga/seed.yaml" >&2
  exit 0
fi

OBJECT="extension:${EXTENSION}"
RELATION="owner"
USER="tenant:${TENANT}"

# read_tuple runs a one-shot fga CLI pod that lists tuples matching
# (object, relation) and greps for the tenant user. Echoes "1" when the
# tuple is present, "0" when absent. Never echoes the raw store contents
# (kept tight; only the boolean is surfaced).
read_tuple() {
  local pod="ext-tuple-read-$$-${RANDOM}"
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
      echo "[ext-tuple] OK: ${OBJECT}#${RELATION}@${USER} ABSENT (finalizer removed it)"
    else
      echo "[ext-tuple] OK: ${OBJECT}#${RELATION}@${USER} PRESENT in live store ${STORE_ID:0:8}…"
    fi
    exit 0
  fi
done

echo "[ext-tuple] FAIL: ${OBJECT}#${RELATION}@${USER} expected ${MODE} within ${TUPLE_TIMEOUT_S}s (last present=${got})" >&2
exit 1
