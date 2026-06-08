#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Dispatch-parity test: chat (/conduct, Agent tool) and program (conductor.sh,
# `claude -p --agent`) dispatch must resolve the SAME persona / model / effort /
# tools from .claude/agents/*, and the program launcher must adopt the persona
# via `--agent` (not a reconstructed system prompt). Also exercises keese's
# specialized-agent routing (agents::for_phase) and that the scheduler skips
# `dispatch: manual` phases. Run: bash conductor/tests/parity.sh
set -euo pipefail
IFS=$'\n\t'

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${HERE}/../.." && pwd)"
# shellcheck source=conductor/lib/common.sh
source "${ROOT}/conductor/lib/common.sh"
# shellcheck source=conductor/lib/agents.sh
source "${ROOT}/conductor/lib/agents.sh"

fail=0
ok() { printf '  PASS  %s\n' "$1"; }
bad() {
  printf '  FAIL  %s\n' "$1"
  fail=1
}
# eq <desc> <got> <want>
eq() {
  if [[ "$2" == "$3" ]]; then
    ok "$1 ($2)"
  else
    bad "$1: got '$2' want '$3'"
  fi
}
# present <desc> <pattern> <file> — pass iff pattern IS in file.
present() {
  if grep -q -- "$2" "$3"; then ok "$1"; else bad "$1"; fi
}
# absent <desc> <pattern> <file> — pass iff pattern is NOT in file.
absent() {
  if grep -q -- "$2" "$3"; then bad "$1"; else ok "$1"; fi
}

echo "== agents::resolve (model<TAB>effort) == agent frontmatter =="
eq "implementer" "$(agents::resolve implementer)" "$(printf 'sonnet\thigh')"
eq "architect" "$(agents::resolve architect)" "$(printf 'opus\txhigh')"
eq "security-reviewer" "$(agents::resolve security-reviewer)" "$(printf 'opus\txhigh')"
eq "rebac-modeler" "$(agents::resolve rebac-modeler)" "$(printf 'opus\txhigh')"
eq "crd-author" "$(agents::resolve crd-author)" "$(printf 'sonnet\thigh')"
eq "explorer" "$(agents::resolve explorer)" "$(printf 'haiku\t')"
eq "debugger" "$(agents::resolve debugger)" "$(printf 'haiku\t')"

echo "== agents::for_tier / for_stage (keese stages.json) =="
eq "tier opus" "$(agents::for_tier opus)" "architect"
eq "tier sonnet" "$(agents::for_tier sonnet)" "implementer"
eq "tier haiku" "$(agents::for_tier haiku)" "explorer"
eq "stage review" "$(agents::for_stage review)" "security-reviewer"
eq "stage crd" "$(agents::for_stage crd)" "crd-author"
eq "stage controller" "$(agents::for_stage controller)" "controller-author"
eq "stage olm" "$(agents::for_stage olm)" "olm-author"
eq "stage rebac" "$(agents::for_stage rebac)" "rebac-modeler"
eq "stage docs" "$(agents::for_stage docs)" "implementer"

echo "== agents::for_phase precedence (agent: > stage: > tier) =="
eq "explicit agent wins" "$(agents::for_phase crd-author '' sonnet)" "crd-author"
eq "stage when no agent" "$(agents::for_phase '' controller opus)" "controller-author"
eq "tier when only tier" "$(agents::for_phase '' '' opus)" "architect"
eq "unknown agent falls through to tier" "$(agents::for_phase not-an-agent '' sonnet)" "implementer"
eq "nothing → implementer" "$(agents::for_phase '' '' '')" "implementer"

echo "== dispatch.sh launcher adopts the persona via --agent =="
run="$(mktemp -d)"
"${ROOT}/conductor/dispatch.sh" --dry-run --run-dir "${run}" --phase smoke --agent architect >/dev/null 2>&1
launcher="${run}/smoke/launch.sh"
present "launcher --agent architect" "--agent 'architect'" "${launcher}"
present "launcher dontAsk" "--permission-mode dontAsk" "${launcher}"
absent "no bypassPermissions" "bypassPermissions" "${launcher}"
absent "no --system-prompt" "--system-prompt" "${launcher}"
rm -rf "${run}"

echo "== scheduler skips dispatch: manual (D4 live-cloud demo) =="
wave="$("${ROOT}/conductor/scheduler.sh" --max 99 2>/dev/null)"
seen="$(jq -r '[.wave[].phase_id, .blocked[].phase_id, .deferred[].phase_id] | join(" ")' <<<"${wave}")"
# Match D4 as a whole token so it doesn't false-match e.g. "D4x".
if [[ " ${seen} " == *" D4 "* ]]; then
  bad "D4 (dispatch:manual) surfaced in scheduler"
else
  ok "D4 skipped (dispatch:manual or not-yet-ready)"
fi

echo
if ((fail == 0)); then
  echo "parity: ALL PASS"
  exit 0
fi
echo "parity: FAILURES"
exit 1
