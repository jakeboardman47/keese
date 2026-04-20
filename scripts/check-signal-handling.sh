#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Enforce rule 06.11: every cmd/**/main.go installs a SIGTERM handler.
# Looks for `signal.NotifyContext` or `signal.Notify` with SIGTERM in
# the arglist; accepts either.

set -euo pipefail
IFS=$'\n\t'

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

if [ ! -d cmd ]; then
  exit 0
fi

failed=0
while IFS= read -r -d '' f; do
  # Accept any of:
  #   signal.Notify(*, syscall.SIGTERM)
  #   signal.NotifyContext(*, syscall.SIGTERM)
  #   ctrl.SetupSignalHandler()            # controller-runtime: installs SIGTERM+SIGINT
  #   signals.SetupSignalHandler()         # alternate import alias
  if grep -qE 'signal\.(Notify|NotifyContext)[^)]*SIGTERM' "${f}" \
    || grep -qE '(ctrl|signals)\.SetupSignalHandler\(\)' "${f}"; then
    continue
  fi
  echo "check-signal-handling: ${f} is missing a SIGTERM handler (signal.Notify or ctrl.SetupSignalHandler)" >&2
  failed=1
done < <(find cmd -name 'main.go' -print0)

exit "${failed}"
