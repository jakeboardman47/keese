#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# check-optional-deps.sh — verify the four hard OLM dependencies declared in
# docs/designs/14b-olm-dependencies.md are present in the CSV + dependencies.yaml.
#
# Checks:
#   1. bundle/metadata/dependencies.yaml exists and contains olm.gvk entries
#      for cert-manager, Capsule, Argo Workflows, ExternalSecrets.
#   2. bundle/manifests/keese.clusterserviceversion.yaml has a non-empty
#      spec.relatedImages list (where provider images are declared).
#
# Usage:
#   scripts/check-optional-deps.sh [--csv PATH] [--deps PATH]
#
# Environment:
#   BUNDLE_CSV     path to CSV (default: bundle/manifests/keese.clusterserviceversion.yaml)
#   BUNDLE_DEPS    path to deps file (default: bundle/metadata/dependencies.yaml)

set -euo pipefail
IFS=$'\n\t'

# shellcheck source=lib/log.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/log.sh"

readonly BUNDLE_CSV="${BUNDLE_CSV:-bundle/manifests/keese.clusterserviceversion.yaml}"
readonly BUNDLE_DEPS="${BUNDLE_DEPS:-bundle/metadata/dependencies.yaml}"

# ── Required GVK entries per 14b §OLM-dependencies.yaml ──────────────────────
# Format: "group version kind description"
readonly -a REQUIRED_GVKS=(
  "cert-manager.io v1 Certificate cert-manager"
  "capsule.clastix.io v1beta2 Tenant Capsule"
  "argoproj.io v1alpha1 Workflow argo-workflows"
  "external-secrets.io v1beta1 ExternalSecret external-secrets"
)

# ── helpers ────────────────────────────────────────────────────────────────────

_usage() {
  printf 'Usage: %s [--csv PATH] [--deps PATH]\n' "$0"
}

_check_deps_tools() {
  for cmd in grep awk; do
    if ! command -v "${cmd}" >/dev/null 2>&1; then
      log::err "required command not on PATH: ${cmd}"
      exit 127
    fi
  done
}

_check_deps_file() {
  if [[ ! -f "${BUNDLE_DEPS}" ]]; then
    log::err "dependencies.yaml not found: ${BUNDLE_DEPS}"
    log::err "run 'make bundle' to regenerate, then ensure dependencies.yaml exists"
    return 1
  fi
  log::info "found ${BUNDLE_DEPS}"
}

_check_csv_file() {
  if [[ ! -f "${BUNDLE_CSV}" ]]; then
    log::err "CSV not found: ${BUNDLE_CSV}"
    return 1
  fi
  log::info "found ${BUNDLE_CSV}"
}

_check_gvk_entry() {
  local group="$1" version="$2" kind="$3" label="$4"
  local found=0

  # Check for the group + kind pair in the deps file.
  # The file may use block-style or flow-style YAML; grep covers both.
  if grep -q "group: ${group}" "${BUNDLE_DEPS}" 2>/dev/null && \
     grep -q "kind: ${kind}" "${BUNDLE_DEPS}" 2>/dev/null; then
    found=1
  fi

  if [[ "${found}" -eq 1 ]]; then
    log::ok "GVK dep declared: ${group}/${version}/${kind} (${label})"
    return 0
  fi

  log::err "MISSING GVK dep: ${group}/${version}/${kind} (${label})"
  log::err "  Add to ${BUNDLE_DEPS}:"
  log::err "    - type: olm.gvk"
  log::err "      value:"
  log::err "        group: ${group}"
  log::err "        version: ${version}"
  log::err "        kind: ${kind}"
  return 1
}

_check_related_images() {
  # relatedImages is optional per OLM spec but 14b §supply-chain says it must
  # be non-empty for the keese CSV. Warn (not fail) if absent — operators may
  # not have set relatedImages yet during early development.
  if grep -q "relatedImages:" "${BUNDLE_CSV}" 2>/dev/null; then
    log::ok "spec.relatedImages declared in CSV"
  else
    log::warn "spec.relatedImages absent from CSV — add provider image entries"
    log::warn "  (14b §supply-chain: images must be listed for disconnected installs)"
  fi
}

# ── steps ──────────────────────────────────────────────────────────────────────

step_check_tools() { _check_deps_tools; }
step_check_files() {
  _check_deps_file && _check_csv_file
}

step_check_gvks() {
  local failures=0
  for entry in "${REQUIRED_GVKS[@]}"; do
    # shellcheck disable=SC2206
    local parts=(${entry})
    if ! _check_gvk_entry "${parts[0]}" "${parts[1]}" "${parts[2]}" "${parts[3]}"; then
      failures=$((failures + 1))
    fi
  done
  if [[ "${failures}" -gt 0 ]]; then
    log::err "${failures} required GVK dep(s) missing from ${BUNDLE_DEPS}"
    return 1
  fi
  log::ok "all four OLM GVK deps declared"
}

step_check_related_images() { _check_related_images; }

# ── main ───────────────────────────────────────────────────────────────────────

main() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --csv)  BUNDLE_CSV="$2"; shift ;;
      --deps) BUNDLE_DEPS="$2"; shift ;;
      -h|--help) _usage; exit 0 ;;
      *) log::err "unknown argument: $1"; _usage >&2; exit 64 ;;
    esac
    shift
  done

  run::step "01" "check tool dependencies" step_check_tools
  run::step "02" "check bundle files exist" step_check_files
  run::step "03" "check required OLM GVK deps" step_check_gvks
  run::step "04" "check spec.relatedImages" step_check_related_images
  log::ok "check-optional-deps complete"
}

main "$@"
