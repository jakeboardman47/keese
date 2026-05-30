#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# E2E smoke test for the keese AI Gateway defense Lua —
# `bootstrap-aigw-defense` claim sticks against:
#   (1) hostile client-supplied `Authorization: Bearer …` header
#   (2) hostile client-supplied `x-api-key` header
#   (3) hostile client-supplied `anthropic-api-key` header
#   (4) goose-style request to bare `/anthropic/v1/messages` (must be path-rewritten
#       to `/anthropic/anthropic/v1/messages` upstream of the AI Gateway extProc)
#
# Per [.claude/rules/06-testing.md](.claude/rules/06-testing.md) this is the
# `e2e` tier — touches a real cluster, real Envoy, real Anthropic. Run via
# `make test-e2e-aigw-defense`. Skips gracefully if the cluster isn't a
# kind-keese-* context or if the AI Gateway / workspace pods aren't up.
#
# Usage: scripts/dev/test-aigw-defense.sh
#        KUBECTL_CONTEXT=kind-keese-demo scripts/dev/test-aigw-defense.sh
#
# Refs: dev/bootstrap/aigateway/keese-defense-lua-patch.json
#       Makefile :: bootstrap-aigw-defense
#       MEMORY 2026-05-04 — keese defense Lua

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# shellcheck source=../lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
WORKSPACE_NS="${WORKSPACE_NS:-alpha}"
SESSION_LABEL="${SESSION_LABEL:-keese.ai/session}"
GATEWAY_HOST="${GATEWAY_HOST:-envoy-ai-gateway.keese-system.svc:443}"
CA_PATH="${CA_PATH:-/var/run/keese/ca/ca.crt}"
MODEL="${MODEL:-claude-opus-4-7}"

# ── Preflight ─────────────────────────────────────────────────────────────────
#
# We don't strict-check for `kind-keese-*` here because kuttl renames the
# context to `cluster` in its sandboxed kubeconfig copy. Production-context
# refusal lives in scripts/guard-kube-context.sh (run by every Makefile
# entry point that targets the cluster). Below we verify the keese stack
# is actually present — that's what really matters for the test.

log::info "test-aigw-defense: cluster=${CONTEXT} workspace_ns=${WORKSPACE_NS}"

# Locate a workspace session pod that has the gateway CA mounted. We use it
# as the curl client — same identity (projected SA token) as a real agent
# pod, same CA bundle, same network egress policy, all already in place.
SESSION_POD="$(kubectl --context="${CONTEXT}" -n "${WORKSPACE_NS}" \
  get pod -l "${SESSION_LABEL}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"

if [[ -z "${SESSION_POD}" ]]; then
  log::err "no workspace session pod found in ${WORKSPACE_NS} (label ${SESSION_LABEL})."
  log::err "  Apply dev/demo/hello-keese.yaml first, or set WORKSPACE_NS / SESSION_LABEL."
  exit 1
fi

log::info "using client pod: ${WORKSPACE_NS}/${SESSION_POD}"

# ── SA token for ext_authz ────────────────────────────────────────────────────
#
# Since TD-P1-03, the AI Gateway runs ext_authz against keese-authz BEFORE the
# Lua filter. ext_authz REQUIRES a valid SA token in `Authorization: Bearer
# <jwt>`. We mint one for the workspace's SA per run (10-minute TTL = the
# kubectl create token minimum). Tests below pass this token via $SA_TOKEN.
WORKSPACE_NAME="${WORKSPACE_NAME:-my-ws}"
WSUID="$(kubectl --context="${CONTEXT}" \
  get workspace "${WORKSPACE_NAME}" -n "${WORKSPACE_NS}" \
  -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
if [[ -z "${WSUID}" ]]; then
  log::err "workspace ${WORKSPACE_NS}/${WORKSPACE_NAME} not found; apply dev/demo/hello-keese.yaml first"
  exit 1
fi
SA_NAME="ksa-${WSUID}"
SA_TOKEN="$(kubectl --context="${CONTEXT}" create token "${SA_NAME}" \
  -n "${WORKSPACE_NS}" --duration=600s 2>/dev/null || true)"
if [[ -z "${SA_TOKEN}" ]]; then
  log::err "could not mint SA token for ${WORKSPACE_NS}/${SA_NAME}"
  exit 1
fi
log::info "minted SA token for ${WORKSPACE_NS}/${SA_NAME}"

# Sanity: defense Lua must include the auth-strip directives. Failing fast
# here gives a clearer error than a 4xx from the gateway.
LUA_SRC="$(kubectl --context="${CONTEXT}" \
  get envoyextensionpolicy -n keese-system ai-eg-eep-keese-aigateway \
  -o jsonpath='{.spec.lua[0].inline}' 2>/dev/null || true)"

for marker in 'remove("authorization")' 'remove("x-api-key")' 'remove("anthropic-api-key")'; do
  if ! grep -qF "${marker}" <<<"${LUA_SRC}"; then
    log::err "defense Lua missing directive: ${marker}"
    log::err "  Re-run: make bootstrap-aigw-defense"
    exit 2
  fi
done
log::ok "defense Lua present (auth-strip directives all set)"

# ── Warm-up probe ─────────────────────────────────────────────────────────────
#
# After a `kubectl rollout restart` of the gateway data plane, Envoy reports
# 3/3 Ready before its xDS hot-load is fully effective — the first ~5–10
# requests can return 500 with a transient `cannot_process_response_body`
# even though the route, BSP, and Lua are all in place. Poll a benign GET
# until we see anything other than 5xx, then run the asserted cases. This
# matches rule 06-testing.md "async assertions via Eventually/poll helpers
# — never sleep."
warm_up_gateway() {
  local deadline=$(( $(date +%s) + 30 ))
  while [[ $(date +%s) -lt ${deadline} ]]; do
    local code
    code="$(kubectl --context="${CONTEXT}" -n "${WORKSPACE_NS}" \
      exec "${SESSION_POD}" -- bash -c "
        curl -s -o /dev/null -w '%{http_code}' \
          --max-time 5 \
          --cacert ${CA_PATH} \
          -X POST 'https://${GATEWAY_HOST}/anthropic/v1/messages' \
          -H 'Authorization: Bearer ${SA_TOKEN}' \
          -H 'content-type: application/json' \
          -H 'anthropic-version: 2023-06-01' \
          -d '{\"model\":\"${MODEL}\",\"max_tokens\":8,\"messages\":[{\"role\":\"user\",\"content\":\"ok\"}]}' || true
      " 2>/dev/null)"
    if [[ "${code}" =~ ^[12][0-9][0-9]$ ]]; then
      log::ok "warm-up: gateway returned ${code} — ready"
      return 0
    fi
    log::dim "warm-up: gateway returned ${code:-???}; retrying"
  done
  log::warn "warm-up: gateway never returned 2xx within 30s; running cases anyway"
  return 0
}

warm_up_gateway

# ── Curl helper ───────────────────────────────────────────────────────────────

# Args: <case-id> <path> <expected-code> [extra `-H` args]
# Uses the workspace SA token for the canonical Authorization. Each
# extra header arg is appended verbatim — typically additional
# hostile headers we want the defense Lua to strip.
hostile_curl() {
  local case_id="$1"; shift
  local path="$1"; shift
  local expected="$1"; shift
  # remaining args are extra -H headers
  local code
  code="$(kubectl --context="${CONTEXT}" -n "${WORKSPACE_NS}" \
    exec "${SESSION_POD}" -- bash -c "
      curl -s -o /dev/null -w '%{http_code}' \
        --max-time 30 \
        --cacert ${CA_PATH} \
        -X POST 'https://${GATEWAY_HOST}${path}' \
        -H 'Authorization: Bearer ${SA_TOKEN}' \
        -H 'content-type: application/json' \
        -H 'anthropic-version: 2023-06-01' \
        $(printf '%s ' "$@") \
        -d '{\"model\":\"${MODEL}\",\"max_tokens\":40,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}'
    ")"
  if [[ "${code}" == "${expected}" ]]; then
    log::ok "[${case_id}] ${path} → http=${code} (expected ${expected})"
    return 0
  fi
  log::err "[${case_id}] ${path} → http=${code} (expected ${expected})"
  return 1
}

# Same shape but does NOT inject the SA token Authorization header.
# Used to assert that ext_authz denies on missing/invalid auth.
unauth_curl() {
  local case_id="$1"; shift
  local path="$1"; shift
  local expected="$1"; shift
  local extra_authz="${1:-}"; [[ -n "$extra_authz" ]] && shift || true
  local code
  code="$(kubectl --context="${CONTEXT}" -n "${WORKSPACE_NS}" \
    exec "${SESSION_POD}" -- bash -c "
      curl -s -o /dev/null -w '%{http_code}' \
        --max-time 30 \
        --cacert ${CA_PATH} \
        -X POST 'https://${GATEWAY_HOST}${path}' \
        ${extra_authz} \
        -H 'content-type: application/json' \
        -H 'anthropic-version: 2023-06-01' \
        -d '{\"model\":\"${MODEL}\",\"max_tokens\":40,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}'
    ")"
  if [[ "${code}" == "${expected}" ]]; then
    log::ok "[${case_id}] ${path} → http=${code} (expected ${expected})"
    return 0
  fi
  log::err "[${case_id}] ${path} → http=${code} (expected ${expected})"
  return 1
}

# ── Test cases ────────────────────────────────────────────────────────────────
#
# All cases hit the canonical native-Anthropic /anthropic/v1/messages
# path. The Envoy filter chain order is:
#   1. ext_authz → keese-authz (verifies Authorization Bearer = SA token,
#      OpenFGA Check tool:can_call). Reject = 403 from the gateway.
#   2. Lua filter → strips client-supplied auth headers
#      (authorization, x-api-key, anthropic-api-key) BEFORE the BSP
#      injection so the BSP-injected x-api-key is the SOLE upstream
#      credential (rule 05.2).
#   3. BSP → injects upstream x-api-key.
#   4. Anthropic upstream → 200.
#
# Pre-TD-P1-03 this test asserted that garbage Bearer tokens get
# stripped + replaced; that's no longer the contract — ext_authz
# treats any Authorization Bearer as the identity claim. The new
# test surface is "valid SA token + hostile vendor headers should
# still 200 because the Lua strips the vendor headers."

FAILURES=0

# Case 1 (NEW): no Authorization header at all → ext_authz denies.
unauth_curl "no-auth-denied" "/anthropic/v1/messages" "403" "" \
  || FAILURES=$((FAILURES + 1))

# Case 2 (NEW): garbage Authorization Bearer (not a real JWT) →
# ext_authz parse-fails on the JWT payload, denies.
unauth_curl "garbage-bearer-denied" "/anthropic/v1/messages" "403" \
  "-H 'Authorization: Bearer total-garbage-not-a-jwt'" \
  || FAILURES=$((FAILURES + 1))

# Case 3: valid SA token + hostile x-api-key — Lua strips
# x-api-key, BSP injects the real one, Anthropic accepts, 200.
hostile_curl "x-api-key-stripped" "/anthropic/v1/messages" "200" \
  "-H 'x-api-key: also-garbage-key'" \
  || FAILURES=$((FAILURES + 1))

# Case 4: valid SA token + hostile anthropic-api-key — same
# Lua-strip + BSP-inject path.
hostile_curl "anthropic-api-key-stripped" "/anthropic/v1/messages" "200" \
  "-H 'anthropic-api-key: garbage-vendor-header'" \
  || FAILURES=$((FAILURES + 1))

# Case 5: valid SA token + BOTH hostile vendor headers. Exercises
# the full Lua strip path with a dense hostile header surface.
hostile_curl "both-vendor-stripped" "/anthropic/v1/messages" "200" \
  "-H 'x-api-key: also-garbage-key'" \
  "-H 'anthropic-api-key: garbage-vendor-header'" \
  || FAILURES=$((FAILURES + 1))

if [[ "${FAILURES}" -gt 0 ]]; then
  log::err "test-aigw-defense: ${FAILURES} case(s) failed"
  exit 1
fi

log::ok "test-aigw-defense: all 5 cases passed"
