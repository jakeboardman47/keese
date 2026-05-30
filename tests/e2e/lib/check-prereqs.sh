#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Sourced (or executed) at the top of every kuttl `command:` step that
# requires real OpenFGA + OpenBao infrastructure. Fails non-zero with a
# clear message when either prerequisite is missing — so e2e suites
# don't silently pass against the NoopRebacWriter / placeholder secrets
# fallback paths.
#
# Required:
#   - configmap openfga-config in keese-system has non-empty store_id
#   - bao kv get -mount=keese tenants/tenant-a/anthropic returns api_key=<non-empty>
#
# Usage:
#   bash tests/e2e/lib/check-prereqs.sh
#   source tests/e2e/lib/check-prereqs.sh   (also works; same effect)
#
# Override the cluster context with KUBECTL_CONTEXT.

set -euo pipefail
IFS=$'\n\t'

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
if [[ -z "${CONTEXT}" ]]; then
  echo "[prereqs] no kubectl context — skipping check (test will likely fail later)" >&2
  exit 0
fi

# 1. OpenFGA store + model IDs in keese-system mirror CM.
STORE_ID="$(kubectl --context="${CONTEXT}" -n keese-system \
  get cm openfga-config -o jsonpath='{.data.store_id}' 2>/dev/null || true)"
MODEL_ID="$(kubectl --context="${CONTEXT}" -n keese-system \
  get cm openfga-config -o jsonpath='{.data.authorization_model_id}' 2>/dev/null || true)"

if [[ -z "${STORE_ID}" || -z "${MODEL_ID}" ]]; then
  cat <<EOF >&2
[prereqs] FAIL: OpenFGA mirror configmap not seeded.

  Expected non-empty store_id + authorization_model_id in
  keese-system/openfga-config.

  Fix:
    kubectl apply -f dev/bootstrap/openfga/seed.yaml
    kubectl wait --for=condition=Complete job/openfga-seed -n openfga --timeout=120s
EOF
  exit 1
fi

# 2. OpenBao Anthropic API key seeded (not the empty placeholder).
#    We exec into the bao pod since the bao CLI isn't in this script's PATH
#    on every CI runner.
BAO_POD="$(kubectl --context="${CONTEXT}" -n openbao \
  get pod -l app.kubernetes.io/name=openbao -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
if [[ -z "${BAO_POD}" ]]; then
  echo "[prereqs] FAIL: no OpenBao pod found in namespace openbao" >&2
  echo "  Fix: helmfile sync dev/bootstrap" >&2
  exit 1
fi

# bao kv get returns "api_key" line; the empty placeholder writes "api_key=".
ANTHROPIC_KV="$(kubectl --context="${CONTEXT}" -n openbao \
  exec "${BAO_POD}" -- env BAO_TOKEN=root \
  bao kv get -mount=keese -format=json tenants/tenant-a/anthropic 2>/dev/null \
  | grep -o '"api_key":"[^"]*"' | head -n1 || true)"

if [[ -z "${ANTHROPIC_KV}" || "${ANTHROPIC_KV}" == '"api_key":""' ]]; then
  cat <<EOF >&2
[prereqs] FAIL: OpenBao Anthropic key is the empty placeholder.

  Fix:
    export ANTHROPIC_API_KEY=sk-ant-...   # in .env.local
    scripts/dev/seed-openbao.sh
EOF
  exit 1
fi

echo "[prereqs] OK: OpenFGA seeded (store_id=${STORE_ID:0:8}…); Anthropic key present"
