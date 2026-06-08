# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Conductor usage/rate-limit guard. Two halves:
#   - STATIC pre-flight: estimate a wave's cost and decide whether it would
#     breach the window ceiling (the conductor turns a breach into an ASK).
#   - LIVE monitor: read the current window's actual cost (installed ccusage,
#     else a local transcript scan) and scale concurrency down / pause as the
#     ceiling approaches.
# Source me; do not execute.
# shellcheck shell=bash

if [[ -n "${__LIB_BUDGET_GUARD_SH_LOADED:-}" ]]; then
  return 0
fi
__LIB_BUDGET_GUARD_SH_LOADED=1

__bg_here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=conductor/lib/conductor-utils.sh
source "${__bg_here}/conductor-utils.sh"

BUDGET_CONFIG="${BUDGET_CONFIG:-${REPO_ROOT:-.}/conductor/config/budget-guard.json}"

# budget::cfg <jq-filter> — read a config value (raw).
budget::cfg() { jq -r "$1 // empty" "${BUDGET_CONFIG}" 2>/dev/null; }

# budget::config_ready — 0 if windowCeilingUSD is set (wizard has run).
budget::config_ready() {
  local c
  c="$(budget::cfg '.windowCeilingUSD')"
  [[ -n "${c}" && "${c}" != "null" ]]
}

# budget::classify_size <phase-file> — small | medium | large, from the count of
# numbered work-item sub-headings plus doc length.
budget::classify_size() {
  local f="$1" items lines
  [[ -f "${f}" ]] || {
    echo medium
    return
  }
  items="$(grep -cE '^### [0-9]+[a-z0-9]*\.[0-9]+' "${f}" 2>/dev/null || true)"
  items="${items:-0}"
  lines="$(wc -l <"${f}" 2>/dev/null | tr -d ' ')"
  lines="${lines:-0}"
  if ((items >= 8 || lines >= 350)); then
    echo large
  elif ((items >= 3 || lines >= 180)); then
    echo medium
  else
    echo small
  fi
}

# budget::estimate_phase <model-tier> <size-class> — USD estimate for one phase.
# Uses a calibrated value if present (calibration["<tier>:<size>"]), else the
# static base * size multiplier * safety margin.
budget::estimate_phase() {
  local tier="$1" size="$2" cal base mult margin
  cal="$(budget::cfg ".calibration[\"${tier}:${size}\"]")"
  if [[ -n "${cal}" && "${cal}" != "null" ]]; then
    printf '%s' "${cal}"
    return
  fi
  base="$(budget::cfg ".estimator.perTierUSD.${tier}")"
  mult="$(budget::cfg ".estimator.sizeMultiplier.${size}")"
  margin="$(budget::cfg '.estimatorSafetyMargin')"
  awk -v b="${base:-3}" -v m="${mult:-1}" -v s="${margin:-1.5}" 'BEGIN{printf "%.4f", b*m*s}'
}

# budget::estimate_wave <wave-manifest.json> — echo total USD for the wave plus a
# per-phase breakdown on stderr. Reads model + phase_file from the manifest.
budget::estimate_wave() {
  local manifest="$1" total=0 pid file model size est
  while IFS=$'\t' read -r pid file model; do
    [[ -n "${pid}" ]] || continue
    size="$(budget::classify_size "${REPO_ROOT}/${file}")"
    est="$(budget::estimate_phase "${model}" "${size}")"
    total="$(awk -v t="${total}" -v e="${est}" 'BEGIN{printf "%.4f", t+e}')"
    printf '  %-12s %-7s %-7s $%s\n' "${pid}" "${model}" "${size}" "${est}" >&2
  done < <(jq -r '.wave[] | "\(.phase_id)\t\(.phase_file)\t\(.model)"' "${manifest}" 2>/dev/null)
  printf '%s' "${total}"
}

# budget::window_cost_usd [hours] — actual cost in the current rate-limit window
# (ccusage's active 5h block). Preference order, most to least accurate:
#   1. installed `ccusage` binary (fast — recommended; `npm i -g ccusage`)
#   2. `npx ccusage` (network; cached after first run; skip with CONDUCT_NO_NPX=1)
#   3. local transcript scan (APPROXIMATE — fixed-grid window, coarse pricing)
# The transcript fallback can diverge from ccusage by a lot (different window
# anchoring + cache pricing); the conductor warns at startup when it must use it.
budget::window_cost_usd() {
  local hours="${1:-5}" cost=""
  if command -v ccusage >/dev/null 2>&1; then
    cost="$(ccusage blocks --json --active 2>/dev/null | jq -r '(.blocks[0].costUSD) // empty' 2>/dev/null)"
  fi
  if [[ -z "${cost}" && "${CONDUCT_NO_NPX:-0}" != "1" ]] && command -v npx >/dev/null 2>&1; then
    cost="$(timeout "${CONDUCT_CCUSAGE_TIMEOUT:-40}" npx -y ccusage@latest blocks --json --active 2>/dev/null | jq -r '(.blocks[0].costUSD) // empty' 2>/dev/null)"
  fi
  if [[ -z "${cost}" ]]; then
    cost="$(python3 "${__bg_here}/usage-window.py" --hours "${hours}" 2>/dev/null | jq -r '.window_cost_usd // 0' 2>/dev/null)"
  fi
  printf '%s' "${cost:-0}"
}

# budget::cost_source — which data source window_cost_usd will use (for warnings).
budget::cost_source() {
  if command -v ccusage >/dev/null 2>&1; then
    echo "ccusage (installed)"
  elif [[ "${CONDUCT_NO_NPX:-0}" != "1" ]] && command -v npx >/dev/null 2>&1; then
    echo "ccusage (npx)"
  else
    echo "transcript-fallback (APPROXIMATE — run 'npm i -g ccusage' for accuracy)"
  fi
}

# budget::scale_slots <window-cost> — integer concurrency slots in
# [0, maxConcurrentSlots]. 0 means hard-pause (>= pauseFraction of ceiling).
budget::scale_slots() {
  local cost="$1" ceil warn pause maxslots
  ceil="$(budget::cfg '.windowCeilingUSD')"
  warn="$(awk -v c="${ceil}" -v f="$(budget::cfg '.warnFraction')" 'BEGIN{printf "%.4f", c*(f==""?0.85:f)}')"
  pause="$(awk -v c="${ceil}" -v f="$(budget::cfg '.pauseFraction')" 'BEGIN{printf "%.4f", c*(f==""?0.95:f)}')"
  maxslots="$(budget::cfg '.maxConcurrentSlots')"
  maxslots="${maxslots:-4}"
  awk -v cost="${cost}" -v warn="${warn}" -v pause="${pause}" -v ceil="${ceil}" -v max="${maxslots}" 'BEGIN{
    if (cost >= pause) { print 0; exit }
    denom = ceil - warn; if (denom <= 0) denom = ceil>0?ceil:1;
    h = (ceil - cost) / denom; if (h<0) h=0; if (h>1) h=1;
    s = int(max*h + 0.5); if (s<1) s=1; if (s>max) s=max; print s
  }'
}

# budget::monitor_tick <snapshot-path> — compute window cost + slots and write a
# snapshot JSON the dashboard and dispatch loop read. Echoes the slot count.
budget::monitor_tick() {
  local snap="$1" hours cost ceil slots state
  hours="$(budget::cfg '.windowHours')"
  hours="${hours:-5}"
  cost="$(budget::window_cost_usd "${hours}")"
  ceil="$(budget::cfg '.windowCeilingUSD')"
  slots="$(budget::scale_slots "${cost}")"
  if ((slots == 0)); then
    state="paused"
  elif awk -v c="${cost}" -v w="$(awk -v ce="${ceil}" -v f="$(budget::cfg '.warnFraction')" 'BEGIN{printf "%.4f", ce*(f==""?0.85:f)}')" 'BEGIN{exit !(c>=w)}'; then
    state="warn"
  else
    state="ok"
  fi
  jq -n --arg now "$(cu::now_iso)" --argjson cost "${cost:-0}" \
    --argjson ceil "${ceil:-0}" --argjson slots "${slots}" --arg state "${state}" \
    '{updated_at:$now, window_cost_usd:$cost, window_ceiling_usd:$ceil, slots:$slots, state:$state}' \
    | cu::atomic_write "${snap}"
  printf '%s' "${slots}"
}

# budget::preflight <wave-manifest> — echo a verdict line:
#   "OK <wave_cost> <projected> <ceiling>"  |  "ASK <wave_cost> <projected> <ceiling>"
# ASK means projected (current window + this wave) would exceed the ceiling.
budget::preflight() {
  local manifest="$1" wave_cost cur projected ceil verdict
  wave_cost="$(budget::estimate_wave "${manifest}" 2>/dev/null)"
  cur="$(budget::window_cost_usd "$(budget::cfg '.windowHours')")"
  ceil="$(budget::cfg '.windowCeilingUSD')"
  projected="$(awk -v a="${cur:-0}" -v b="${wave_cost:-0}" 'BEGIN{printf "%.4f", a+b}')"
  if awk -v p="${projected}" -v c="${ceil:-0}" 'BEGIN{exit !(p>c)}'; then
    verdict=ASK
  else
    verdict=OK
  fi
  printf '%s %s %s %s' "${verdict}" "${wave_cost}" "${projected}" "${ceil}"
}

# budget::record_phase_cost <phase-cost> — add a finished agent's exact
# total_cost_usd to a running tally (for calibration + accurate reporting).
# Appends to the calibration sample log; the conductor folds these into config.
budget::calibrate() {
  local tier="$1" size="$2" actual="$3"
  local cur
  cur="$(budget::cfg ".calibration[\"${tier}:${size}\"]")"
  local merged
  if [[ -n "${cur}" && "${cur}" != "null" ]]; then
    merged="$(awk -v a="${cur}" -v b="${actual}" 'BEGIN{printf "%.4f", (a*0.7)+(b*0.3)}')"
  else
    merged="${actual}"
  fi
  local out
  out="$(jq --arg k "${tier}:${size}" --argjson v "${merged}" '.calibration[$k]=$v' "${BUDGET_CONFIG}")" \
    && printf '%s\n' "${out}" | cu::atomic_write "${BUDGET_CONFIG}"
}

# budget::setup_wizard — interactive: show recent windows (npx ccusage if needed)
# and prompt for a ceiling, writing it to config. Returns non-zero if declined.
budget::setup_wizard() {
  echo "── Conductor budget setup ───────────────────────────────────────────"
  echo "Recent Claude usage windows (cost is a proxy for rate-limit consumption):"
  if command -v ccusage >/dev/null 2>&1; then
    ccusage blocks 2>/dev/null | tail -12 || true
  else
    echo "(installing ccusage view via npx — one time, ~30s)…"
    timeout 60 npx -y ccusage@latest blocks 2>/dev/null | tail -12 \
      || python3 "${__bg_here}/usage-window.py" 2>/dev/null | jq .
  fi
  echo
  local ceil
  read -r -p "Set per-window (5h) USD ceiling for the conductor (e.g. 25): " ceil
  if ! [[ "${ceil}" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
    echo "no valid ceiling entered; aborting setup" >&2
    return 1
  fi
  local out
  out="$(jq --argjson c "${ceil}" '.windowCeilingUSD=$c' "${BUDGET_CONFIG}")" \
    && printf '%s\n' "${out}" | cu::atomic_write "${BUDGET_CONFIG}"
  echo "windowCeilingUSD set to \$${ceil} in ${BUDGET_CONFIG}"
}
