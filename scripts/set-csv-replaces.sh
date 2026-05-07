#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# set-csv-replaces.sh — set spec.replaces on the new CSV AND extend the
# FBC catalog index template (bundle/.config/index-template.yaml) with a
# new channel entry + bundle stanza so the replaces: chain is reflected
# in the OperatorHub File-Based Catalog.
#
# Reads the previous CSV name from bundle/.previous-csv and patches
# spec.replaces in bundle/manifests/keese.clusterserviceversion.yaml.
# After patching, writes the new CSV name to bundle/.previous-csv so the
# next release can chain correctly.
#
# Design ref: docs/designs/14b-olm-dependencies.md §CSV replaces chain (TD-P3-03)
#
# Usage:
#   scripts/set-csv-replaces.sh
#
# Environment overrides:
#   BUNDLE_CSV          path to the CSV file (default: bundle/manifests/keese.clusterserviceversion.yaml)
#   PREVIOUS_CSV_FILE   path to the previous-csv tracking file (default: bundle/.previous-csv)
#   INDEX_TEMPLATE      path to FBC index template (default: bundle/.config/index-template.yaml)

set -euo pipefail
IFS=$'\n\t'

# shellcheck source=lib/log.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/log.sh"

readonly BUNDLE_CSV="${BUNDLE_CSV:-bundle/manifests/keese.clusterserviceversion.yaml}"
readonly PREVIOUS_CSV_FILE="${PREVIOUS_CSV_FILE:-bundle/.previous-csv}"
readonly INDEX_TEMPLATE="${INDEX_TEMPLATE:-bundle/.config/index-template.yaml}"

# ── helpers ────────────────────────────────────────────────────────────────────

_check_deps() {
  local missing=0
  for cmd in yq python3; do
    if ! command -v "${cmd}" >/dev/null 2>&1; then
      log::err "required command not on PATH: ${cmd}"
      missing=1
    fi
  done
  if [[ "${missing}" -ne 0 ]]; then
    log::err "install missing tools (yq: https://github.com/mikefarah/yq)"
    exit 127
  fi
}

_read_previous_csv() {
  if [[ ! -f "${PREVIOUS_CSV_FILE}" ]]; then
    log::err "previous-csv file not found: ${PREVIOUS_CSV_FILE}"
    log::err "create it with the prior CSV name, e.g.: echo keese.v0.0.0 > bundle/.previous-csv"
    exit 1
  fi
  local prev
  prev="$(tr -d '[:space:]' < "${PREVIOUS_CSV_FILE}")"
  if [[ -z "${prev}" ]]; then
    log::err "${PREVIOUS_CSV_FILE} is empty — populate with the prior CSV name (e.g. keese.v0.0.0)"
    exit 1
  fi
  printf '%s' "${prev}"
}

_read_current_csv_name() {
  if [[ ! -f "${BUNDLE_CSV}" ]]; then
    log::err "CSV not found: ${BUNDLE_CSV}"
    exit 1
  fi
  yq e '.metadata.name' "${BUNDLE_CSV}"
}

_patch_replaces() {
  local prev="$1"
  log::info "patching spec.replaces = ${prev} in ${BUNDLE_CSV}"
  yq e -i ".spec.replaces = \"${prev}\"" "${BUNDLE_CSV}"
}

_update_previous_csv_file() {
  local current="$1"
  log::info "updating ${PREVIOUS_CSV_FILE} → ${current}"
  printf '%s\n' "${current}" > "${PREVIOUS_CSV_FILE}"
}

# ── steps ──────────────────────────────────────────────────────────────────────

step_check_deps() { _check_deps; }

step_patch_replaces() {
  local prev current
  prev="$(_read_previous_csv)"
  current="$(_read_current_csv_name)"

  log::info "current CSV : ${current}"
  log::info "previous CSV: ${prev}"

  if [[ "${prev}" == "${current}" ]]; then
    log::warn "previous CSV equals current CSV (${current}); nothing to patch"
    log::warn "bump the version in the CSV metadata before running this script"
    exit 1
  fi

  _patch_replaces "${prev}"
  log::ok "spec.replaces set to ${prev}"
}

step_advance_pointer() {
  local current
  current="$(_read_current_csv_name)"
  _update_previous_csv_file "${current}"
  log::ok "${PREVIOUS_CSV_FILE} updated to ${current}"
}

# _update_catalog_index inserts a new channel entry (with replaces:) and a
# bundle stanza for <current> CSV into the FBC index template so the
# OperatorHub catalog reflects the full replaces: chain (TD-P3-03).
_update_catalog_index() {
  local current="$1" prev="$2"
  if [[ ! -f "${INDEX_TEMPLATE}" ]]; then
    log::warn "FBC index template not found at ${INDEX_TEMPLATE} — skipping catalog update"
    return 0
  fi
  if grep -q "name: ${current}" "${INDEX_TEMPLATE}"; then
    log::info "channel entry for ${current} already present in ${INDEX_TEMPLATE} — skipping"
    return 0
  fi

  # Extract version string from CSV name (e.g. keese.v0.1.0 → v0.1.0)
  local ver="${current#keese.}"

  python3 - "${INDEX_TEMPLATE}" "${current}" "${prev}" "${ver}" <<'PYEOF'
import sys, re

tpl_path, new_csv, replaced_csv, new_ver = sys.argv[1:]

with open(tpl_path) as f:
    content = f.read()

# Append new channel entry after the last keese.v... entry in the channel block.
channel_entry = f"""      - name: {new_csv}
        replaces: {replaced_csv}"""

pattern = r'(      - name: keese\.[^\n]+(?:\n        replaces: [^\n]+)?)'
matches = list(re.finditer(pattern, content))
if not matches:
    print(f"ERROR: no keese channel entries found in {tpl_path}", file=sys.stderr)
    sys.exit(1)
last_match = matches[-1]
content = content[:last_match.end()] + "\n" + channel_entry + content[last_match.end():]

# Append bundle stanza at end of file.
bundle_stanza = f"""
  - schema: olm.bundle
    image: ghcr.io/keese-ai/keese-bundle:{new_ver}
    name: {new_csv}
    package: keese
    properties:
      - type: olm.maxOpenShiftVersion
        value: "4.99"
"""
content = content.rstrip() + "\n" + bundle_stanza

with open(tpl_path, 'w') as f:
    f.write(content)
print(f"[set-csv-replaces] catalog index updated: {new_csv} replaces: {replaced_csv}")
PYEOF

  log::ok "FBC catalog index updated with ${current} replaces: ${prev}"
}

step_update_catalog_index() {
  local current prev
  current="$(_read_current_csv_name)"
  prev="$(_read_previous_csv)"
  _update_catalog_index "${current}" "${prev}"
}

# ── main ───────────────────────────────────────────────────────────────────────

main() {
  run::step "01" "check dependencies" step_check_deps
  run::step "02" "patch spec.replaces" step_patch_replaces
  run::step "03" "advance .previous-csv pointer" step_advance_pointer
  run::step "04" "update FBC catalog index" step_update_catalog_index
  log::ok "set-csv-replaces complete"
}

main "$@"
