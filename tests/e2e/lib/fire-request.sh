#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Sourceable request-firing primitives for the authz e2e suites.
#
# These functions are the same request-firing pattern EH4 introduced in
# tests/e2e/rebac-decision/test-rebac-decision.sh — minting a projected
# SA token, firing a real curl through the Envoy AI Gateway from inside a
# workspace session pod (mounted CA + Bearer token, identical to a real
# agent pod), and polling for an allow->deny status flip without ever
# sleeping-as-assertion (rule 06: every loop body re-fires a real request).
#
# EH4's test-rebac-decision.sh is a top-level script (it runs its four
# cases on `source`, so it cannot be sourced for functions only) and lives
# under a protected reuse contract — EH5 must NOT copy or edit it. To share
# the firing primitives without duplicating logic, the primitives live here
# in tests/e2e/lib/ (additive, on the EH5 `outputs:` allowlist) and any
# authz suite sources THIS file. EH4 keeps its inline copy untouched; new
# suites (EH5 onward) source this.
#
# Usage (source, then call):
#   source tests/e2e/lib/fire-request.sh
#   token="$(fr::mint_sa_token my-ws alpha)"
#   fr::assert_status case-id alpha "$token" 200 /search
#   fr::poll_status   case-id alpha "$token" 403 90 /search
#
# All functions echo ONLY HTTP status codes — never response bodies, never
# tokens (rule 02 + rule 05.10). The caller passes the workspace namespace
# and the request path so the same primitives drive both the cluster
# ToolBinding path (EH4: /anthropic/v1/messages) and the namespaced
# WorkspaceTool path (EH5: /search).
#
# Env overrides (all optional; sensible defaults):
#   KUBECTL_CONTEXT   kube context (default: current-context)
#   GATEWAY_HOST      gateway host:port (default: envoy-ai-gateway.keese-system.svc:443)
#   CA_PATH           in-pod CA bundle path (default: /var/run/keese/ca/ca.crt)
#   SESSION_LABEL     session-pod selector (default: keese.ai/session)
#   FR_HTTP_TIMEOUT   per-request curl --max-time seconds (default: 30)
#
# Refs: tests/e2e/rebac-decision/test-rebac-decision.sh  (origin of the pattern)
#       internal/authz/extauth/resolver.go               (path -> tool: resolution)
#       internal/controller/keese/workspace_controller.go (allowed_in tuple write)

# Guard against double-sourcing (idempotent — rule 01 bash conventions).
if [[ -n "${FR_SOURCED:-}" ]]; then
  # shellcheck disable=SC2317  # reached only when this file is re-sourced.
  return 0 2>/dev/null || true
fi
FR_SOURCED=1

FR_CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
FR_GATEWAY_HOST="${GATEWAY_HOST:-envoy-ai-gateway.keese-system.svc:443}"
FR_CA_PATH="${CA_PATH:-/var/run/keese/ca/ca.crt}"
FR_SESSION_LABEL="${SESSION_LABEL:-keese.ai/session}"
FR_HTTP_TIMEOUT="${FR_HTTP_TIMEOUT:-30}"

# fr::session_pod <namespace> — echo the name of a workspace session pod in
# the namespace (our curl client: same identity + CA + egress as an agent).
fr::session_pod() {
  local ns="$1"
  kubectl --context="${FR_CONTEXT}" -n "${ns}" \
    get pod -l "${FR_SESSION_LABEL}" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true
}

# fr::mint_sa_token <workspace> <namespace> — mint a short-lived (10m)
# projected SA token for the workspace's deterministic SA (ksa-<wsuid>).
# Echoes the raw token on stdout; callers must never log it (rule 05.10).
fr::mint_sa_token() {
  local ws_name="$1" ns="$2"
  local wsuid
  wsuid="$(kubectl --context="${FR_CONTEXT}" \
    get workspace "${ws_name}" -n "${ns}" \
    -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
  if [[ -z "${wsuid}" ]]; then
    echo "[fire-request] workspace ${ns}/${ws_name} not found" >&2
    return 1
  fi
  local sa_name="ksa-${wsuid}"
  local token
  token="$(kubectl --context="${FR_CONTEXT}" create token "${sa_name}" \
    -n "${ns}" --duration=600s 2>/dev/null || true)"
  if [[ -z "${token}" ]]; then
    echo "[fire-request] could not mint SA token for ${ns}/${sa_name}" >&2
    return 1
  fi
  printf '%s' "${token}"
}

# fr::fire <namespace> <token> <path> [method] [body] — fire one real
# request through the gateway from inside the namespace's session pod.
# Echoes ONLY the HTTP status code (never the body — rule 02 + 05.10).
fr::fire() {
  local ns="$1" token="$2" path="$3"
  local method="${4:-POST}" body="${5:-{}}"
  local pod
  pod="$(fr::session_pod "${ns}")"
  if [[ -z "${pod}" ]]; then
    echo "000"
    return 0
  fi
  kubectl --context="${FR_CONTEXT}" -n "${ns}" \
    exec "${pod}" -- bash -c "
      curl -s -o /dev/null -w '%{http_code}' \
        --max-time ${FR_HTTP_TIMEOUT} \
        --cacert ${FR_CA_PATH} \
        -X ${method} 'https://${FR_GATEWAY_HOST}${path}' \
        -H 'Authorization: Bearer ${token}' \
        -H 'content-type: application/json' \
        -d '${body}'
    " 2>/dev/null || true
}

# fr::assert_status <case-id> <namespace> <token> <expected> <path> [method] [body]
# Fire once; pass iff the status equals expected. Returns non-zero on miss.
fr::assert_status() {
  local case_id="$1" ns="$2" token="$3" expected="$4" path="$5"
  local method="${6:-POST}" body="${7:-{}}"
  local code
  code="$(fr::fire "${ns}" "${token}" "${path}" "${method}" "${body}")"
  if [[ "${code}" == "${expected}" ]]; then
    echo "[fire-request][${case_id}] ${path} -> http=${code} (expected ${expected}) OK"
    return 0
  fi
  echo "[fire-request][${case_id}] ${path} -> http=${code:-???} (expected ${expected}) FAIL" >&2
  return 1
}

# fr::poll_status <case-id> <namespace> <token> <expected> <timeout-s> <path> [method] [body]
# Re-fire a real request until the status equals expected or the deadline
# passes. No sleep-as-assertion: the loop body always fires (rule 06).
fr::poll_status() {
  local case_id="$1" ns="$2" token="$3" expected="$4" timeout="$5" path="$6"
  local method="${7:-POST}" body="${8:-{}}"
  local deadline=$(($(date +%s) + timeout))
  local code=""
  while [[ $(date +%s) -lt ${deadline} ]]; do
    code="$(fr::fire "${ns}" "${token}" "${path}" "${method}" "${body}")"
    if [[ "${code}" == "${expected}" ]]; then
      echo "[fire-request][${case_id}] flipped to http=${code} (expected ${expected}) OK"
      return 0
    fi
    echo "[fire-request][${case_id}] http=${code:-???}; waiting for flip to ${expected}" >&2
  done
  echo "[fire-request][${case_id}] never reached http=${expected} within ${timeout}s (last=${code:-???}) FAIL" >&2
  return 1
}

# fr::warm_up <namespace> <token> <path> — poll a benign request until the
# gateway returns a non-5xx (xDS hot-load can transiently 5xx after a
# data-plane restart). Best-effort; never fails the suite.
fr::warm_up() {
  local ns="$1" token="$2" path="$3"
  local deadline=$(($(date +%s) + 30))
  local code
  while [[ $(date +%s) -lt ${deadline} ]]; do
    code="$(fr::fire "${ns}" "${token}" "${path}")"
    if [[ "${code}" =~ ^[1-4][0-9][0-9]$ ]]; then
      echo "[fire-request] warm-up: gateway returned ${code} — ready"
      return 0
    fi
    echo "[fire-request] warm-up: gateway returned ${code:-???}; retrying" >&2
  done
  echo "[fire-request] warm-up: gateway never returned <5xx within 30s; running anyway" >&2
  return 0
}
