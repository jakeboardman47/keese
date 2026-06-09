#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# E2E: live ReBAC allow/deny decision through the running ext_authz
# (keese-authz). This is the keystone authz suite (EH4). It proves the
# Envoy AI Gateway's ext_authz filter makes a CRD-driven authorization
# decision against the live OpenFGA store — not a model unit test, the
# real request path:
#
#   workspace pod  --(projected SA token, mounted CA)-->  Envoy AI Gateway
#       --> ext_authz (keese-authz) --> OpenFGA Check
#       --> 200 (allow) | 403 (deny, fail-closed)
#
# The decision is implemented in internal/authz/extauth/{check,resolver,
# subject}.go + internal/rebac/. `Authorize` resolves a ToolBinding from
# the request path, extracts the SA subject from the Bearer JWT, then
# calls `fga.Check(service_account:<sa>, can_call, tool:<finalToolName>)`.
# OpenFGA grants only when BOTH tuples exist:
#   - tenant:<t>#member@service_account:<sa>      (Workspace controller)
#   - tool:<n>#allowed_in@workspace:<wsuid>        (from egress.allowedTools)
# Drop either and Check denies → ext_authz returns 403 (fail-closed).
#
# Cases:
#   1. ALLOW   — `my-ws` grants anthropic.messages → POST → HTTP 200.
#   2. DENY    — `deny-ws` (empty egress.allowedTools, no tool tuple) →
#                identical POST → HTTP 403.
#   3. AUDIT   — the keese-authz deny audit line captures
#                (binding/tool, user, workspace, decision=deny) and is
#                free of any token / request-body material (rule 05.10).
#   4. REVOKE  — delete `my-ws`'s authorizing tuple, re-fire, assert the
#                decision flips allow→deny within the ext_authz cache TTL.
#
# Reuses the request-firing pattern from scripts/dev/test-aigw-defense.sh
# (curl from inside a workspace session pod that already carries the
# mounted gateway CA + projected SA token).
#
# Per .claude/rules/06-testing.md this is the `e2e` tier. Run via the
# kuttl suite (tests/e2e/rebac-decision/00-test.yaml) under
# `make test-e2e`. Prereq-gated by ../lib/check-prereqs.sh — skips
# cleanly when OpenFGA / OpenBao are placeholder fallbacks.
#
# Usage: tests/e2e/rebac-decision/test-rebac-decision.sh
#        KUBECTL_CONTEXT=kind-keese-demo tests/e2e/rebac-decision/test-rebac-decision.sh
#
# Refs: scripts/dev/test-aigw-defense.sh   (request-firing pattern)
#       internal/authz/extauth/check.go    (Authorize decision flow)
#       internal/authz/extauth/audit.go    (rule 05.10 audit allowlist)
#       dev/bootstrap/aigateway/keese-authz.yaml  (ext_authz deployment)

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

# shellcheck source=../../../scripts/lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
WORKSPACE_NS="${WORKSPACE_NS:-alpha}"
SESSION_LABEL="${SESSION_LABEL:-keese.ai/session}"
GATEWAY_HOST="${GATEWAY_HOST:-envoy-ai-gateway.keese-system.svc:443}"
CA_PATH="${CA_PATH:-/var/run/keese/ca/ca.crt}"
MODEL="${MODEL:-claude-opus-4-7}"
MESSAGES_PATH="${MESSAGES_PATH:-/anthropic/v1/messages}"

# ext_authz (keese-authz) deployment — its stdout carries the structured
# audit lines emitted by extauth.LogAudit.
AUTHZ_NS="${AUTHZ_NS:-keese-system}"
AUTHZ_LABEL="${AUTHZ_LABEL:-app.kubernetes.io/name=keese-authz}"

# Workspaces under test. ALLOW_WS is the granted workspace (from
# dev/demo/hello-keese.yaml). DENY_WS is provisioned by 00-apply.yaml in
# this suite with an empty egress.allowedTools (no authorizing tuple).
ALLOW_WS="${ALLOW_WS:-my-ws}"
DENY_WS="${DENY_WS:-deny-ws}"

# Cache flip budget — Authorize's OpenFGA result is cached per gateway
# pod; the revocation case polls up to this long for allow→deny.
# Generous default keeps the test non-flaky on a warm cluster while
# still failing if revocation never propagates.
REVOKE_TIMEOUT_S="${REVOKE_TIMEOUT_S:-90}"

# ── Preflight ───────────────────────────────────────────────────────────────
#
# We don't strict-check `kind-keese-*` here (kuttl renames the context to
# `cluster` in its sandboxed kubeconfig). Production-context refusal lives
# in .claude/settings.json + scripts/guard-kube-context.sh. Below we verify
# the keese authz stack is actually present.

log::info "test-rebac-decision: cluster=${CONTEXT} workspace_ns=${WORKSPACE_NS}"

# A workspace session pod is our curl client — same identity (projected SA
# token), CA bundle, and egress policy as a real agent pod.
SESSION_POD="$(kubectl --context="${CONTEXT}" -n "${WORKSPACE_NS}" \
  get pod -l "${SESSION_LABEL}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
if [[ -z "${SESSION_POD}" ]]; then
  log::err "no workspace session pod found in ${WORKSPACE_NS} (label ${SESSION_LABEL})."
  log::err "  Apply dev/demo/hello-keese.yaml first, or set WORKSPACE_NS / SESSION_LABEL."
  exit 1
fi
log::info "using client pod: ${WORKSPACE_NS}/${SESSION_POD}"

# The keese-authz (ext_authz) deployment must be up to make a decision.
AUTHZ_POD="$(kubectl --context="${CONTEXT}" -n "${AUTHZ_NS}" \
  get pod -l "${AUTHZ_LABEL}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
if [[ -z "${AUTHZ_POD}" ]]; then
  log::err "no keese-authz ext_authz pod found in ${AUTHZ_NS} (label ${AUTHZ_LABEL})."
  log::err "  Apply dev/bootstrap/aigateway/keese-authz.yaml first."
  exit 1
fi
log::info "ext_authz pod: ${AUTHZ_NS}/${AUTHZ_POD}"

# ── SA token minting ─────────────────────────────────────────────────────────
#
# ext_authz requires a valid SA token in `Authorization: Bearer <jwt>`. We
# mint a 10-minute token (kubectl create token minimum) for the workspace's
# SA. The SA name is `ksa-<workspace-uid>` (see subject.go extractWorkspace).
mint_sa_token() {
  local ws_name="$1"
  local wsuid
  wsuid="$(kubectl --context="${CONTEXT}" \
    get workspace "${ws_name}" -n "${WORKSPACE_NS}" \
    -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
  if [[ -z "${wsuid}" ]]; then
    log::err "workspace ${WORKSPACE_NS}/${ws_name} not found"
    return 1
  fi
  local sa_name="ksa-${wsuid}"
  local token
  token="$(kubectl --context="${CONTEXT}" create token "${sa_name}" \
    -n "${WORKSPACE_NS}" --duration=600s 2>/dev/null || true)"
  if [[ -z "${token}" ]]; then
    log::err "could not mint SA token for ${WORKSPACE_NS}/${sa_name}"
    return 1
  fi
  printf '%s' "${token}"
}

# ── Request firing ───────────────────────────────────────────────────────────
#
# Fire a real POST to the canonical native-Anthropic messages path through
# the gateway, from inside the session pod, with the supplied SA token.
# Echoes ONLY the HTTP status code (never the body — rule 02 + 05.10).
fire_request() {
  local token="$1"
  kubectl --context="${CONTEXT}" -n "${WORKSPACE_NS}" \
    exec "${SESSION_POD}" -- bash -c "
      curl -s -o /dev/null -w '%{http_code}' \
        --max-time 30 \
        --cacert ${CA_PATH} \
        -X POST 'https://${GATEWAY_HOST}${MESSAGES_PATH}' \
        -H 'Authorization: Bearer ${token}' \
        -H 'content-type: application/json' \
        -H 'anthropic-version: 2023-06-01' \
        -d '{\"model\":\"${MODEL}\",\"max_tokens\":8,\"messages\":[{\"role\":\"user\",\"content\":\"ok\"}]}'
    " 2>/dev/null || true
}

# Assert a single fire produces the expected status.
assert_status() {
  local case_id="$1" token="$2" expected="$3"
  local code
  code="$(fire_request "${token}")"
  if [[ "${code}" == "${expected}" ]]; then
    log::ok "[${case_id}] ${MESSAGES_PATH} → http=${code} (expected ${expected})"
    return 0
  fi
  log::err "[${case_id}] ${MESSAGES_PATH} → http=${code:-???} (expected ${expected})"
  return 1
}

# Poll for a status flip (used by the revocation case). Re-fires until the
# code matches `expected` or the deadline passes. No sleep-as-assertion:
# the loop body always re-fires a real request (rule 06 "Eventually").
poll_status() {
  local case_id="$1" token="$2" expected="$3" timeout="$4"
  local deadline=$(($(date +%s) + timeout))
  local code=""
  while [[ $(date +%s) -lt ${deadline} ]]; do
    code="$(fire_request "${token}")"
    if [[ "${code}" == "${expected}" ]]; then
      log::ok "[${case_id}] flipped to http=${code} (expected ${expected})"
      return 0
    fi
    log::dim "[${case_id}] http=${code:-???}; waiting for flip to ${expected}"
  done
  log::err "[${case_id}] never reached http=${expected} within ${timeout}s (last=${code:-???})"
  return 1
}

# ── Warm-up ──────────────────────────────────────────────────────────────────
#
# After a gateway data-plane restart, Envoy reports Ready before its xDS
# hot-load is effective; first requests can 5xx transiently. Poll a benign
# request with the ALLOW token until we see a non-5xx, then run cases.
warm_up_gateway() {
  local token="$1"
  local deadline=$(($(date +%s) + 30))
  local code
  while [[ $(date +%s) -lt ${deadline} ]]; do
    code="$(fire_request "${token}")"
    if [[ "${code}" =~ ^[1-4][0-9][0-9]$ ]]; then
      log::ok "warm-up: gateway returned ${code} — ready"
      return 0
    fi
    log::dim "warm-up: gateway returned ${code:-???}; retrying"
  done
  log::warn "warm-up: gateway never returned <5xx within 30s; running cases anyway"
  return 0
}

# ── Audit-log assertions (rule 05.10) ────────────────────────────────────────
#
# The deny decision emits one structured audit line on keese-authz stdout
# via extauth.LogAudit. We assert:
#   (a) a deny line is present that captures the decision tuple shape
#       (decision=deny, plus user/tool/workspace fields), AND
#   (b) NO token material or request-body content appears in those lines.
# We inspect only the audit lines (those carrying `decision`), and we
# never echo raw tokens here — the token is matched by substring and only
# its presence/absence is reported (rule 02 CI hygiene).
authz_logs_since() {
  # $1 = since-time seconds. Pull only the audit lines.
  local since="$1"
  kubectl --context="${CONTEXT}" -n "${AUTHZ_NS}" \
    logs "${AUTHZ_POD}" --since="${since}s" 2>/dev/null \
    | grep -F 'decision' || true
}

# Assert the deny audit line carries the (tuple, SA, host/workspace,
# decision) shape and is clean of secret material.
assert_audit_clean_deny() {
  local case_id="$1" since="$2" token="$3"
  local logs
  logs="$(authz_logs_since "${since}")"

  if [[ -z "${logs}" ]]; then
    log::err "[${case_id}] no ext_authz audit lines found in keese-authz logs"
    return 1
  fi

  local fail=0

  # (a) A deny decision must be present with its identifying fields. The
  #     audit allowlist (audit.go AuditFields) logs decision/user/tool/
  #     workspace — the (tuple, SA, host) the spec requires.
  if ! grep -Eq 'decision[^a-z]*deny' <<<"${logs}"; then
    log::err "[${case_id}] no deny decision found in audit lines"
    fail=1
  fi
  for field in user tool workspace; do
    if ! grep -q "${field}" <<<"${logs}"; then
      log::err "[${case_id}] audit line missing required field: ${field}"
      fail=1
    fi
  done

  # (b) Fail-closed secret check: the SA bearer token must NOT appear, and
  #     no obvious request-body / bearer material may leak (rule 05.10).
  if grep -qF "${token}" <<<"${logs}"; then
    log::err "[${case_id}] FAIL (rule 05.10): SA token leaked into audit log"
    fail=1
  fi
  for forbidden in 'Bearer ' 'max_tokens' 'eyJ'; do
    if grep -qF "${forbidden}" <<<"${logs}"; then
      log::err "[${case_id}] FAIL (rule 05.10): forbidden material '${forbidden}' in audit log"
      fail=1
    fi
  done

  if [[ "${fail}" -eq 0 ]]; then
    log::ok "[${case_id}] deny audit line captures (tuple,SA,host,decision); no token/body"
    return 0
  fi
  return 1
}

# ── Run ──────────────────────────────────────────────────────────────────────

FAILURES=0

ALLOW_TOKEN="$(mint_sa_token "${ALLOW_WS}")" || exit 1
DENY_TOKEN="$(mint_sa_token "${DENY_WS}")" || exit 1
log::info "minted SA tokens for ${ALLOW_WS} (allow) and ${DENY_WS} (deny)"

warm_up_gateway "${ALLOW_TOKEN}"

# Case 1 — ALLOW: granted workspace → 200.
assert_status "allow-granted" "${ALLOW_TOKEN}" "200" \
  || FAILURES=$((FAILURES + 1))

# Window (seconds) for the deny audit grep. Kept tight so we read only the
# lines this run produced.
DENY_SINCE=5

# Case 2 — DENY: ungranted workspace (no tool tuple) → 403, fail-closed.
assert_status "deny-ungranted" "${DENY_TOKEN}" "403" \
  || FAILURES=$((FAILURES + 1))

# Case 3 — AUDIT: the deny line is clean (rule 05.10).
assert_audit_clean_deny "deny-audit-clean" "${DENY_SINCE}" "${DENY_TOKEN}" \
  || FAILURES=$((FAILURES + 1))

# Case 4 — REVOKE: strip the ALLOW workspace's authorizing tool tuple by
# clearing its egress.allowedTools, then poll for allow→deny. The Workspace
# controller deletes the `tool#allowed_in` tuple on reconcile; ext_authz's
# cached allow expires within the cache TTL.
log::info "[revoke] clearing egress.allowedTools on ${ALLOW_WS} to revoke the grant"
if kubectl --context="${CONTEXT}" -n "${WORKSPACE_NS}" patch workspace "${ALLOW_WS}" \
  --type=merge -p '{"spec":{"egress":{"allowedTools":[]}}}' >/dev/null 2>&1; then
  poll_status "revoke-flip" "${ALLOW_TOKEN}" "403" "${REVOKE_TIMEOUT_S}" \
    || FAILURES=$((FAILURES + 1))
else
  log::err "[revoke-flip] could not patch ${ALLOW_WS} egress.allowedTools"
  FAILURES=$((FAILURES + 1))
fi

if [[ "${FAILURES}" -gt 0 ]]; then
  log::err "test-rebac-decision: ${FAILURES} case(s) failed"
  exit 1
fi

log::ok "test-rebac-decision: all cases passed (allow 200, deny 403, clean audit, revocation flip)"
