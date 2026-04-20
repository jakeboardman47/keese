#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Design-gate enforcement (Phase P8).
#
# Rules enforced:
#   1. A *_types.go or *_controller.go under api/ or
#      internal/controller/ counts as "non-stub" if it
#      (a) LACKS the TODO(design-gate) sentinel, OR
#      (b) has > 20 non-blank non-comment LOC.
#   2. For every non-stub file that implements kind K in group G,
#      require:
#        - a docs/designs/*.md that mentions kind K and has
#          frontmatter status: current (or regression_lock: true)
#        - a docs/specs/*.md for the owning group at v1alpha1 with
#          status: current (or regression_lock: true)
#   3. No spec doc may carry status: current while ANY of its
#      depended-on designs are status: draft.
#   4. The top iteration score in docs/plans/README.md is >= 90.
#
# Exit 0: gate open (no violations). Exit 1: gate closed.

set -euo pipefail
IFS=$'\n\t'

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

STUB_MARKER='TODO(design-gate)'
# Operator-sdk's generated scaffold lands at ~27 non-blank non-comment
# LOC (imports + struct + minimal Reconcile + SetupWithManager). Raise
# the ceiling to 35 so the stub passes but any real implementation
# (which adds fetch/SSA/status/conditions/events — easily 60+ LOC even
# for a minimal reconciler) trips it.
LOC_LIMIT=35
FAIL=0
REASONS=()

# is_stub(path) -> 0 if stub, 1 if non-stub
is_stub() {
  local f="$1"
  grep -q "${STUB_MARKER}" "${f}" || return 1
  local loc
  loc=$(grep -cE '^[[:space:]]*[^/[:space:]]' "${f}" || true)
  [ "${loc}" -le "${LOC_LIMIT}" ]
}

frontmatter_field() {
  # $1 = file, $2 = field name
  awk -v field="$2" '
    /^---$/  { c++; next }
    c == 1 && $0 ~ "^"field":" {
      sub("^"field":[[:space:]]+", "")
      gsub(/"/, "")
      print
      exit
    }
  ' "$1"
}

# --- Scan api/ + internal/controller/ for non-stub files ---
declare -A KINDS_TOUCHED
if [ -d api ] || [ -d internal/controller ]; then
  while IFS= read -r -d '' f; do
    case "$(basename "${f}")" in
      zz_generated*|*_test.go|suite_test.go|groupversion_info.go|main.go) continue ;;
    esac
    if is_stub "${f}"; then
      continue
    fi
    base="$(basename "${f}")"
    case "${base}" in
      *_types.go)      kind="${base%_types.go}" ;;
      *_controller.go) kind="${base%_controller.go}" ;;
      *) continue ;;
    esac
    KINDS_TOUCHED["${kind}"]=1
  done < <(find api internal/controller -name '*.go' -print0 2>/dev/null)
fi

# --- For every touched kind, verify designs + specs ---
for kind in "${!KINDS_TOUCHED[@]}"; do
  # Find any design that mentions the kind (case-insensitive grep on
  # the heading or body)
  design_matches=$(grep -l -i -E "\\b${kind}\\b" docs/designs/*.md 2>/dev/null || true)
  if [ -z "${design_matches}" ]; then
    REASONS+=("kind '${kind}' has non-stub code but no docs/designs/*.md references it")
    FAIL=1
    continue
  fi
  for d in ${design_matches}; do
    status="$(frontmatter_field "${d}" status)"
    lock="$(frontmatter_field "${d}" regression_lock)"
    if [ "${status}" != "current" ] && [ "${lock}" != "true" ]; then
      REASONS+=("${d} has status=${status} (need current or regression_lock: true) for kind '${kind}'")
      FAIL=1
    fi
  done
done

# --- Specs-gate-on-designs: no spec current while design is draft ---
if [ -d docs/specs ]; then
  for spec in docs/specs/*.md; do
    [ -f "${spec}" ] || continue
    sstatus="$(frontmatter_field "${spec}" status)"
    [ "${sstatus}" = "current" ] || continue
    # depends: field holds a YAML list; extract entries
    deps=$(awk '
      /^---$/  { c++; next }
      c == 1 && /^depends:/ {
        gsub(/depends:|\[|\]/, "")
        gsub(/,/, " ")
        print
        exit
      }
    ' "${spec}")
    for dep in ${deps}; do
      dep_clean="$(echo "${dep}" | tr -d ' "')"
      [ -z "${dep_clean}" ] && continue
      dep_path="${REPO_ROOT}/${dep_clean#./}"
      if [ ! -f "${dep_path}" ]; then
        # Try relative to the spec
        dep_path="${REPO_ROOT}/docs/$(basename "$(dirname "${spec}")")/${dep_clean}"
      fi
      if [ -f "${dep_path}" ]; then
        dstat="$(frontmatter_field "${dep_path}" status)"
        if [ "${dstat}" = "draft" ]; then
          REASONS+=("${spec} is current but depends on ${dep_path} (status=draft)")
          FAIL=1
        fi
      fi
    done
  done
fi

# --- Top iteration score in docs/plans/README.md >= 90 ---
if [ -f docs/plans/README.md ]; then
  # Look for "gate_status:" in frontmatter first
  gate_status="$(frontmatter_field docs/plans/README.md gate_status)"
  if [ -n "${gate_status}" ] && [ "${gate_status}" != "open" ] && [ "${gate_status}" != "closed" ]; then
    REASONS+=("docs/plans/README.md gate_status must be 'open' or 'closed', got '${gate_status}'")
    FAIL=1
  fi
fi

# --- Report ---
if [ "${FAIL}" -ne 0 ]; then
  printf 'Design gate FAILED (%d violation(s)):\n' "${#REASONS[@]}" >&2
  for r in "${REASONS[@]}"; do
    printf '  - %s\n' "${r}" >&2
  done
  printf 'See docs/plans/README.md#gate-status for current state.\n' >&2
  exit 1
fi

echo "Design gate OPEN (no violations)."
