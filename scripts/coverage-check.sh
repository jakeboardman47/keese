#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Per-package coverage gate (rule 06-testing §Coverage).
#
# Runs `go test -short -coverprofile=<tmp> ./...`, computes statement-weighted
# line coverage per package, compares each against the floor declared in
# test/coverage-targets.yaml, prints a table, and exits non-zero listing every
# package below its target. A package with no target entry is informational
# (warn), never a failure. Idempotent; safe to re-run.
#
# CI-invocable: no interactive prompts, deterministic exit codes. KUBEBUILDER_
# ASSETS is NOT required (-short skips envtest-backed integration suites).
#
# Coverage measurement does not need the race detector, so this script defaults
# CGO_ENABLED=0 for portability (the macOS nix clang/SDK combo fails to link
# crypto/x509 system verification under CGO). Override by exporting CGO_ENABLED.

set -euo pipefail
IFS=$'\n\t'

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

# shellcheck source=scripts/lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"

TARGETS_FILE="${COVERAGE_TARGETS:-${REPO_ROOT}/test/coverage-targets.yaml}"

# Packages excluded from the coverage run. test/e2e is a kuttl/ginkgo suite that
# requires a live cluster in BeforeSuite; it has no unit coverage and would fail
# the -short test run for infrastructure reasons unrelated to coverage.
COVERAGE_EXCLUDE_REGEX="${COVERAGE_EXCLUDE_REGEX:-/test/e2e$}"

# Temp coverage profile; script-global so the EXIT trap can clean it up after
# main() returns (a `local` would be out of scope under `set -u`).
PROFILE=""
cleanup() { [[ -n "${PROFILE}" ]] && rm -f "${PROFILE}"; }
trap cleanup EXIT

require() {
  local tool="$1"
  if ! command -v "${tool}" >/dev/null 2>&1; then
    log::err "required tool not found on PATH: ${tool}"
    exit 1
  fi
}

main() {
  require go
  require yq

  if [[ ! -f "${TARGETS_FILE}" ]]; then
    log::err "coverage targets file not found: ${TARGETS_FILE}"
    exit 1
  fi

  # Template ends in X's: BSD (macOS) mktemp rejects a suffix after them, so no
  # .out extension here. `go tool cover` reads the profile regardless of name.
  PROFILE="$(mktemp "${TMPDIR:-/tmp}/keese-cov.XXXXXX")"
  local profile="${PROFILE}"

  # Enumerate the packages to cover, dropping infra-only suites. One import path
  # per array element so word-splitting on paths is impossible.
  local pkgs=()
  local line
  while IFS= read -r line; do
    [[ -n "${line}" ]] && pkgs+=("${line}")
  done < <(go list ./... | grep -Ev "${COVERAGE_EXCLUDE_REGEX}")

  if [[ "${#pkgs[@]}" -eq 0 ]]; then
    log::err "go list ./... produced no packages"
    exit 1
  fi

  log::info "running go test -short -coverprofile (CGO_ENABLED=${CGO_ENABLED:-0})"
  # Default per-package coverage (no -coverpkg): each package is measured
  # against its OWN statements, matching the seeded floors. A failing package
  # (e.g. a real test failure) must surface, but we still want the partial
  # profile for packages that did build; capture the rc and treat it as fatal
  # only if no profile was produced at all.
  local test_rc=0
  CGO_ENABLED="${CGO_ENABLED:-0}" go test -short \
    -coverprofile="${profile}" \
    "${pkgs[@]}" >/dev/null 2>&1 || test_rc=$?

  if [[ ! -s "${profile}" ]]; then
    log::err "no coverage profile produced (go test rc=${test_rc})"
    exit 1
  fi
  if [[ "${test_rc}" -ne 0 ]]; then
    log::warn "go test exited ${test_rc}; evaluating coverage from the profile produced"
  fi

  evaluate "${profile}"
}

# evaluate <profile>
# Aggregates the coverprofile into statement-weighted per-package coverage and
# compares each against test/coverage-targets.yaml.
evaluate() {
  local profile="$1"

  # Validate the profile is well-formed (this is the `go tool cover -func`
  # the gate is specified in terms of); its per-function detail is also handy
  # for a human debugging a FAIL row.
  if ! go tool cover -func="${profile}" >/dev/null 2>&1; then
    log::err "go tool cover -func failed on the profile"
    exit 1
  fi

  # measured: "pkg<TAB>pct" sorted by pkg. Statement-weighted (covered/total),
  # the canonical definition `go tool cover` reports as a package total.
  local measured
  measured="$(
    tail -n +2 "${profile}" | awk '
      {
        # block line: file:start.col,end.col numstmts count
        split($1, a, ":"); file = a[1]
        m = split(file, parts, "/")
        pkg = ""
        for (i = 1; i < m; i++) pkg = pkg parts[i] (i < m - 1 ? "/" : "")
        total[pkg] += $2
        if ($3 + 0 > 0) covered[pkg] += $2
      }
      END {
        for (p in total) {
          pc = (total[p] > 0) ? 100 * covered[p] / total[p] : 0
          printf "%s\t%.1f\n", p, pc
        }
      }
    ' | sort
  )"

  printf '\n%-58s %8s %8s  %s\n' "PACKAGE" "COVERED" "TARGET" "STATUS"
  printf '%s\n' "------------------------------------------------------------------------------------"

  local failures=0 warns=0
  local pkg pct target status
  while IFS=$'\t' read -r pkg pct; do
    [[ -z "${pkg}" ]] && continue
    target="$(yq -r ".targets[\"${pkg}\"] // \"\"" "${TARGETS_FILE}")"

    if [[ -z "${target}" || "${target}" == "null" ]]; then
      status="warn (no target)"
      warns=$((warns + 1))
      printf '%-58s %7s%% %8s  %s\n' "${pkg}" "${pct}" "-" "${status}"
      continue
    fi

    if awk -v c="${pct}" -v t="${target}" 'BEGIN { exit !(c + 0 < t + 0) }'; then
      status="FAIL"
      failures=$((failures + 1))
    else
      status="ok"
    fi
    printf '%-58s %7s%% %7s%%  %s\n' "${pkg}" "${pct}" "${target}" "${status}"
  done <<<"${measured}"

  printf '%s\n' "------------------------------------------------------------------------------------"

  if [[ "${warns}" -gt 0 ]]; then
    log::warn "${warns} package(s) have no coverage target (informational)"
  fi

  if [[ "${failures}" -gt 0 ]]; then
    log::err "${failures} package(s) below coverage target — see FAIL rows above"
    exit 1
  fi

  log::ok "all packages meet their coverage targets"
}

main "$@"
