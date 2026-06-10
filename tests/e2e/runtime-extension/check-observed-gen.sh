#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# EH11 runtime-extension: assert status.observedGeneration ==
# metadata.generation on the Ready extension (rule 04.4 — the controller
# sets ObservedGeneration = Generation on the Ready converge). kuttl's
# declarative assert cannot compare two live fields, so this command step
# reads both via jsonpath and compares.
#
# No sleep-as-assertion: 01-assert.yaml has already gated rext-e2e to Ready
# before this step runs, so the fields are populated; this is a one-shot
# read. Fail-closed on mismatch.
#
# Overrides: KUBECTL_CONTEXT, EXT_NS, EXT_NAME.

set -euo pipefail
IFS=$'\n\t'

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
EXT_NS="${EXT_NS:-runtime-extension-e2e}"
EXT_NAME="${EXT_NAME:-rext-e2e}"

if [[ -z "${CONTEXT}" ]]; then
  echo "[observed-gen] no kubectl context — skipping" >&2
  exit 0
fi

GEN="$(kubectl --context="${CONTEXT}" -n "${EXT_NS}" \
  get runtimeextension "${EXT_NAME}" -o jsonpath='{.metadata.generation}' 2>/dev/null || true)"
OBS="$(kubectl --context="${CONTEXT}" -n "${EXT_NS}" \
  get runtimeextension "${EXT_NAME}" -o jsonpath='{.status.observedGeneration}' 2>/dev/null || true)"

if [[ -z "${GEN}" || -z "${OBS}" ]]; then
  echo "[observed-gen] FAIL: generation=${GEN:-<none>} observedGeneration=${OBS:-<none>}" >&2
  exit 1
fi
if [[ "${GEN}" != "${OBS}" ]]; then
  echo "[observed-gen] FAIL: observedGeneration ${OBS} != generation ${GEN}" >&2
  exit 1
fi

echo "[observed-gen] OK: observedGeneration=${OBS}==generation for ${EXT_NS}/${EXT_NAME}"
