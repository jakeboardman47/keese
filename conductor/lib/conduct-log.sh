# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Agent-side conductor reporting. A dispatched implementer sources this and calls
# conduct::log / conduct::status at step boundaries. Every function is a safe
# no-op when the CONDUCT_* env vars are unset, so manual (non-conductor) runs are
# unaffected. Source me; do not execute.
# shellcheck shell=bash

if [[ -n "${__LIB_CONDUCT_LOG_SH_LOADED:-}" ]]; then
  return 0
fi
__LIB_CONDUCT_LOG_SH_LOADED=1

# Pull in cu::now_iso / cu::atomic_write if available; otherwise define minimal
# fallbacks so this helper works even on a branch whose base predates the libs.
__cl_here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "${__cl_here}/conductor-utils.sh" ]]; then
  # shellcheck source=conductor/lib/conductor-utils.sh
  source "${__cl_here}/conductor-utils.sh"
fi
if ! declare -F cu::now_iso >/dev/null 2>&1; then
  cu::now_iso() { date -u +"%Y-%m-%dT%H:%M:%SZ"; }
fi
if ! declare -F cu::atomic_write >/dev/null 2>&1; then
  cu::atomic_write() {
    local dest="$1" tmp
    tmp="${dest}.tmp.$$"
    cat >"${tmp}" && mv -f "${tmp}" "${dest}"
  }
fi

# conduct::_sanitize — drop common credential shapes from a log line. Defence in
# depth on top of the never-log-secrets rule; the conductor never wants a leaked
# token landing in a tailable file.
conduct::_sanitize() {
  sed -E \
    -e 's/(sk-[A-Za-z0-9_-]{8})[A-Za-z0-9_-]+/\1<redacted>/g' \
    -e 's/([Bb]earer )[A-Za-z0-9._-]+/\1<redacted>/g' \
    -e 's/(AKIA)[A-Z0-9]{12,}/\1<redacted>/g' \
    -e 's/(gh[pousr]_)[A-Za-z0-9]{20,}/\1<redacted>/g' \
    -e 's/(eyJ[A-Za-z0-9_-]{6})[A-Za-z0-9._-]+/\1<redacted-jwt>/g'
}

# conduct::log <level> <message...> — append a timestamped line to the per-phase
# session.log. Tailable with `tail -f`. No-op if CONDUCT_LOG_PATH is unset.
conduct::log() {
  [[ -n "${CONDUCT_LOG_PATH:-}" ]] || return 0
  local level="$1"
  shift
  local line
  line="$(printf '%s [%s] %s: %s' "$(cu::now_iso)" "${level}" "${CONDUCT_PHASE_ID:-?}" "$*" | conduct::_sanitize)"
  printf '%s\n' "${line}" >>"${CONDUCT_LOG_PATH}" 2>/dev/null || true
}

# conduct::status <jq-expr> [jq-args...] — merge a patch into the per-phase
# status.json (atomically). updated_at is always refreshed. No-op if unset.
conduct::status() {
  [[ -n "${CONDUCT_STATUS_PATH:-}" ]] || return 0
  command -v jq >/dev/null 2>&1 || return 0
  local expr="$1"
  shift
  local now base out
  now="$(cu::now_iso)"
  base='{}'
  [[ -f "${CONDUCT_STATUS_PATH}" ]] && base="$(cat "${CONDUCT_STATUS_PATH}" 2>/dev/null || echo '{}')"
  out="$(printf '%s' "${base}" | jq \
    --arg __now "${now}" --arg __p "${CONDUCT_PHASE_ID:-}" "$@" \
    "(.updated_at=\$__now) | (.phase_id=\$__p) | (${expr})" 2>/dev/null)" || return 0
  printf '%s\n' "${out}" | cu::atomic_write "${CONDUCT_STATUS_PATH}"
}

# conduct::status_kv <key> <value> — set one string field on status.json.
conduct::status_kv() {
  conduct::status ".[\$__k] = \$__v" --arg __k "$1" --arg __v "$2"
}

# conduct::state <state> [step] — common case: update the state glyph + step.
# shellcheck disable=SC2016  # jq program; $__s/$__step are jq variables.
conduct::state() {
  local state="$1" step="${2:-}"
  conduct::status '.state=$__s | (if $__step!="" then .step=$__step else . end)' \
    --arg __s "${state}" --arg __step "${step}"
  conduct::log info "state=${state}${step:+ — ${step}}"
}

# conduct::pct <0-100> — update the progress percentage.
# shellcheck disable=SC2016  # jq program; $__v is a jq variable.
conduct::pct() {
  conduct::status '.pct = ($__v|tonumber)' --arg __v "$1"
}
