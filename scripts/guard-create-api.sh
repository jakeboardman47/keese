#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Refuse `operator-sdk create api` if the kind already exists in
# PROJECT. Use:
#   ./scripts/guard-create-api.sh <group> <Kind>
# Exit 0 if the kind is not yet registered; exit 1 otherwise.

set -euo pipefail
IFS=$'\n\t'

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <group> <Kind>" >&2
  exit 2
fi
group="$1"
kind="$2"

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

if [ ! -f PROJECT ]; then
  # Project not yet initialized; safe to proceed.
  exit 0
fi

if grep -qE "^[[:space:]]+group:[[:space:]]+${group}$" PROJECT \
  && grep -qE "^[[:space:]]+kind:[[:space:]]+${kind}$" PROJECT; then
  # Both must appear in the same resource block.
  # Quick check: verify the kind line is within ~10 lines of its group line.
  awk -v g="${group}" -v k="${kind}" '
    /^- api:/ { block_start=NR }
    $0 ~ "group: "g"$" { group_found=NR }
    $0 ~ "kind: "k"$" {
      if (group_found >= block_start) { print "hit"; exit }
    }
  ' PROJECT | grep -q hit && {
    echo "guard-create-api: ${kind}.${group}.operator.keese.ai already in PROJECT — refusing re-create" >&2
    exit 1
  }
fi
exit 0
