#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Verify no template placeholders (e.g., {{PROJECT_NAME}}) remain in the
# tree after initial substitution. Fail with a pointer listing if any
# placeholder survives.

set -euo pipefail
IFS=$'\n\t'

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/log.sh
source "${HERE}/lib/log.sh"

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

# Placeholders are ALL-CAPS identifiers wrapped in double curly braces.
# Exclude the plans/ and .git/ directories (plans can legitimately reference
# placeholder examples when documenting template conventions).
pattern='\{\{[A-Z_]+\}\}'

if grep -rn --exclude-dir=.git --exclude-dir=docs/plans "${pattern}" . > /tmp/keese-placeholder-hits.txt 2>/dev/null; then
  log::err "template placeholders still present:"
  sed 's/^/  /' /tmp/keese-placeholder-hits.txt >&2
  exit 1
fi

log::ok "no remaining template placeholders"
