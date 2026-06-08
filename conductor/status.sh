#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Live, read-only status dashboard for a conductor run. Renders a compact table
# from the run ledger + per-phase status.json + budget snapshot. No network, no
# writes. Ctrl-C to exit.
#
# Usage:
#   conductor/status.sh [--run RUN_ID] [--once] [--interval N]
#   # tail a single thread's detailed log:
#   tail -f .plan-logs/conduct/latest/<phase-id>/session.log

set -euo pipefail
IFS=$'\n\t'

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="${HERE}/lib"
# shellcheck source=conductor/lib/common.sh
source "${LIB}/common.sh"

CONDUCT_ROOT="${PLAN_LOGS}/conduct"
RUN_ID="latest"
ONCE=0
INTERVAL="${CONDUCT_STATUS_INTERVAL:-5}"

while (($# > 0)); do
  case "$1" in
    --run)
      RUN_ID="$2"
      shift 2
      ;;
    --once)
      ONCE=1
      shift
      ;;
    --interval)
      INTERVAL="$2"
      shift 2
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 2
      ;;
  esac
done

RUN_DIR="${CONDUCT_ROOT}/${RUN_ID}"
[[ -d "${RUN_DIR}" ]] || {
  echo "no run at ${RUN_DIR} (try --run <id>; runs:" >&2
  ls "${CONDUCT_ROOT}" 2>/dev/null >&2
  exit 1
}
LEDGER="${RUN_DIR}/ledger.json"

# Fixed-width 6-char ASCII tags so columns always align (no emoji double-width).
glyph() {
  case "$1" in
    running) printf 'RUN   ' ;;
    dispatching) printf 'DISP  ' ;;
    queued) printf 'QUEUE ' ;;
    merge-pending) printf 'MERGE?' ;;
    merged | done) printf 'DONE  ' ;;
    failed) printf 'FAIL  ' ;;
    stuck) printf 'STUCK ' ;;
    conflict) printf 'CONFL ' ;;
    blocked) printf 'BLOCK ' ;;
    *) printf '%-6.6s' "$1" ;;
  esac
}

render() {
  local now
  now="$(date -u +%H:%M:%SZ)"
  local run wave status
  run="$(jq -r '.run_id' "${LEDGER}" 2>/dev/null || echo '?')"
  wave="$(jq -r '.wave // 0' "${LEDGER}" 2>/dev/null || echo 0)"
  status="$(jq -r '.status // "?"' "${LEDGER}" 2>/dev/null || echo '?')"

  # budget line
  local snap bcost bceil bstate bslots
  snap="${RUN_DIR}/budget-snapshot.json"
  bcost="$(jq -r '.window_cost_usd // "?"' "${snap}" 2>/dev/null || echo '?')"
  bceil="$(jq -r '.window_ceiling_usd // "?"' "${snap}" 2>/dev/null || echo '?')"
  bstate="$(jq -r '.state // "?"' "${snap}" 2>/dev/null || echo '?')"
  bslots="$(jq -r '.slots // "?"' "${snap}" 2>/dev/null || echo '?')"

  printf '\033[2J\033[H'
  printf '╭─ Conductor %s · wave %s · %s · %s ───────────\n' "${run}" "${wave}" "${status}" "${now}"
  printf '│ budget: window $%s / $%s  [%s]  live-slots=%s\n' "${bcost}" "${bceil}" "${bstate}" "${bslots}"
  printf '├─────────┬────────┬────────┬─────┬─────┬────────┬──────────────────────────\n'
  printf '│ %-7s │ %-6s │ %-6s │ %3s │ %3s │ %6s │ %s\n' phase state model pct cmt "cost\$" step
  printf '├─────────┼────────┼────────┼─────┼─────┼────────┼──────────────────────────\n'

  local phase st model commits cost g sjson pct step
  while IFS= read -r phase; do
    [[ -n "${phase}" ]] || continue
    st="$(jq -r --arg p "${phase}" '.slots[$p].status // "?"' "${LEDGER}" 2>/dev/null || echo '?')"
    model="$(jq -r --arg p "${phase}" '.slots[$p].model // "-"' "${LEDGER}" 2>/dev/null || echo '-')"
    commits="$(jq -r --arg p "${phase}" '.slots[$p].commits // 0' "${LEDGER}" 2>/dev/null || echo 0)"
    cost="$(jq -r --arg p "${phase}" '.slots[$p].cost_usd // 0' "${LEDGER}" 2>/dev/null || echo 0)"
    sjson="${RUN_DIR}/${phase}/status.json"
    pct="$(jq -r '.pct // 0' "${sjson}" 2>/dev/null || echo 0)"
    step="$(jq -r '.step // ""' "${sjson}" 2>/dev/null || echo '')"
    g="$(glyph "${st}")"
    printf '│ %-7s │ %s │ %-6s │ %3s │ %3s │ %6s │ %.30s\n' \
      "${phase#phase-}" "${g}" "${model}" "${pct}" "${commits}" "${cost}" "${step}"
  done < <(jq -r '.slots | keys[]' "${LEDGER}" 2>/dev/null | sort -V)

  printf '╰── tail a thread:  tail -f %s/<phase>/session.log\n' "${RUN_DIR#"${REPO_ROOT}/"}"
}

if ((ONCE)); then
  render
else
  trap 'printf "\033[?25h\n"; exit 0' INT TERM
  printf '\033[?25l' # hide cursor
  while :; do
    render
    sleep "${INTERVAL}"
  done
fi
