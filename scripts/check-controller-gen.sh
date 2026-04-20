#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Fail if `make manifests generate` produces a diff — i.e., committed
# CRD/RBAC/webhook manifests are stale with respect to the Go types.

set -euo pipefail
IFS=$'\n\t'

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

# Nothing to check until api/ exists (phase P6).
if [ ! -d api ]; then
  exit 0
fi

if ! command -v controller-gen >/dev/null 2>&1; then
  echo "check-controller-gen: controller-gen missing; skipping (nix shell?)" >&2
  exit 0
fi

# Run the sdk-generated manifests+generate targets and check for drift.
if [ ! -f Makefile.operator-sdk-generated ]; then
  exit 0
fi

make -s -f Makefile.operator-sdk-generated manifests generate

if ! git diff --quiet -- config/crd config/rbac config/webhook api; then
  echo "check-controller-gen: generated manifests are out of date." >&2
  echo "  Run 'make manifests generate' and commit the result." >&2
  git diff --stat -- config/crd config/rbac config/webhook api >&2
  exit 1
fi
