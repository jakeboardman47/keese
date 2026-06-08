#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Conductor — autonomous parallel phase orchestrator. Runs the project in waves:
# scan → conflict-free batch → budget pre-flight → score → dispatch (detached) →
# poll (heartbeat · budget · refresh) → completion gate (ASK to merge) → advance.
#
# Hybrid autonomy: scan/score/implement are autonomous; merging to main and
# exceeding the budget ceiling are ASK gates. When run unattended (no tty), ASK
# gates take the SAFE default (hold the merge / do not exceed budget) so an
# overnight run never blocks and never spends past the ceiling.
#
# Usage:
#   conductor/conductor.sh [--max N] [--once] [--dry-run] [--resume]
#                          [--no-preflight] [--budget-setup]
#                          [--conflict-check] [--review]
#   See docs/designs/29-conductor-orchestration.md and docs/references/agent-dispatch.md.

set -euo pipefail
IFS=$'\n\t'

if ((BASH_VERSINFO[0] < 4)); then
  echo "conductor.sh requires bash >= 4 (enter the Nix dev shell: 'nix develop')" >&2
  exit 3
fi

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="${HERE}/lib"
# shellcheck source=conductor/lib/common.sh
source "${LIB}/common.sh"
# shellcheck source=conductor/lib/conductor-utils.sh
source "${LIB}/conductor-utils.sh"
# shellcheck source=conductor/lib/ledger.sh
source "${LIB}/ledger.sh"
# shellcheck source=conductor/lib/budget-guard.sh
source "${LIB}/budget-guard.sh"
# shellcheck source=conductor/lib/review.sh
source "${LIB}/review.sh"
# shellcheck source=conductor/lib/agents.sh
source "${LIB}/agents.sh"
# shellcheck source=conductor/lib/registry.sh
source "${LIB}/registry.sh"

CONDUCT_ROOT="${PLAN_LOGS}/conduct"
LOCK_FILE="${CONDUCT_ROOT}/conductor.lock"
POLL_SEC="${CONDUCT_POLL_SEC:-45}"
STUCK_SEC="${CONDUCT_STUCK_SEC:-900}" # no heartbeat AND no commit for this long
MAX_ATTEMPTS="${CONDUCT_MAX_ATTEMPTS:-3}"

MAX_CONCURRENT=""
ONCE=0
DRY_RUN=0
RESUME=0
PREFLIGHT=1
BUDGET_SETUP=0
CONFLICT_CHECK=0
REVIEW=0
MAX_REVIEW_ITERS="${CONDUCT_MAX_REVIEW_ITERS:-2}"

while (($# > 0)); do
  case "$1" in
    --max)
      MAX_CONCURRENT="$2"
      shift 2
      ;;
    --once)
      ONCE=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --resume)
      RESUME=1
      shift
      ;;
    --no-preflight)
      PREFLIGHT=0
      shift
      ;;
    --budget-setup)
      BUDGET_SETUP=1
      shift
      ;;
    --conflict-check)
      CONFLICT_CHECK=1
      shift
      ;;
    --review)
      REVIEW=1
      shift
      ;;
    -h | --help)
      sed -n '5,20p' "${BASH_SOURCE[0]}"
      exit 0
      ;;
    *)
      log::err "unknown flag: $1"
      exit 2
      ;;
  esac
done

RUN_DIR=""

# ─── ask gate (tty-aware) ───────────────────────────────────────────────────
# conductor::ask <prompt> [safe-default:yes|no] — interactive y/n when attended;
# returns the SAFE default (no, unless overridden) when no tty is present so
# unattended runs never block. Returns 0 for yes, 1 for no.
conductor::ask() {
  local prompt="$1" safe="${2:-no}" ans
  if [[ ! -t 0 ]]; then
    log::warn "[unattended] ${prompt} → defaulting to '${safe}'"
    [[ "${safe}" == yes ]]
    return
  fi
  read -r -p "${prompt} [y/N] " ans
  [[ "${ans}" =~ ^[Yy] ]]
}

# ─── startup ────────────────────────────────────────────────────────────────
conductor::startup_checks() {
  cu::require git jq python3 claude >/dev/null || exit 1
  if ! budget::config_ready; then
    log::warn "budget ceiling not set — running setup wizard"
    budget::setup_wizard || {
      log::err "budget config required; aborting"
      exit 1
    }
  fi
  local src
  src="$(budget::cost_source)"
  log::info "usage source: ${src}"
  [[ "${src}" == transcript-fallback* ]] && log::warn "live usage is APPROXIMATE; 'npm i -g ccusage' for accuracy"
  # bypassPermissions sanity (cheap, read-only).
  if ! ((DRY_RUN)); then
    if ! claude -p --permission-mode bypassPermissions --model haiku --output-format json "reply: ok" \
      >/dev/null 2>&1; then
      log::err "headless 'claude -p --permission-mode bypassPermissions' failed."
      log::err "Accept the bypass prompt once in an interactive 'claude' session, then retry."
      exit 1
    fi
  fi
}

# ─── run lifecycle ──────────────────────────────────────────────────────────
conductor::new_run() {
  local run_id
  run_id="$(date -u +%Y%m%d-%H%M%S)"
  RUN_DIR="${CONDUCT_ROOT}/${run_id}"
  mkdir -p "${RUN_DIR}"
  local cfg
  cfg="$(cat "${BUDGET_CONFIG}")"
  ledger::init "${RUN_DIR}/ledger.json" "${run_id}" "${cfg}"
  ln -sfn "${run_id}" "${CONDUCT_ROOT}/latest"
  registry::record conductor "${run_id}" "conductor wave loop" "$$" "${RUN_DIR}" "${RUN_DIR}/ledger.json"
  log::ok "new run ${run_id} (${RUN_DIR})"
}

conductor::resume_run() {
  local latest
  latest="$(readlink "${CONDUCT_ROOT}/latest" 2>/dev/null || true)"
  if [[ -z "${latest}" || ! -f "${CONDUCT_ROOT}/${latest}/ledger.json" ]]; then
    log::warn "no resumable run found; starting fresh"
    conductor::new_run
    return
  fi
  RUN_DIR="${CONDUCT_ROOT}/${latest}"
  LEDGER="${RUN_DIR}/ledger.json"
  export LEDGER
  registry::record conductor "${latest}" "conductor wave loop (resumed)" "$$" "${RUN_DIR}" "${RUN_DIR}/ledger.json"
  log::ok "resuming run ${latest}"
  conductor::recover
}

# Recovery decision table: classify each non-terminal slot and act.
conductor::recover() {
  local phase pid status commits branch wt
  while IFS= read -r phase; do
    [[ -n "${phase}" ]] || continue
    status="$(ledger::slot_get "${phase}" status)"
    case "${status}" in
      done | merged | failed | merge-pending | conflict | blocked) continue ;;
    esac
    pid="$(ledger::slot_get "${phase}" pid)"
    branch="$(ledger::slot_get "${phase}" branch)"
    wt="$(ledger::slot_get "${phase}" worktree)"
    commits=0
    [[ -d "${wt}" ]] && commits="$(git -C "${wt}" rev-list --count "main..${branch}" 2>/dev/null || echo 0)"
    if [[ -n "${pid}" && "${pid}" != "0" ]] && kill -0 "${pid}" 2>/dev/null; then
      log::info "recover ${phase}: process ${pid} still alive — reattaching"
      ledger::slot_status "${phase}" running
    elif ((commits > 0)); then
      log::warn "recover ${phase}: process gone, ${commits} commit(s) present — re-dispatch (resume)"
      ledger::slot_status "${phase}" queued
      ledger::slot_set "${phase}" resume_sha "$(git -C "${wt}" rev-parse --short "${branch}" 2>/dev/null || echo '')"
    else
      log::warn "recover ${phase}: process gone, no commits — re-dispatch fresh"
      ledger::slot_status "${phase}" queued
    fi
  done < <(ledger::all_slots)
}

# ─── preflight scoring (feature 6) ──────────────────────────────────────────
# Echo SHIP|REVISE|REPLAN. Best-effort: any failure defaults to SHIP with a warn
# so a flaky scorer never blocks the build.
conductor::preflight_score() {
  local file="$1" out verdict
  ((PREFLIGHT)) || {
    echo SHIP
    return
  }
  # shellcheck disable=SC2015  # intentional best-effort: any failure -> empty out -> SHIP
  out="$(cd "${REPO_ROOT}" && claude -p --agent plan-scorer --permission-mode plan --output-format json \
    "Score ${file} against docs/plans/rubric.md. Reply with ONLY a compact JSON object: {\"score\":<0-100>,\"verdict\":\"SHIP\"|\"REVISE\"|\"REPLAN\"}." \
    2>/dev/null | jq -r '.result // empty' 2>/dev/null || true)"
  verdict="$(grep -oE '"verdict"[[:space:]]*:[[:space:]]*"(SHIP|REVISE|REPLAN)"' <<<"${out}" | grep -oE 'SHIP|REVISE|REPLAN' | head -1 || true)"
  echo "${verdict:-SHIP}"
}

# Re-dispatch slots recovery left as 'queued' (their agent died but the worktree
# + commits survive). The scheduler skips ledger-queued phases, so this is the
# only path that resumes them. The worktree is refreshed onto current main first
# (safe: no agent is running on it).
conductor::redispatch_queued() {
  local phase branch wt resume agent slot attempts
  while IFS= read -r phase; do
    [[ -n "${phase}" ]] || continue
    # Preserve the attempt count across re-dispatch (slot_init would reset it to
    # 1, allowing an infinite recover→queued→crash retry loop).
    attempts="$(ledger::slot_get "${phase}" attempts)"
    attempts="${attempts:-1}"
    if ((attempts >= MAX_ATTEMPTS)); then
      log::err "${phase}: ${attempts} attempts exhausted on resume — escalating"
      ledger::slot_status "${phase}" blocked
      continue
    fi
    branch="$(ledger::slot_get "${phase}" branch)"
    wt="$(ledger::slot_get "${phase}" worktree)"
    resume="$(ledger::slot_get "${phase}" resume_sha)"
    agent="$(ledger::slot_get "${phase}" agent)"
    agent="${agent:-implementer}"
    if [[ -n "${branch}" && -d "${wt}" ]]; then
      "${HERE}/worktree-refresh.sh" --branch "${branch}" --worktree "${wt}" --force >/dev/null 2>&1 || true
    fi
    # Exponential backoff + jitter before a retry (prior attempts > 1) so a
    # rate-limited / overloaded phase does not hammer the API on re-dispatch.
    if ((attempts > 1)); then
      local back
      back="$(cu::backoff_secs "$((attempts - 1))")"
      log::info "${phase}: backing off ${back}s before retry"
      sleep "${back}"
    fi
    log::info "re-dispatch ${phase} (attempt $((attempts + 1)), resume${resume:+ @${resume}})"
    local args=(--run-dir "${RUN_DIR}" --phase "${phase}" --agent "${agent}")
    [[ -n "${resume}" ]] && args+=(--resume-sha "${resume}")
    slot="$("${HERE}/dispatch.sh" "${args[@]}")"
    ledger::slot_init "${phase}" "${slot}"
    ledger::slot_set_json "${phase}" attempts "$((attempts + 1))"
    ledger::slot_status "${phase}" running
  done < <(ledger::slots_by_status queued)
}

# ─── dispatch a wave ────────────────────────────────────────────────────────
conductor::dispatch_wave() {
  local manifest="$1" slots stagger phase file model manifest_agent agent verdict slot
  if [[ -f "${RUN_DIR}/PAUSED" ]]; then
    log::warn "run paused (/workflows pause) — not dispatching this wave"
    return 0
  fi
  slots="$(budget::monitor_tick "${RUN_DIR}/budget-snapshot.json")"
  stagger="$(budget::cfg '.dispatchStaggerSeconds')"
  stagger="${stagger:-8}"
  if ((slots == 0)); then
    log::warn "budget paused — not dispatching this wave"
    return 0
  fi
  local dispatched=0
  while IFS=$'\t' read -r phase file model manifest_agent; do
    [[ -n "${phase}" ]] || continue
    ((dispatched < slots)) || {
      log::info "live budget slots (${slots}) reached; deferring rest of wave"
      break
    }
    verdict="$(conductor::preflight_score "${file}")"
    if [[ "${verdict}" != "SHIP" ]]; then
      log::warn "preflight ${phase}: ${verdict} — skipping dispatch (needs plan work)"
      ledger::slot_init "${phase}" "$(jq -n --arg p "${phase}" --arg v "${verdict}" \
        '{phase_id:$p, status:"blocked", note:("preflight "+$v)}')"
      continue
    fi
    # The scheduler already resolved the specialized persona (agent: > stage: >
    # model_tier) into the manifest's .agent field; trust it, falling back to the
    # tier map then implementer only if it is somehow empty.
    agent="${manifest_agent}"
    [[ -n "${agent}" ]] || agent="$(agents::for_tier "${model}")"
    agent="${agent:-implementer}"
    log::info "dispatch ${phase} (${agent}, tier ${model})"
    slot="$("${HERE}/dispatch.sh" --run-dir "${RUN_DIR}" --phase "${phase}" \
      --agent "${agent}" --phase-file "${file}")"
    ledger::slot_init "${phase}" "${slot}"
    ledger::slot_status "${phase}" running
    dispatched=$((dispatched + 1))
    ((dispatched < slots)) && sleep "${stagger}"
  done < <(jq -r '.wave[] | "\(.phase_id)\t\(.phase_file)\t\(.model)\t\(.agent // "")"' "${manifest}")
  log::ok "wave dispatched: ${dispatched} phase(s)"
}

# ─── poll the active wave to completion ─────────────────────────────────────
conductor::slot_poll() {
  local phase="$1" pid branch wt stream commits last hb now age status result is_err commit_ts commit_age
  status="$(ledger::slot_get "${phase}" status)"
  case "${status}" in dispatching | running | stuck) ;; *) return ;; esac
  pid="$(ledger::slot_get "${phase}" pid)"
  branch="$(ledger::slot_get "${phase}" branch)"
  wt="$(ledger::slot_get "${phase}" worktree)"
  stream="$(ledger::slot_get "${phase}" stream)"

  if [[ -d "${wt}" ]]; then
    commits="$(git -C "${wt}" rev-list --count "main..${branch}" 2>/dev/null || echo 0)"
    ledger::slot_set_json "${phase}" commits "${commits:-0}"
    last="$(git -C "${wt}" rev-parse --short "${branch}" 2>/dev/null || echo '')"
    [[ -n "${last}" ]] && ledger::slot_set "${phase}" last_commit "${last}"
  fi

  # process finished?
  if [[ -z "${pid}" || "${pid}" == "0" ]] || ! kill -0 "${pid}" 2>/dev/null; then
    # Last result line of the stream (grep|tail is portable; macOS has no `tac`).
    local resline
    resline="$(grep '"type":"result"' "${stream}" 2>/dev/null | tail -1 || true)"
    # NB: jq's `//` treats `false` as empty, so `.is_error // true` is WRONG
    # (it returns true when is_error is false). Test key presence explicitly.
    is_err="$(jq -r 'if has("is_error") then .is_error else true end' <<<"${resline}" 2>/dev/null || echo true)"
    result="$(jq -r '.total_cost_usd // 0' <<<"${resline}" 2>/dev/null || echo 0)"
    ledger::slot_set_json "${phase}" cost_usd "${result:-0}"
    if [[ "${is_err}" == "false" ]]; then
      log::ok "${phase}: implementer finished (\$${result})"
      ledger::slot_status "${phase}" merge-pending
    else
      log::warn "${phase}: implementer exited with error/incomplete"
      ledger::slot_status "${phase}" failed
    fi
    return
  fi

  # stuck detection: stuck only when BOTH the heartbeat AND the most-recent
  # commit are older than STUCK_SEC (a quiet but actively-committing agent that
  # never calls conduct::status must not be flagged).
  hb="$(cu::mtime "$(ledger::slot_get "${phase}" status_path)")"
  now="$(cu::epoch_now)"
  age=$((now - hb))
  commit_ts=0
  [[ -d "${wt}" ]] && commit_ts="$(git -C "${wt}" log -1 --format=%ct "${branch}" 2>/dev/null || echo 0)"
  commit_age=$((now - ${commit_ts:-0}))
  if ((age > STUCK_SEC && commit_age > STUCK_SEC)); then
    log::warn "${phase}: no heartbeat (${age}s) and no commit (${commit_age}s) — marking stuck"
    ledger::slot_status "${phase}" stuck
  fi
}

conductor::poll_wave() {
  local any phase slots
  while :; do
    slots="$(budget::monitor_tick "${RUN_DIR}/budget-snapshot.json")"
    ((slots == 0)) && log::warn "budget hard-pause active (window near ceiling)"
    any=0
    while IFS= read -r phase; do
      [[ -n "${phase}" ]] || continue
      any=1
      conductor::slot_poll "${phase}"
      # opportunistic mid-flight refresh if the helper exists
      if [[ -x "${HERE}/worktree-refresh.sh" ]]; then
        "${HERE}/worktree-refresh.sh" --branch "$(ledger::slot_get "${phase}" branch)" \
          --worktree "$(ledger::slot_get "${phase}" worktree)" --check-only >/dev/null 2>&1 || true
      fi
    done < <(ledger::inprogress_slots)
    ((any == 0)) && break
    sleep "${POLL_SEC}"
  done
}

# ─── completion gate (ASK before merge) ─────────────────────────────────────
conductor::completion_gate() {
  local phase branch summary wt riters
  while IFS= read -r phase; do
    [[ -n "${phase}" ]] || continue
    branch="$(ledger::slot_get "${phase}" branch)"
    wt="$(ledger::slot_get "${phase}" worktree)"
    summary="${RUN_DIR}/${phase}/SUMMARY.md"

    # Optional review-fix loop: review the diff; blocking findings send the phase
    # back to the implementer (with the findings) instead of offering a merge.
    if ((REVIEW)); then
      riters="$(ledger::slot_get "${phase}" review_iters)"
      riters="${riters:-0}"
      if ((riters < MAX_REVIEW_ITERS)) \
        && ! review::phase "${phase}" "${wt}" "${branch}" "${RUN_DIR}/${phase}/review.json"; then
        log::warn "${phase}: review found blocking issues — sending back to implementer (iter $((riters + 1)))"
        ledger::slot_set_json "${phase}" review_iters "$((riters + 1))"
        ledger::slot_set "${phase}" resume_sha "$(git -C "${wt}" rev-parse --short "${branch}" 2>/dev/null || echo '')"
        ledger::slot_status "${phase}" queued
        continue
      fi
    fi

    echo "──────── ${phase} ready to merge (branch ${branch}) ────────"
    [[ -f "${summary}" ]] && sed -n '1,40p' "${summary}"
    if conductor::ask "Merge ${phase} to main?" no; then
      if "${HERE}/worktree-merge.sh" "${branch}"; then
        ledger::slot_status "${phase}" merged
        ledger::slot_set "${phase}" merged_at "$(cu::now_iso)"
        conductor::apply_memory "${summary}"
        log::ok "merged ${phase}"
      else
        ledger::slot_status "${phase}" conflict
        log::err "${phase}: merge failed/conflict — left for manual resolution"
      fi
    else
      log::info "${phase}: held for review (still merge-pending)"
    fi
  done < <(ledger::slots_by_status merge-pending)

  # re-dispatch failed/stuck within attempt budget
  while IFS= read -r phase; do
    [[ -n "${phase}" ]] || continue
    conductor::handle_failed "${phase}"
  done < <(printf '%s\n%s\n' "$(ledger::slots_by_status failed)" "$(ledger::slots_by_status stuck)")
}

conductor::handle_failed() {
  local phase attempts
  [[ -n "${phase:=$1}" ]] || return
  attempts="$(ledger::slot_get "${phase}" attempts)"
  attempts="${attempts:-1}"
  if ((attempts >= MAX_ATTEMPTS)); then
    log::err "${phase}: ${attempts} attempts exhausted — escalating to user"
    ledger::slot_status "${phase}" blocked
    return
  fi
  if conductor::ask "${phase} failed (attempt ${attempts}). Re-dispatch?" no; then
    ledger::slot_set_json "${phase}" attempts "$((attempts + 1))"
    ledger::slot_status "${phase}" queued
  fi
}

# Append the SUMMARY's "MEMORY.md entries to add on merge" section to MEMORY.md.
conductor::apply_memory() {
  local summary="$1" entries
  [[ -f "${summary}" ]] || return 0
  entries="$(awk '/MEMORY\.md entries to add on merge/{f=1;next} /^## /{f=0} f' "${summary}" \
    | grep -E '^\s*-\s' || true)"
  if [[ -n "${entries}" ]]; then
    printf '%s\n' "${entries}" >>"${REPO_ROOT}/MEMORY.md"
    log::info "appended $(grep -c . <<<"${entries}") MEMORY.md entry(ies)"
  fi
}

# ─── dry-run preview ────────────────────────────────────────────────────────
conductor::preview() {
  local manifest="$1" verdict
  log::info "── DRY-RUN wave preview ──"
  jq -r '.wave[] | "  \(.phase_id) [\(.model)] — \(.reason)"' "${manifest}"
  echo "budget pre-flight:" >&2
  local pf
  pf="$(budget::preflight "${manifest}")"
  log::info "  verdict=$(awk '{print $1}' <<<"${pf}") wave≈\$$(awk '{print $2}' <<<"${pf}") projected≈\$$(awk '{print $3}' <<<"${pf}") ceiling=\$$(awk '{print $4}' <<<"${pf}")"
  if ((PREFLIGHT)); then
    log::info "  (preflight scoring skipped in dry-run; would gate each phase on SHIP)"
  fi
  log::info "deferred: $(jq -r '[.deferred[].phase_id]|join(", ")' "${manifest}")"
  log::info "blocked:  $(jq -r '[.blocked[].phase_id]|join(", ")' "${manifest}")"
}

# ─── main ───────────────────────────────────────────────────────────────────
main() {
  mkdir -p "${CONDUCT_ROOT}"

  if ((BUDGET_SETUP)); then
    budget::setup_wizard
    exit $?
  fi

  conductor::startup_checks

  if ! ((DRY_RUN)); then
    cu::lock_acquire "${LOCK_FILE}" || {
      log::err "another conductor holds ${LOCK_FILE} (pid $(cat "${LOCK_FILE}" 2>/dev/null))"
      exit 1
    }
    trap 'cu::lock_release "${LOCK_FILE}"' EXIT
  fi

  if ((RESUME)); then
    conductor::resume_run
  elif ((DRY_RUN)); then
    # dry-run uses an ephemeral ledger so it never disturbs a real run
    RUN_DIR="$(mktemp -d)"
    ledger::init "${RUN_DIR}/ledger.json" "dry" "$(cat "${BUDGET_CONFIG}")"
  else
    conductor::new_run
  fi

  local max manifest ready
  max="${MAX_CONCURRENT:-$(budget::cfg '.maxConcurrentSlots')}"
  max="${max:-4}"

  while :; do
    # Resume any recovered work first (queued slots the scheduler won't pick up).
    ((DRY_RUN)) || conductor::redispatch_queued

    manifest="${RUN_DIR}/wave.json"
    "${HERE}/scheduler.sh" --max "${max}" --ledger "${LEDGER}" >"${manifest}"

    # Optional LLM conflict-check: a second opinion that may drop colliding phases
    # the static footprint missed. Best-effort; safe-default keeps the full wave.
    if ((CONFLICT_CHECK)) && ! ((DRY_RUN)); then
      local drops dropjson
      drops="$(review::conflict_check "${manifest}" | sort -u | grep -v '^$' || true)"
      if [[ -n "${drops}" ]]; then
        log::warn "conflict-check defers: $(tr '\n' ' ' <<<"${drops}")"
        dropjson="$(printf '%s\n' "${drops}" | jq -R . | jq -sc .)"
        jq --argjson d "${dropjson}" \
          '.deferred += [.wave[] | select(.phase_id as $p | $d|index($p)) | {phase_id, reason:"llm conflict-check"}] | .wave |= map(select(.phase_id as $p | ($d|index($p))|not))' \
          "${manifest}" >"${manifest}.tmp" && mv "${manifest}.tmp" "${manifest}"
      fi
    fi
    ready="$(jq '.wave | length' "${manifest}")"

    if ((DRY_RUN)); then
      conductor::preview "${manifest}"
      rm -rf "${RUN_DIR}"
      break
    fi

    local active pending
    active="$(ledger::count_status running)"
    pending="$(ledger::count_status merge-pending)"
    if ((ready == 0 && active == 0)); then
      if ((pending > 0)); then
        if [[ -t 0 ]]; then
          # Attended: re-offer the held merges, then re-evaluate next loop.
          conductor::completion_gate
          continue
        fi
        log::warn "${pending} phase(s) held in merge-pending — run '/conduct --resume' to merge them"
      fi
      log::ok "no ready phases and nothing active — conductor done"
      break
    fi

    if ((ready > 0)); then
      local pf verdict
      pf="$(budget::preflight "${manifest}")"
      verdict="$(awk '{print $1}' <<<"${pf}")"
      if [[ "${verdict}" == "ASK" ]]; then
        log::warn "wave would cost ≈\$$(awk '{print $2}' <<<"${pf}"); projected window ≈\$$(awk '{print $3}' <<<"${pf}") > ceiling \$$(awk '{print $4}' <<<"${pf}")"
        if ! conductor::ask "Proceed past budget ceiling?" no; then
          log::warn "budget gate declined — pausing dispatch this wave"
          ledger::set '.status' "paused-budget"
          break
        fi
      fi
      conductor::dispatch_wave "${manifest}"
    fi

    conductor::poll_wave
    conductor::completion_gate

    ((ONCE)) && {
      log::info "--once: stopping after one wave"
      break
    }
  done
}

main "$@"
