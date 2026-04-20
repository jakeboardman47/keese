#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Run `tofu validate` in each deploy/opentofu/ module. Uses
# `-backend=false` so `tofu init` does not contact a remote state.

set -euo pipefail
IFS=$'\n\t'

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

if [ ! -d deploy/opentofu ]; then
  exit 0
fi

if ! command -v tofu >/dev/null 2>&1; then
  echo "check-tofu-validate: tofu missing; skipping" >&2
  exit 0
fi

failed=0
for mod in deploy/opentofu/*/; do
  [ -d "${mod}" ] || continue
  # Only run if the module has any .tf/.tofu sources.
  if ! ls "${mod}"*.tf "${mod}"*.tofu >/dev/null 2>&1; then
    continue
  fi
  (
    cd "${mod}"
    tofu init -backend=false -input=false >/dev/null
    tofu validate
  ) || failed=1
done

exit "${failed}"
