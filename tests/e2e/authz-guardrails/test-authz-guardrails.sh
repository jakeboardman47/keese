#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# E2E: GuardrailBinding reconcile + default-inherit + status/events (EH5,
# deliverables 1 + 3). Asserts the SHIPPED GuardrailBinding controller
# (internal/controller/authz/guardrailbinding_controller.go) behaviour
# against the live cluster:
#
#   1. STATUS      — eh5-tenant + eh5-workspace reach Phase=Ready /
#                    Ready=True with status.observedGeneration ==
#                    metadata.generation and a non-nil effectivePolicy
#                    (rule 04.4 — observedGeneration on every status).
#   2. INHERIT     — default-inherit (design 06): the workspace binding
#                    inherits the tenant binding (ParentReadable=True) and
#                    the merged effectivePolicy.deny is the UNION of the
#                    cluster default + tenant + workspace deny sets
#                    (strictest-wins). We assert the workspace-level deny
#                    (browser.navigate) AND the tenant-level deny (net.raw)
#                    both appear in the workspace binding's effectivePolicy
#                    — i.e. the parent's restriction was inherited, not
#                    dropped.
#   3. EVENTS      — the controller emits its finite-table event reasons
#                    (rule 04.11): BindingMerged + EffectivePolicyComputed
#                    on the happy path. We assert these reasons appear on
#                    the binding's event stream (NOT free-text).
#
# Status/conditions are observable via `kubectl get`, so unlike the
# request-path tests this script asserts purely against the API server —
# no gateway request firing here. The tool allow/deny request path lives
# in 02-tool-allow-deny.yaml -> test-tool-allow-deny.sh.
#
# Prereq-gated by ../lib/check-prereqs.sh (run from the kuttl step). Per
# rule 06 this is the e2e tier; run via the kuttl suite.
#
# Usage: tests/e2e/authz-guardrails/test-authz-guardrails.sh
#        KUBECTL_CONTEXT=kind-keese-demo tests/e2e/authz-guardrails/test-authz-guardrails.sh
#
# Refs: internal/controller/authz/guardrailbinding_controller.go (reconcile)
#       internal/controller/authz/guardrail_events.go            (event reasons)
#       internal/controller/authz/guardrail_merge.go             (strictest-wins)
#       docs/designs/06-guardrailbinding.md                      (default-inherit)

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

# shellcheck source=../../../scripts/lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"

CONTEXT="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
NS="${WORKSPACE_NS:-alpha}"

TENANT_BINDING="${TENANT_BINDING:-eh5-tenant}"
WS_BINDING="${WS_BINDING:-eh5-workspace}"

# Tenant- and workspace-level deny entries seeded by guardrails.yaml. The
# inherit assertion proves the tenant deny survives the merge into the
# workspace binding's effectivePolicy.
TENANT_DENY="${TENANT_DENY:-net.raw}"
WS_DENY="${WS_DENY:-browser.navigate}"

FAILURES=0

log::info "test-authz-guardrails: cluster=${CONTEXT} ns=${NS}"

kbinding() {
  # $1 = binding name, $2 = jsonpath (without surrounding braces)
  kubectl --context="${CONTEXT}" -n "${NS}" \
    get guardrailbinding "$1" -o jsonpath="{$2}" 2>/dev/null || true
}

# ── 1. STATUS: observedGeneration + Ready + effectivePolicy ──────────────────
assert_status_settled() {
  local case_id="$1" name="$2"

  local gen obsgen phase ready ep
  gen="$(kbinding "${name}" '.metadata.generation')"
  obsgen="$(kbinding "${name}" '.status.observedGeneration')"
  phase="$(kbinding "${name}" '.status.phase')"
  ready="$(kbinding "${name}" '.status.conditions[?(@.type=="Ready")].status')"
  ep="$(kbinding "${name}" '.status.effectivePolicy.observedGeneration')"

  local fail=0
  if [[ "${phase}" != "Ready" ]]; then
    log::err "[${case_id}] ${name}: phase=${phase:-<none>} (expected Ready)"
    fail=1
  fi
  if [[ "${ready}" != "True" ]]; then
    log::err "[${case_id}] ${name}: Ready=${ready:-<none>} (expected True)"
    fail=1
  fi
  # rule 04.4: observedGeneration must track metadata.generation.
  if [[ -z "${obsgen}" || "${obsgen}" != "${gen}" ]]; then
    log::err "[${case_id}] ${name}: observedGeneration=${obsgen:-<none>} != generation=${gen:-<none>}"
    fail=1
  fi
  # effectivePolicy must be written, with its own observedGeneration stamp.
  if [[ -z "${ep}" || "${ep}" != "${gen}" ]]; then
    log::err "[${case_id}] ${name}: effectivePolicy.observedGeneration=${ep:-<none>} != generation=${gen:-<none>}"
    fail=1
  fi

  if [[ "${fail}" -eq 0 ]]; then
    log::ok "[${case_id}] ${name}: Phase=Ready, Ready=True, observedGeneration=${obsgen}==gen, effectivePolicy stamped"
    return 0
  fi
  return 1
}

assert_status_settled "status-tenant" "${TENANT_BINDING}" || FAILURES=$((FAILURES + 1))
assert_status_settled "status-workspace" "${WS_BINDING}" || FAILURES=$((FAILURES + 1))

# ── 2. INHERIT: default-inherit / strictest-wins ─────────────────────────────
#
# The workspace binding inherits the tenant binding. Assert:
#   (a) ParentReadable=True on the workspace binding (parent resolved), and
#   (b) the merged effectivePolicy.deny on the workspace binding contains
#       BOTH the workspace-local deny AND the inherited tenant deny — i.e.
#       the parent restriction was merged in, not dropped (design 06 union).
assert_inherit() {
  local case_id="$1"
  local parent_readable deny_json
  parent_readable="$(kbinding "${WS_BINDING}" '.status.conditions[?(@.type=="ParentReadable")].status')"
  deny_json="$(kbinding "${WS_BINDING}" '.status.effectivePolicy.tools.deny')"

  local fail=0
  if [[ "${parent_readable}" != "True" ]]; then
    log::err "[${case_id}] ${WS_BINDING}: ParentReadable=${parent_readable:-<none>} (expected True)"
    fail=1
  fi
  # Workspace-local deny must be present.
  if ! grep -q "${WS_DENY}" <<<"${deny_json}"; then
    log::err "[${case_id}] workspace deny '${WS_DENY}' missing from effectivePolicy.deny: ${deny_json:-<empty>}"
    fail=1
  fi
  # Inherited tenant deny must survive the merge (the load-bearing invariant).
  if ! grep -q "${TENANT_DENY}" <<<"${deny_json}"; then
    log::err "[${case_id}] inherited tenant deny '${TENANT_DENY}' missing from effectivePolicy.deny: ${deny_json:-<empty>}"
    log::err "[${case_id}] default-inherit BROKEN: parent restriction was dropped in the merge"
    fail=1
  fi

  if [[ "${fail}" -eq 0 ]]; then
    log::ok "[${case_id}] default-inherit holds: ParentReadable=True; deny=union(tenant '${TENANT_DENY}', workspace '${WS_DENY}')"
    return 0
  fi
  return 1
}

assert_inherit "default-inherit" || FAILURES=$((FAILURES + 1))

# ── 3. EVENTS: finite-table reasons (rule 04.11) ─────────────────────────────
#
# The happy path emits BindingMerged + EffectivePolicyComputed. We read the
# binding's involvedObject events and assert these reasons are present
# (proving the controller uses the constant table, not free text).
binding_event_reasons() {
  local name="$1"
  kubectl --context="${CONTEXT}" -n "${NS}" get events \
    --field-selector "involvedObject.kind=GuardrailBinding,involvedObject.name=${name}" \
    -o jsonpath='{range .items[*]}{.reason}{"\n"}{end}' 2>/dev/null || true
}

assert_events() {
  local case_id="$1" name="$2"
  local reasons fail=0
  reasons="$(binding_event_reasons "${name}")"
  for want in BindingMerged EffectivePolicyComputed; do
    if ! grep -qx "${want}" <<<"${reasons}"; then
      log::err "[${case_id}] ${name}: missing event reason '${want}' (got: $(tr '\n' ',' <<<"${reasons}"))"
      fail=1
    fi
  done
  if [[ "${fail}" -eq 0 ]]; then
    log::ok "[${case_id}] ${name}: emitted finite-table reasons BindingMerged + EffectivePolicyComputed"
    return 0
  fi
  return 1
}

assert_events "events-tenant" "${TENANT_BINDING}" || FAILURES=$((FAILURES + 1))
assert_events "events-workspace" "${WS_BINDING}" || FAILURES=$((FAILURES + 1))

if [[ "${FAILURES}" -gt 0 ]]; then
  log::err "test-authz-guardrails: ${FAILURES} assertion group(s) failed"
  exit 1
fi

log::ok "test-authz-guardrails: status + default-inherit + event-reason assertions passed"
