#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Dry-run build every kustomization under config/. Fails on any build
# error.

set -euo pipefail
IFS=$'\n\t'

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

if ! command -v kustomize >/dev/null 2>&1; then
  echo "check-kustomize-overlays: kustomize missing; skipping" >&2
  exit 0
fi

if [ ! -d config ]; then
  exit 0
fi

failed=0
while IFS= read -r -d '' dir; do
  if kustomize build "${dir}" >/dev/null 2>&1; then
    :
  else
    echo "check-kustomize-overlays: ${dir} failed to build" >&2
    kustomize build "${dir}" 2>&1 | sed 's/^/  /' >&2 || true
    failed=1
  fi
done < <(find config \( -name kustomization.yaml -o -name kustomization.yml \) -print0)

exit "${failed}"
