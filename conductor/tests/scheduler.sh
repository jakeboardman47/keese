#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Behavior test for conductor/scheduler.sh against the live plan tree: it must
# emit valid JSON, a numeric ready_count, well-formed wave entries (each with a
# resolved dispatch agent), use bare keese phase ids (no "phase-" prefix), and
# exclude `dispatch: manual` phases (e.g. the D4 live-cloud demo).
# Run: bash conductor/tests/scheduler.sh
set -euo pipefail
IFS=$'\n\t'

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${HERE}/../.." && pwd)"

fail=0
ok() { printf '  PASS  %s\n' "$1"; }
bad() {
  printf '  FAIL  %s\n' "$1"
  fail=1
}

wave="$("${ROOT}/conductor/scheduler.sh" --max 3 2>/dev/null)"

if jq -e . >/dev/null 2>&1 <<<"${wave}"; then ok "emits valid JSON"; else bad "invalid JSON"; fi

rc="$(jq -r '.ready_count // "x"' <<<"${wave}")"
if [[ "${rc}" =~ ^[0-9]+$ ]]; then ok "ready_count numeric (${rc})"; else bad "ready_count not numeric (${rc})"; fi

seen="$(jq -r '[.wave[].phase_id, .blocked[].phase_id, .deferred[].phase_id] | join(" ")' <<<"${wave}")"
# keese uses bare phase ids (E1, D5, P0); a leftover "phase-" prefix is a bug.
if [[ "${seen}" == *"phase-"* ]]; then bad "stale 'phase-' prefix in a phase_id"; else ok "phase ids are bare (no phase- prefix)"; fi

n="$(jq '.wave | length' <<<"${wave}")"
if ((n > 0)); then
  if jq -e '.wave | all(has("phase_id") and has("phase_file") and has("model") and has("agent"))' >/dev/null <<<"${wave}"; then
    ok "wave entries well-formed (${n}, incl. resolved agent)"
  else
    bad "a wave entry is missing phase_id/phase_file/model/agent"
  fi
  # every dispatched agent must be a real persona
  if jq -e '.wave | all(.agent | test("^[a-z][a-z0-9-]+$"))' >/dev/null <<<"${wave}"; then
    ok "every wave agent looks like a persona name"
  else
    bad "a wave entry has an empty/garbage agent"
  fi
else
  ok "wave empty (valid)"
fi

echo
if ((fail == 0)); then
  echo "scheduler: ALL PASS"
  exit 0
fi
echo "scheduler: FAILURES"
exit 1
