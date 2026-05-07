#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# build-catalog.sh — render the FBC template and validate the catalog.
#
# Usage: scripts/build-catalog.sh [--skip-validate]
#
# Outputs catalog/keese/catalog.json (opm FBC JSON).
# Requires: opm (operator-package-manager) on PATH.

set -euo pipefail
IFS=$'\n\t'

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEMPLATE="${REPO_ROOT}/bundle/.config/index-template.yaml"
CATALOG_DIR="${REPO_ROOT}/catalog/keese"
OUTPUT="${CATALOG_DIR}/catalog.json"

SKIP_VALIDATE=false
for arg in "$@"; do
  if [[ "${arg}" == "--skip-validate" ]]; then
    SKIP_VALIDATE=true
  fi
done

if ! command -v opm &>/dev/null; then
  echo "ERROR: opm not found. Install via: brew install operator-sdk or download from" \
    "https://github.com/operator-framework/operator-registry/releases" >&2
  exit 1
fi

echo "[build-catalog] Rendering FBC template → ${OUTPUT}"
mkdir -p "${CATALOG_DIR}"
opm alpha render-template basic "${TEMPLATE}" > "${OUTPUT}"

echo "[build-catalog] Template rendered: $(wc -l < "${OUTPUT}") lines"

if [[ "${SKIP_VALIDATE}" == "true" ]]; then
  echo "[build-catalog] Skipping opm validate (--skip-validate set)"
else
  echo "[build-catalog] Validating catalog ..."
  opm validate "${CATALOG_DIR}"
  echo "[build-catalog] Validation passed."
fi

echo "[build-catalog] Done: ${OUTPUT}"
