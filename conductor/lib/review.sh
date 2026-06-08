# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Optional LLM passes the static heuristics can't do alone:
#   - review::conflict_check — a second opinion on whether a wave's phases would
#     collide when run in parallel (augments footprint.sh).
#   - review::phase — a post-implementation review of a phase's diff; blocking
#     findings send the phase back to the implementer before the merge gate.
# Both are BEST-EFFORT: any failure (no claude, bad JSON, timeout) yields the
# SAFE default (no conflicts / not blocking) so they never wedge the loop.
# Source me; do not execute.
# shellcheck shell=bash

if [[ -n "${__LIB_REVIEW_SH_LOADED:-}" ]]; then
  return 0
fi
__LIB_REVIEW_SH_LOADED=1

__rv_here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=conductor/lib/common.sh
source "${__rv_here}/common.sh"

# review::_json <result-text> — extract the last {...} object from claude text.
review::_json() { grep -oE '\{.*\}' <<<"$1" | tail -1; }

# review::conflict_check <wave-manifest.json> — echo phase_ids to DROP from the
# wave (newline-separated) because an LLM judged they would conflict with another
# batched phase beyond what footprints caught. Empty = no change.
review::conflict_check() {
  local manifest="$1" phases n out obj
  command -v claude >/dev/null 2>&1 || return 0
  phases="$(jq -c '[.wave[] | {phase_id, phase_file, footprint}]' "${manifest}" 2>/dev/null)" || return 0
  n="$(jq 'length' <<<"${phases}" 2>/dev/null || echo 0)"
  ((n >= 2)) || return 0
  out="$(cd "${REPO_ROOT}" && timeout "${CONDUCT_REVIEW_TIMEOUT:-120}" claude -p \
    --model haiku --permission-mode plan --output-format json \
    "These phases will run IN PARALLEL in separate git worktrees. Read each phase plan file and judge whether any TWO would edit the same files/areas and conflict on merge (beyond the footprints given). Reply with ONLY compact JSON: {\"drop\":[\"phase-id\", ...]} listing the phase(s) to defer to avoid a conflict (drop the fewest; prefer dropping the later phase id). Empty list if safe. Phases: ${phases}" \
    2>/dev/null | jq -r '.result // empty' 2>/dev/null)" || return 0
  obj="$(review::_json "${out}")" || return 0
  jq -r '(.drop // [])[]' <<<"${obj}" 2>/dev/null || true
}

# review::phase <phase-id> <worktree> <branch> <findings-out> — review the diff
# main..branch. Writes findings to <findings-out>. Returns 0 if OK to merge,
# 1 if there are BLOCKING findings (caller should send it back to the implementer).
review::phase() {
  local phase="$1" wt="$2" branch="$3" out_file="$4" diff out obj blocking
  command -v claude >/dev/null 2>&1 || return 0
  [[ -d "${wt}" ]] || return 0
  diff="$(git -C "${wt}" diff --stat "main...${branch}" 2>/dev/null | tail -40)"
  [[ -n "${diff}" ]] || return 0
  out="$(cd "${wt}" && timeout "${CONDUCT_REVIEW_TIMEOUT:-180}" claude -p \
    --agent security-reviewer --permission-mode plan --output-format json \
    "Review your worktree's changes for ${phase} (branch ${branch}) for correctness/security bugs and undeclared stubs, against docs/plans/${phase}*.md and .claude/agents/implementer.md. Reply with ONLY compact JSON: {\"blocking\":<bool>,\"findings\":[{\"severity\":\"blocker|high|medium|low\",\"issue\":\"...\",\"file\":\"...\"}]}. blocking=true only if there is a blocker/high correctness or security bug, or an undeclared stub." \
    2>/dev/null | jq -r '.result // empty' 2>/dev/null)" || return 0
  obj="$(review::_json "${out}")" || return 0
  [[ -n "${obj}" ]] || return 0
  printf '%s\n' "${obj}" | jq . >"${out_file}" 2>/dev/null || printf '%s\n' "${obj}" >"${out_file}"
  blocking="$(jq -r '.blocking // false' <<<"${obj}" 2>/dev/null || echo false)"
  [[ "${blocking}" == "true" ]] && return 1
  return 0
}
