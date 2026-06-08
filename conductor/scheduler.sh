#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Conductor wave scheduler. Re-scans plan state every wave, picks phases that are
# READY (deps satisfied, not already done or running), predicts each one's
# filesystem footprint, and greedy-colors a conflict-free batch up to the
# concurrency cap. Emits a wave manifest as JSON on stdout. Read-only: never
# writes the ledger.
#
# Usage:
#   conductor/scheduler.sh [--max N] [--ledger PATH] [--plans-dir DIR]
#
# The "effective status" of a phase is the most-advanced status across its own
# frontmatter, the plans README table, and the run ledger — so a stale
# frontmatter never causes the conductor to re-dispatch finished work.

set -euo pipefail
IFS=$'\n\t'

if ((BASH_VERSINFO[0] < 4)); then
  echo "scheduler.sh requires bash >= 4 (enter the Nix dev shell: 'nix develop')" >&2
  exit 3
fi

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="${HERE}/lib"
# shellcheck source=conductor/lib/common.sh
source "${LIB}/common.sh"
# shellcheck source=conductor/lib/conductor-utils.sh
source "${LIB}/conductor-utils.sh"
# shellcheck source=conductor/lib/footprint.sh
source "${LIB}/footprint.sh"
# shellcheck source=conductor/lib/agents.sh
source "${LIB}/agents.sh" # for agents::for_phase (resolve the dispatch persona)

MAX_CONCURRENT="${CONDUCT_MAX_CONCURRENT:-4}"
LEDGER_PATH=""
PLANS_DIR="${REPO_ROOT}/docs/plans"

while (($# > 0)); do
  case "$1" in
    --max)
      MAX_CONCURRENT="$2"
      shift 2
      ;;
    --max=*)
      MAX_CONCURRENT="${1#--max=}"
      shift
      ;;
    --ledger)
      LEDGER_PATH="$2"
      shift 2
      ;;
    --ledger=*)
      LEDGER_PATH="${1#--ledger=}"
      shift
      ;;
    --plans-dir)
      PLANS_DIR="$2"
      shift 2
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 2
      ;;
  esac
done

cu::require jq python3 >/dev/null

FM_PARSER="${LIB}/frontmatter.py"

# --- status ranking ---------------------------------------------------------
# Higher = more advanced. Used to combine sources and to evaluate revisit gates.
sched::rank() {
  case "$1" in
    "" | draft | open) echo 0 ;;
    planned) echo 1 ;;
    in-progress) echo 2 ;;
    scaffold-only) echo 3 ;;
    # keese vocab: `partial` ≈ shipped-with-stubs (dep-satisfying, requeueable).
    shipped-with-stubs | partial) echo 4 ;;
    # keese vocab: `shipped` ≈ complete (terminal, do not re-dispatch).
    done | complete | merged | shipped) echo 5 ;;
    historical | superseded) echo 9 ;; # treat as "do not touch"
    *) echo 0 ;;
  esac
}

# --- data stores ------------------------------------------------------------
declare -A FM_STATUS DEPS MODEL FILE DISPATCH AGENT STAGE
declare -A RM_STATUS LEDGER_STATUS
declare -A RV_PHASE RV_PSTATUS RV_ENV
declare -a ALL_IDS=()

# --- parse plans README tables → README status map (secondary signal) --------
# keese keeps phase index tables in several README.md files (docs/plans/README.md
# plus demo/ and expansion/) whose columns differ (6 / 4 / 5 wide). Frontmatter
# status: is authoritative; this is only a stale-frontmatter safety net. Two
# invariants hold across every keese table, so we parse by them rather than by a
# fixed column index: the Phase ID is always the FIRST content cell, and Status
# is always the LAST content cell. We take the first whitespace token of each
# (so a status like "partial (cloud deferred)" ranks as `partial`).
sched::load_readme() {
  local readme line id status
  while IFS= read -r readme; do
    [[ -f "${readme}" ]] || continue
    while IFS= read -r line; do
      [[ "${line}" == \|* ]] || continue
      # skip header + separator rows
      [[ "${line}" == *'---'* ]] && continue
      [[ "${line}" == *'| Phase '* || "${line}" == '|Phase'* ]] && continue
      # first content cell (after the leading pipe) → phase id token
      id="$(awk -F'|' '{print $2}' <<<"${line}" | xargs 2>/dev/null || true)"
      id="${id%% *}"
      # last non-empty content cell → status; first token of it
      status="$(awk -F'|' '{for(i=NF;i>=1;i--){gsub(/^ +| +$/,"",$i); if($i!=""){print $i; break}}}' <<<"${line}" 2>/dev/null || true)"
      status="${status%% *}"
      [[ -n "${id}" && -n "${status}" ]] || continue
      # only record for ids that look like keese phase ids (P0, D5, E11, TD)
      [[ "${id}" =~ ^[A-Za-z]+[0-9]*$ ]] || continue
      RM_STATUS["${id}"]="${status}"
    done <"${readme}"
  done < <(find "${PLANS_DIR}" -name README.md -type f 2>/dev/null)
}

# --- load ledger statuses ---------------------------------------------------
sched::load_ledger() {
  [[ -n "${LEDGER_PATH}" && -f "${LEDGER_PATH}" ]] || return 0
  local id st
  while IFS=$'\t' read -r id st; do
    [[ -n "${id}" ]] && LEDGER_STATUS["${id}"]="${st}"
  done < <(jq -r '.slots | to_entries[] | "\(.key|ltrimstr("phase-"))\t\(.value.status)"' "${LEDGER_PATH}" 2>/dev/null || true)
}

# --- scan phase files -------------------------------------------------------
# keese phase docs do NOT use a flat docs/plans/phase-*.md convention; they live
# in track subdirs (docs/plans/demo/D*.md, docs/plans/expansion/E*.md, …) under
# arbitrary slugged filenames. Discover every *.md recursively and treat a doc as
# a phase iff its frontmatter carries a `phase:` key (the migrated schema).
sched::scan() {
  local f fm id
  while IFS= read -r f; do
    [[ -n "${f}" ]] || continue
    case "${f##*/}" in README.md | rubric.md) continue ;; esac
    fm="$(python3 "${FM_PARSER}" "${f}" 2>/dev/null || echo '{}')"
    id="$(jq -r '.phase // ""' <<<"${fm}")"
    [[ -n "${id}" ]] || continue
    ALL_IDS+=("${id}")
    FILE["${id}"]="${f}"
    FM_STATUS["${id}"]="$(jq -r '.status // ""' <<<"${fm}")"
    MODEL["${id}"]="$(jq -r '.model_tier // "sonnet"' <<<"${fm}")"
    DISPATCH["${id}"]="$(jq -r '.dispatch // ""' <<<"${fm}")"
    AGENT["${id}"]="$(jq -r '.agent // ""' <<<"${fm}")"
    STAGE["${id}"]="$(jq -r '.stage // ""' <<<"${fm}")"
    DEPS["${id}"]="$(jq -r '(.depends_on // []) | join(" ")' <<<"${fm}")"
    RV_PHASE["${id}"]="$(jq -r '.revisit_when_phase // ""' <<<"${fm}")"
    RV_PSTATUS["${id}"]="$(jq -r '.revisit_when_status // "merged"' <<<"${fm}")"
    RV_ENV["${id}"]="$(jq -r '.revisit_when_env // ""' <<<"${fm}")"
    # stash the full frontmatter for footprinting (outputs)
    printf '%s' "${fm}" >"${TMPDIR_SCHED}/${id}.fm.json"
  done < <(find "${PLANS_DIR}" -type f -name '*.md' 2>/dev/null | sort)
}

# --- effective status (max advancement across sources) ----------------------
sched::effective() {
  local id="$1" best=0 r
  for src in "${FM_STATUS[$id]:-}" "${RM_STATUS[$id]:-}" "${LEDGER_STATUS[$id]:-}"; do
    r="$(sched::rank "${src}")"
    ((r > best)) && best="${r}"
  done
  echo "${best}"
}

# --- is this id an umbrella? (>= 2 prefix-children) -------------------------
sched::is_umbrella() {
  local id="$1" other count=0
  for other in "${ALL_IDS[@]}"; do
    [[ "${other}" != "${id}" && "${other}" == "${id}"* ]] && ((count++))
  done
  ((count >= 2))
}

# --- dep satisfied? (every dep is shipped-with-stubs or better) -------------
sched::deps_ok() {
  local id="$1" dep r
  # DEPS[$id] is space-joined, but the script-level IFS=$'\n\t' does not split on
  # space — so a phase with >1 dependency would otherwise be read as a single
  # token ("26b 26c"), match no known phase, hit the unknown-dep skip below, and
  # have ALL its deps silently ignored. Split on space (+ newline/tab) here.
  local IFS=$' \n\t'
  for dep in ${DEPS[$id]:-}; do
    # tolerate a dep id that isn't a known phase (e.g. "seed-games-catalog")
    if [[ -z "${FILE[$dep]:-}" && -z "${RM_STATUS[$dep]:-}" ]]; then
      continue
    fi
    r="$(sched::effective "${dep}")"
    (("${r}" >= 4)) || return 1 # need shipped-with-stubs(4)+ for a dep
  done
  return 0
}

# --- revisit gate cleared? (for shipped-with-stubs auto-requeue) ------------
sched::revisit_cleared() {
  local id="$1"
  local wp="${RV_PHASE[$id]:-}" wenv="${RV_ENV[$id]:-}"
  # env-var trigger: condition clears when the var is set in .env.local
  if [[ -n "${wenv}" ]]; then
    local val
    val="$(bash -c "source '${REPO_ROOT}/.env.local' 2>/dev/null; printf '%s' \"\${${wenv}:-}\"" 2>/dev/null || true)"
    [[ -n "${val}" ]] && return 0
  fi
  # phase-status trigger: clears when the named phase reaches the wanted rank
  if [[ -n "${wp}" ]]; then
    local want got
    want="$(sched::rank "${RV_PSTATUS[$id]:-merged}")"
    got="$(sched::effective "${wp}")"
    (("${got}" >= "${want}")) && return 0
  fi
  return 1
}

# --- main -------------------------------------------------------------------
TMPDIR_SCHED="$(mktemp -d)"
trap 'rm -rf "${TMPDIR_SCHED}"' EXIT

sched::load_readme
sched::load_ledger
sched::scan

declare -a READY=() BLOCKED=() FP_IDS=()
declare -A FOOTPRINT

for id in "${ALL_IDS[@]}"; do
  eff="$(sched::effective "${id}")"
  fm_rank="$(sched::rank "${FM_STATUS[$id]:-}")"

  # already done / historical / superseded → nothing to do
  ((eff >= 5)) && continue
  [[ "$(sched::rank "${FM_STATUS[$id]:-}")" -ge 9 ]] && continue

  # already active in the ledger → being built, skip
  case "${LEDGER_STATUS[$id]:-}" in
    queued | dispatching | running | stuck | merge-pending) continue ;;
  esac

  # umbrellas are coordination nodes, not implementable units
  if sched::is_umbrella "${id}"; then
    continue
  fi

  # dispatch: manual — authored on main by the orchestrator (e.g. platform
  # phases that edit protected paths); never auto-dispatched.
  if [[ "${DISPATCH[$id]:-}" == "manual" ]]; then
    continue
  fi

  # determine readiness
  reason=""
  if ! sched::deps_ok "${id}"; then
    BLOCKED+=("${id}|deps not satisfied: ${DEPS[$id]:-none}")
    continue
  fi

  if ((fm_rank == 1)); then
    # planned → ready
    reason="planned, deps ok"
  elif [[ "${FM_STATUS[$id]:-}" == "shipped-with-stubs" ]]; then
    if sched::revisit_cleared "${id}"; then
      reason="stub revisit cleared (${RV_PHASE[$id]:-}${RV_ENV[$id]:+ env:${RV_ENV[$id]}})"
    else
      continue # stub, no cleared revisit trigger → leave for human
    fi
  else
    continue # in-progress(2)/scaffold-only(3) without revisit → not auto-dispatched
  fi

  READY+=("${id}|${reason}")
  FP_IDS+=("${id}")
  FOOTPRINT["${id}"]="$(footprint::for_phase "${id}" "${FILE[$id]}" "$(cat "${TMPDIR_SCHED}/${id}.fm.json")")"
done

# --- greedy conflict-free batch (ordered by phase id ascending) -------------
mapfile -t SORTED < <(printf '%s\n' "${FP_IDS[@]:-}" | sort -V)
declare -a WAVE=() DEFERRED=()
for id in "${SORTED[@]}"; do
  [[ -n "${id}" ]] || continue
  if ((${#WAVE[@]} >= MAX_CONCURRENT)); then
    DEFERRED+=("${id}|wave full (max ${MAX_CONCURRENT})")
    continue
  fi
  conflict=""
  for chosen in "${WAVE[@]:-}"; do
    [[ -n "${chosen}" ]] || continue
    if footprint::conflicts "${FOOTPRINT[$id]}" "${FOOTPRINT[$chosen]}"; then
      conflict="${chosen}"
      break
    fi
  done
  if [[ -n "${conflict}" ]]; then
    DEFERRED+=("${id}|footprint conflict with ${conflict}")
  else
    WAVE+=("${id}")
  fi
done

# --- emit manifest ----------------------------------------------------------
{
  echo '{'
  echo "  \"generated_at\": \"$(cu::now_iso)\","
  echo "  \"max_concurrent\": ${MAX_CONCURRENT},"
  echo "  \"ready_count\": ${#FP_IDS[@]},"
  # wave
  printf '  "wave": ['
  first=1
  for id in "${WAVE[@]:-}"; do
    [[ -n "${id}" ]] || continue
    ((first)) || printf ','
    first=0
    reason=""
    for r in "${READY[@]}"; do [[ "${r%%|*}" == "${id}" ]] && reason="${r#*|}"; done
    # resolve the dispatch persona: agent: > stage: > model_tier (agents.sh)
    agent="$(agents::for_phase "${AGENT[$id]:-}" "${STAGE[$id]:-}" "${MODEL[$id]:-sonnet}")"
    jq -nc --arg id "${id}" --arg file "${FILE[$id]#"${REPO_ROOT}/"}" \
      --arg model "${MODEL[$id]:-sonnet}" --arg agent "${agent}" --arg reason "${reason}" \
      --argjson fp "${FOOTPRINT[$id]}" \
      '{phase_id:$id, phase_file:$file, model:$model, agent:$agent, reason:$reason, footprint:$fp}'
  done
  printf '],\n'
  # deferred
  printf '  "deferred": ['
  first=1
  for d in "${DEFERRED[@]:-}"; do
    [[ -n "${d}" ]] || continue
    ((first)) || printf ','
    first=0
    jq -nc --arg id "${d%%|*}" --arg reason "${d#*|}" '{phase_id:$id, reason:$reason}'
  done
  printf '],\n'
  # blocked
  printf '  "blocked": ['
  first=1
  for b in "${BLOCKED[@]:-}"; do
    [[ -n "${b}" ]] || continue
    ((first)) || printf ','
    first=0
    jq -nc --arg id "${b%%|*}" --arg reason "${b#*|}" '{phase_id:$id, reason:$reason}'
  done
  printf ']\n'
  echo '}'
} | jq .
