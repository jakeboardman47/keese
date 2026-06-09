#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# EH8 step 3 (stub) — the DOWNSTREAM admission-outcome flip for
# cosign-installplan-verify, gated on the keese-cosign-webhook + OLM being
# deployed.
#
# When live, this would:
#   1. Ensure the gate is ON (override=true; re-projected to the webhook).
#   2. Create an OLM InstallPlan referencing an UNSIGNED ghcr.io/keese-ai/*
#      image and assert admission DENY with Result.Reason in
#      {BundleUnsigned, BundleNotDigestPinned} (handler.go fail-closed path).
#   3. Flip the gate OFF (override=false) and assert the same InstallPlan is
#      ADMITTED with Result.Reason=AllowedFeatureGateOff (handler.go
#      short-circuit at the top of Handle()).
#
# The local bootstrap (`make bootstrap-infra`) deploys neither OLM nor the
# webhook (config/cosign-webhook/ ships manifests but no overlay/bootstrap
# applies them), so the precondition can't be met. We detect that and SKIP
# cleanly (exit 0). Tracking trigger: revisit_when_featuregate_effect_observable.
#
# Detection (all must be present to run the live assertion):
#   - operators.coreos.com/v1alpha1 InstallPlan CRD registered, AND
#   - the keese-cosign-installplan ValidatingWebhookConfiguration exists, AND
#   - the keese-cosign-webhook Deployment is Available in keese-system.
#
# Env: KUBECTL_CONTEXT (default: current-context). Skips with no context.

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

# shellcheck source=../../../scripts/lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"

REVISIT="revisit_when_featuregate_effect_observable"
GATE="cosign-installplan-verify"

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
if [[ -z "${CONTEXT}" ]]; then
  log::warn "[admission] no kubectl context — skipping (structural validation only)"
  exit 0
fi

skip() {
  log::warn "[admission] SKIP (${GATE}) — ${1}"
  log::dim "  The CR-reconcile + projection flip (steps 0-2) is the live behavior assertion."
  log::dim "  Trigger to enable this end-to-end admission proof: ${REVISIT}"
  exit 0
}

# 1. OLM InstallPlan CRD present?
if ! kubectl --context="${CONTEXT}" get crd installplans.operators.coreos.com \
  >/dev/null 2>&1; then
  skip "OLM InstallPlan CRD (operators.coreos.com/v1alpha1) not registered"
fi

# 2. cosign webhook configuration registered?
if ! kubectl --context="${CONTEXT}" \
  get validatingwebhookconfiguration keese-cosign-installplan \
  >/dev/null 2>&1; then
  skip "keese-cosign-installplan ValidatingWebhookConfiguration not present"
fi

# 3. webhook Deployment available?
avail="$(kubectl --context="${CONTEXT}" -n keese-system \
  get deployment keese-cosign-webhook \
  -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null || true)"
if [[ "${avail}" != "True" ]]; then
  skip "keese-cosign-webhook Deployment not Available in keese-system"
fi

# ── Live path (only when the webhook + OLM are deployed). ─────────────────────
# Not exercised in the local bootstrap; implemented behind the precondition so
# the same suite runs unmodified once the webhook ships in an overlay.
log::err "[admission] live cosign-webhook detected but the admission-flip assertion"
log::err "  is not yet implemented (${REVISIT}). Failing rather than passing blind."
log::err "  Implement: create an unsigned-image InstallPlan, assert DENY (gate on)"
log::err "  → AllowedFeatureGateOff (gate off). See handler.go Handle()."
exit 1
