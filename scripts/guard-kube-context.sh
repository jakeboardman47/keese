#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Refuse to run a make target if the current kubectl context matches
# a production pattern. Enforced by rule 05.14.

set -euo pipefail
IFS=$'\n\t'

# If kubectl is not installed or not configured, there's no context to
# guard — assume safe (the target will fail itself if kubectl is
# required).
if ! command -v kubectl >/dev/null 2>&1; then
  exit 0
fi
ctx="$(kubectl config current-context 2>/dev/null || true)"
if [ -z "${ctx}" ]; then
  exit 0
fi

case "${ctx}" in
  prod-* | *production* | *prd* | *prod)
    echo "ERROR: refusing to run against kubectl context: ${ctx}" >&2
    echo "       This looks like a production context. Switch with" >&2
    echo "         kubectl config use-context kind-keese-dev" >&2
    echo "       or similar before retrying." >&2
    exit 1
    ;;
esac

exit 0
