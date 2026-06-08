#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Tests for conductor/lib/registry.sh: append-only record/status, and the
# fold-by-id (last-status-wins) that /workflows reads. Run: bash conductor/tests/registry.sh
set -euo pipefail
IFS=$'\n\t'

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${HERE}/../.." && pwd)"
CONDUCT_REGISTRY="$(mktemp)"
export CONDUCT_REGISTRY
# shellcheck source=conductor/lib/common.sh
source "${ROOT}/conductor/lib/common.sh"
# shellcheck source=conductor/lib/registry.sh
source "${ROOT}/conductor/lib/registry.sh"

fail=0
ok() { printf '  PASS  %s\n' "$1"; }
bad() {
  printf '  FAIL  %s\n' "$1"
  fail=1
}
eq() {
  if [[ "$2" == "$3" ]]; then ok "$1 ($2)"; else bad "$1: got '$2' want '$3'"; fi
}

registry::record conductor run-1 "wave" 111 /d/r /d/r/ledger.json
registry::record phase phase-a "agent/a" 222 /d/r/a /d/r/a/status.json
registry::status phase-a running
registry::status phase-a merged

cur="$(registry::current)"
eq "two runs folded" "$(jq 'length' <<<"${cur}")" "2"
eq "phase-a last status wins" "$(jq -r '.[] | select(.id=="phase-a").status' <<<"${cur}")" "merged"
eq "phase-a pid preserved" "$(jq -r '.[] | select(.id=="phase-a").pid' <<<"${cur}")" "222"
eq "phase-a kind preserved" "$(jq -r '.[] | select(.id=="phase-a").kind' <<<"${cur}")" "phase"
eq "run-1 still running" "$(jq -r '.[] | select(.id=="run-1").status' <<<"${cur}")" "running"
eq "run-1 kind" "$(jq -r '.[] | select(.id=="run-1").kind' <<<"${cur}")" "conductor"

rm -f "${CONDUCT_REGISTRY}"
echo
if ((fail == 0)); then
  echo "registry: ALL PASS"
  exit 0
fi
echo "registry: FAILURES"
exit 1
