#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Dispatch ONE phase as a detached, headless `claude -p` implementer in an
# isolated worktree. Prints a JSON slot descriptor on stdout for the conductor
# to record in the ledger (single-writer rule: only conductor.sh writes it).
#
# Usage:
#   conductor/dispatch.sh --run-dir DIR --phase phase-23c --model sonnet \
#       [--base main] [--resume-sha SHA] [--dry-run]
#
# The agent runs with cwd in the worktree and is granted --add-dir to the conduct
# directory so it can write its status.json / session.log / SUMMARY.md back to the
# main repo's gitignored .plan-logs/conduct tree.

set -euo pipefail
IFS=$'\n\t'

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="${HERE}/lib"
# shellcheck source=conductor/lib/common.sh
source "${LIB}/common.sh"
# shellcheck source=conductor/lib/conductor-utils.sh
source "${LIB}/conductor-utils.sh"
# shellcheck source=conductor/lib/agents.sh
source "${LIB}/agents.sh"
# shellcheck source=conductor/lib/registry.sh
source "${LIB}/registry.sh"

RUN_DIR=""
PHASE=""
AGENT="implementer"
MODEL=""
BASE="main"
RESUME_SHA=""
DRY_RUN=0
EFFORT_OVERRIDE=""
PHASE_FILE=""

while (($# > 0)); do
  case "$1" in
    --run-dir)
      RUN_DIR="$2"
      shift 2
      ;;
    --phase)
      PHASE="$2"
      shift 2
      ;;
    --phase-file)
      PHASE_FILE="$2"
      shift 2
      ;;
    --agent)
      AGENT="$2"
      shift 2
      ;;
    --model)
      MODEL="$2"
      shift 2
      ;;
    --base)
      BASE="$2"
      shift 2
      ;;
    --resume-sha)
      RESUME_SHA="$2"
      shift 2
      ;;
    --effort)
      EFFORT_OVERRIDE="$2"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    *)
      log::err "unknown flag: $1"
      exit 2
      ;;
  esac
done

[[ -n "${RUN_DIR}" && -n "${PHASE}" ]] || {
  log::err "usage: dispatch.sh --run-dir DIR --phase PHASE [--agent NAME] [--model M] [--dry-run]"
  exit 2
}
cu::require claude jq python3 >/dev/null || {
  log::err "claude + jq + python3 required"
  exit 1
}
agents::exists "${AGENT}" || {
  log::err "unknown agent '${AGENT}' (no .claude/agents/${AGENT}.md)"
  exit 2
}

phase_dir="${RUN_DIR}/${PHASE}"
mkdir -p "${phase_dir}"
status_path="${phase_dir}/status.json"
log_path="${phase_dir}/session.log"
stream_path="${phase_dir}/stream.jsonl"
stderr_path="${phase_dir}/stderr.log"
summary_path="${phase_dir}/SUMMARY.md"
prompt_path="${phase_dir}/prompt.md"

# 1) Create (or reattach) the isolated worktree + branch via the existing tool.
#    Dry-run computes the path without creating anything.
slug="$(echo "${PHASE}" | tr '[:upper:]/' '[:lower:]-')"
branch="agent/${slug}-${AGENT}"
if ((DRY_RUN)); then
  wt_path="$(paths::worktree_base)/${slug}-${AGENT}"
else
  wt_path="$("${HERE}/agent-dispatch.sh" "${PHASE}" "${AGENT}" --branch="${branch}" --base="${BASE}" \
    | tail -1)"
  [[ -d "${wt_path}" ]] || {
    log::err "worktree not created for ${PHASE} (got '${wt_path}')"
    exit 1
  }
fi

# 2) Resolve the phase plan file. Prefer the explicit --phase-file passed by the
#    conductor (taken from the scheduler manifest); otherwise search recursively
#    under docs/plans (keese keeps phase docs in track subdirs like
#    docs/plans/expansion/E1-*.md, not as flat docs/plans/<id>.md).
phase_file=""
if [[ -n "${PHASE_FILE}" ]]; then
  phase_file="${PHASE_FILE#"${REPO_ROOT}/"}"
else
  cand="$(find "${REPO_ROOT}/docs/plans" -type f \( -name "${PHASE}.md" -o -name "${PHASE}-*.md" \) 2>/dev/null | sort | head -1 || true)"
  [[ -n "${cand}" ]] && phase_file="${cand#"${REPO_ROOT}/"}"
fi

# 3) Seed the per-phase status + summary scaffolding.
jq -n --arg p "${PHASE}" --arg now "$(cu::now_iso)" \
  '{phase_id:$p, state:"queued", step:"dispatched", pct:0, updated_at:$now}' >"${status_path}"
: >"${log_path}"
cat >"${summary_path}" <<EOF
<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# ${PHASE} — conductor run summary

(Implementer fills this in. Required sections: What shipped, Stubs shipped,
Follow-ups, Test evidence, MEMORY.md entries to add on merge.)
EOF

# 4) Assign a session id up front → deterministic, tailable transcript path.
session_id="$(cu::uuid)"

# 5) Compose the implementer prompt.
{
  cat <<EOF
Task: implement ${phase_file:-docs/plans/${PHASE}.md} for ${PHASE}, dispatched by
the Conductor. Working tree: ${wt_path} (this is your cwd; you are on branch
${branch}). Your agent persona, model, effort, and tools are already loaded via
--agent; additionally follow these Conductor operating rules:
1. Read the plan and the skills it names. Do NOT load unrelated docs.
2. Source the reporting helper and report progress at every step:
     source conductor/lib/conduct-log.sh
     conduct::state planning "reading plan"      # then implementing/testing/committing
     conduct::pct 25                              # rough progress
   These write your tailable session log + heartbeat; they are no-ops if unset.
3. COMMIT FREQUENTLY — one Conventional Commit per logical unit. Commits are the
   conductor's checkpoints; uncommitted work is lost if the run is interrupted.
4. Run 'make lint' and 'make test'; fix failures before claiming done.
5. If you must ship a stub, declare it (Stub policy) AND add a revisit block to
   the phase frontmatter (revisit_when_phase / revisit_when_env) so the conductor
   can auto-requeue it later. Set status: shipped-with-stubs (never 'complete').
6. Do NOT edit protected paths — conductor/worktree-merge.sh enforces the list and
   will reject the merge. Propose such changes in your SUMMARY instead.
7. Write your final summary to: ${summary_path}
EOF
  if [[ -n "${RESUME_SHA}" ]]; then
    cat <<EOF

RESUMING. Your prior work is already committed on this branch; inspect it first
with: git log --oneline ${BASE}..HEAD
Continue from where it left off — do not redo committed work. Last known commit: ${RESUME_SHA}
If ${phase_dir}/review.json exists, it lists BLOCKING review findings from a prior
pass — fix every one of them before you finish.
EOF
  fi
} >"${prompt_path}"

# 6) Write an inspectable launcher into the phase dir. It bakes in the CONDUCT_*
#    environment (absolute paths into the main repo's gitignored conduct tree),
#    cd's into the worktree, and execs the headless implementer. cwd = worktree;
#    --add-dir grants the agent write access to the conduct dir on main.
#    Paths are single-quoted in the heredoc; a single quote in any path would
#    break the launcher, so fail loudly rather than emit a broken script.
for _p in "${wt_path}" "${phase_dir}" "${prompt_path}" "${log_path}" "${status_path}" "${summary_path}"; do
  case "${_p}" in
    *\'*)
      log::err "path contains a single quote, refusing to build launcher: ${_p}" >&2
      exit 1
      ;;
  esac
done
run_id="$(basename "${RUN_DIR}")"
# Model + effort come from the agent persona (claude -p --agent <name>). MODEL /
# EFFORT_OVERRIDE are optional explicit overrides; when empty the agent frontmatter
# drives them. Plus an optional per-agent hard cost cap from budget-guard.json.
MODEL_FLAG=""
[[ -n "${MODEL}" ]] && MODEL_FLAG="--model '${MODEL}'"
EFFORT_FLAG=""
[[ -n "${EFFORT_OVERRIDE}" ]] && EFFORT_FLAG="--effort '${EFFORT_OVERRIDE}'"
MAXBUDGET="$(jq -r '.perAgentMaxUSD // empty' "${REPO_ROOT}/conductor/config/budget-guard.json" 2>/dev/null || true)"
BUDGET_FLAG=""
[[ -n "${MAXBUDGET}" && "${MAXBUDGET}" != "null" ]] && BUDGET_FLAG="--max-budget-usd '${MAXBUDGET}'"
cat >"${phase_dir}/launch.sh" <<LAUNCH
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
set -euo pipefail
export CONDUCT_RUN_ID='${run_id}'
export CONDUCT_PHASE_ID='${PHASE}'
export CONDUCT_DIR='${phase_dir}'
export CONDUCT_LOG_PATH='${log_path}'
export CONDUCT_STATUS_PATH='${status_path}'
export CONDUCT_SUMMARY_PATH='${summary_path}'
export CONDUCT_SESSION_ID='${session_id}'
cd '${wt_path}'
exec claude -p \\
  --agent '${AGENT}' \\
  --session-id '${session_id}' \\
  --permission-mode dontAsk \\
  ${MODEL_FLAG} ${EFFORT_FLAG} ${BUDGET_FLAG} \\
  --fallback-model sonnet \\
  --add-dir '${phase_dir}' \\
  --output-format stream-json --include-partial-messages --verbose \\
  <'${prompt_path}'
LAUNCH
chmod +x "${phase_dir}/launch.sh"

# 7) Launch (or dry-run) the detached headless implementer. nohup + background +
#    disown so the agent survives the conductor's parent shell losing its
#    connection. Recovery is still ledger-driven if the process dies entirely.
# stdout is the machine-readable slot contract; all human logs go to stderr.
pid=0
if ((DRY_RUN)); then
  log::info "[dry-run] would launch implementer for ${PHASE}:" >&2
  log::dim "  launcher: ${phase_dir}/launch.sh  (cwd=${wt_path} agent=${AGENT} session=${session_id})" >&2
else
  nohup "${phase_dir}/launch.sh" >"${stream_path}" 2>"${stderr_path}" &
  pid=$!
  disown "${pid}" 2>/dev/null || true
  log::ok "dispatched ${PHASE} (pid=${pid}, session=${session_id})" >&2
  # Record in the unified run registry so /workflows can see this phase.
  registry::record phase "${PHASE}" "${branch}" "${pid}" "${phase_dir}" "${status_path}"
fi

# 8) Emit the slot descriptor for the conductor to record in the ledger.
slot_model="$(agents::resolve "${AGENT}" | cut -f1)"
[[ -n "${MODEL}" ]] && slot_model="${MODEL}"
jq -nc \
  --arg phase_id "${PHASE}" \
  --arg branch "${branch}" \
  --arg worktree "${wt_path}" \
  --arg phase_file "${phase_file}" \
  --arg session_id "${session_id}" \
  --arg agent "${AGENT}" \
  --arg model "${slot_model}" \
  --argjson pid "${pid}" \
  --arg stream "${stream_path}" \
  --arg status_path "${status_path}" \
  --arg log_path "${log_path}" \
  --arg dispatched_at "$(cu::now_iso)" \
  '{phase_id:$phase_id, branch:$branch, worktree:$worktree, phase_file:$phase_file,
    session_id:$session_id, agent:$agent, model:$model, pid:$pid, stream:$stream,
    status_path:$status_path, log_path:$log_path, status:"dispatching",
    attempts:1, commits:0, cost_usd:0, dispatched_at:$dispatched_at}'
