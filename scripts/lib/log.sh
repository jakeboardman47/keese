# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Colorized logging + run::step mutation boundary. Source me; do not execute.
# shellcheck shell=bash

if [[ -n "${__LIB_LOG_SH_LOADED:-}" ]]; then
  return 0
fi
__LIB_LOG_SH_LOADED=1

if [[ -t 1 ]]; then
  __LOG_RED=$'\e[31m'
  __LOG_YEL=$'\e[33m'
  __LOG_GRN=$'\e[32m'
  __LOG_BLU=$'\e[34m'
  __LOG_DIM=$'\e[2m'
  __LOG_RST=$'\e[0m'
else
  __LOG_RED="" __LOG_YEL="" __LOG_GRN="" __LOG_BLU="" __LOG_DIM="" __LOG_RST=""
fi

log::info() { printf '%s[info]%s %s\n' "${__LOG_BLU}" "${__LOG_RST}" "$*"; }
log::ok() { printf '%s[ ok ]%s %s\n' "${__LOG_GRN}" "${__LOG_RST}" "$*"; }
log::warn() { printf '%s[warn]%s %s\n' "${__LOG_YEL}" "${__LOG_RST}" "$*" >&2; }
log::err() { printf '%s[err ]%s %s\n' "${__LOG_RED}" "${__LOG_RST}" "$*" >&2; }
log::dim() { printf '%s%s%s\n' "${__LOG_DIM}" "$*" "${__LOG_RST}"; }

# run::step <id> <desc> <fn> [args...]
# Wraps a function call with start/end logging, duration, and a breadcrumb in
# .plan-logs/state.json. Resume-friendly: --from / --to can skip steps by id.
run::step() {
  local id="$1" desc="$2" fn="$3"
  shift 3
  local from="${RUN_FROM:-}" to="${RUN_TO:-}"

  # Resume gating.
  if [[ -n "${from}" && "${id}" < "${from}" ]]; then
    log::dim "skip ${id} ${desc} (< --from=${from})"
    return 0
  fi
  if [[ -n "${to}" && "${id}" > "${to}" ]]; then
    log::dim "skip ${id} ${desc} (> --to=${to})"
    return 0
  fi

  log::info "step ${id} — ${desc}"
  local start
  start="$(date +%s)"

  if "${fn}" "$@"; then
    local elapsed=$(($(date +%s) - start))
    log::ok "step ${id} — ${desc} (${elapsed}s)"
    run::_breadcrumb "${id}" "ok"
  else
    local rc=$?
    local elapsed=$(($(date +%s) - start))
    log::err "step ${id} — ${desc} failed (rc=${rc}, ${elapsed}s)"
    run::_breadcrumb "${id}" "fail"
    return "${rc}"
  fi
}

run::_breadcrumb() {
  local id="$1" status="$2"
  local state="${PLAN_LOGS:-/tmp}/state.json"
  if command -v jq >/dev/null 2>&1 && [[ -f "${state}" ]]; then
    jq --arg id "${id}" --arg s "${status}" \
      '.last_step = $id | .steps[$id] = $s' "${state}" >"${state}.tmp" \
      && mv "${state}.tmp" "${state}"
  else
    printf '{"last_step":"%s","steps":{"%s":"%s"}}\n' "${id}" "${id}" "${status}" >"${state}"
  fi
}
