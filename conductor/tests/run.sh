#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Run every conductor shell test (conductor/tests/*.sh except this runner).
# Used by `make conductor-test`. Does NOT set -e: a failing test must not abort
# the suite — each is captured and the runner exits non-zero if any failed.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fail=0

for t in "${HERE}"/*.sh; do
  name="$(basename "${t}")"
  [[ "${name}" == "run.sh" ]] && continue
  echo "── ${name} ───────────────────────────────"
  if bash "${t}"; then
    echo "✓ ${name}"
  else
    echo "✗ ${name}"
    fail=1
  fi
  echo
done

if ((fail == 0)); then
  echo "conductor tests: ALL GREEN"
  exit 0
fi
echo "conductor tests: FAILURES"
exit 1
