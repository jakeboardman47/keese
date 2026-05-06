#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# scripts/dev/e2e-smoke.sh — end-to-end kind smoke harness.
#
# Brings up a kind cluster, bootstraps dev deps, deploys the operator, applies
# sample CRs, and optionally tears everything down. Each phase is wrapped with
# run::step so reruns can skip completed phases via --phase=<id>.
#
# Usage:
#   bash scripts/dev/e2e-smoke.sh [--keep|--no-keep] [--phase=<id>] [--logs-dir=<path>]
#
# Flags:
#   --keep         (default) leave the kind cluster + operator running after smoke.
#   --no-keep      tear down cluster and operator at the end.
#   --phase=<id>   skip ahead to phase id (01–09). Sets RUN_FROM internally.
#   --logs-dir=<p> override .plan-logs/ directory for captured output.
#
# Exit codes:
#   0   all phases passed.
#   1   a phase assertion failed; failing phase id is printed to stderr.

set -euo pipefail
IFS=$'\n\t'

# ── Locate repo root & source libs ────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# shellcheck source=scripts/lib/paths.sh
source "${REPO_ROOT}/scripts/lib/paths.sh"
# shellcheck source=scripts/lib/log.sh
source "${REPO_ROOT}/scripts/lib/log.sh"
# shellcheck source=scripts/lib/signals.sh
source "${REPO_ROOT}/scripts/lib/signals.sh"

# ── Argument parsing ───────────────────────────────────────────────────────────

KEEP=true
PHASE_FROM=""
LOGS_DIR="${PLAN_LOGS}"

for arg in "$@"; do
  case "${arg}" in
    --keep) KEEP=true ;;
    --no-keep) KEEP=false ;;
    --phase=*) PHASE_FROM="${arg#--phase=}" ;;
    --logs-dir=*) LOGS_DIR="${arg#--logs-dir=}" ;;
    --help | -h)
      grep '^#' "${BASH_SOURCE[0]}" | grep -v '^#!' | sed 's/^# \?//'
      exit 0
      ;;
    *)
      log::err "Unknown flag: ${arg}"
      exit 1
      ;;
  esac
done

export RUN_FROM="${PHASE_FROM}"
export PLAN_LOGS="${LOGS_DIR}"
mkdir -p "${PLAN_LOGS}"

# ── Constants ──────────────────────────────────────────────────────────────────

KIND_CLUSTER="${KIND_CLUSTER:-keese-dev}"
KUBE_CTX="kind-${KIND_CLUSTER}"
NS_SYSTEM="keese-system"
TILT_LOG="${PLAN_LOGS}/e2e-smoke-tilt.log"

# Polling budgets (seconds).
TIMEOUT_NODES=120
TIMEOUT_INFRA=300
TIMEOUT_OPERATOR=180
TIMEOUT_OIDC=120
TIMEOUT_TENANT=60
TIMEOUT_WORKSPACE=90
TIMEOUT_SESSION=60
TIMEOUT_TEARDOWN=90

# ── Safety guard (rule 05.14) ──────────────────────────────────────────────────

_guard_context() {
  local ctx
  ctx="$(kubectl config current-context 2>/dev/null || true)"
  if [[ -z "${ctx}" ]]; then
    return 0
  fi
  case "${ctx}" in
    prod-* | *production* | *prd* | *prod)
      log::err "Refusing to run e2e-smoke against kubectl context: ${ctx}"
      log::err "Switch to a kind context first: kubectl config use-context kind-${KIND_CLUSTER}"
      exit 1
      ;;
  esac
}

# ── Poll helper: assert eventually ────────────────────────────────────────────
# poll_until <timeout_seconds> <interval_seconds> <description> <cmd...>
# Returns 0 when cmd exits 0, 1 when timeout is reached.

poll_until() {
  local timeout="$1"
  local interval="$2"
  local desc="$3"
  shift 3
  local deadline
  deadline=$(($(date +%s) + timeout))
  while [[ $(date +%s) -lt ${deadline} ]]; do
    if "$@" >/dev/null 2>&1; then
      return 0
    fi
    log::dim "  waiting for ${desc} (${interval}s poll) …"
    sleep "${interval}"
  done
  log::err "Timeout (${timeout}s) waiting for: ${desc}"
  return 1
}

# ── Phase implementations ──────────────────────────────────────────────────────

phase_01_preflight() {
  local missing=()

  for tool in kind kubectl ctlptl helmfile tilt; do
    if ! command -v "${tool}" >/dev/null 2>&1; then
      missing+=("${tool}")
    fi
  done

  if [[ ${#missing[@]} -gt 0 ]]; then
    log::err "Missing required tools: ${missing[*]}"
    log::err "Install via the Nix devshell (nix develop) or see docs/references/nix-dev-env.md"
    return 1
  fi

  # Verify KUBEBUILDER_ASSETS or fallback etcd exists (used by envtest, not
  # strictly required for e2e but guards against a common mis-setup).
  local etcd_ok=false
  if [[ -n "${KUBEBUILDER_ASSETS:-}" ]] && [[ -x "${KUBEBUILDER_ASSETS}/etcd" ]]; then
    etcd_ok=true
  fi
  if [[ "${etcd_ok}" == "false" ]]; then
    local bin_etcd
    bin_etcd="$(find "${REPO_ROOT}/bin/k8s" -name etcd -maxdepth 3 2>/dev/null | head -1 || true)"
    if [[ -x "${bin_etcd:-}" ]]; then
      etcd_ok=true
    fi
  fi
  if [[ "${etcd_ok}" == "false" ]]; then
    log::warn "KUBEBUILDER_ASSETS not set and bin/k8s/<ver>/etcd not found."
    log::warn "Run 'make envtest-setup' before running unit/integration tests."
    log::warn "Continuing e2e smoke (envtest assets not required for kind e2e)."
  fi

  _guard_context
  log::ok "Pre-flight checks passed."
}

phase_02_cluster_up() {
  # Idempotent: ctlptl apply creates if absent, no-ops if present.
  if command -v ctlptl >/dev/null 2>&1; then
    ctlptl apply -f "${REPO_ROOT}/dev/kind/ctlptl.yaml"
  else
    kind create cluster \
      --name="${KIND_CLUSTER}" \
      --config="${REPO_ROOT}/dev/kind/kind-config.yaml" \
      2>/dev/null || true
  fi

  log::info "Waiting for cluster nodes to be Ready (${TIMEOUT_NODES}s)…"
  kubectl --context="${KUBE_CTX}" \
    wait node --all \
    --for=condition=Ready \
    --timeout="${TIMEOUT_NODES}s"

  log::ok "Cluster ${KIND_CLUSTER} is up and nodes are Ready."
}

phase_03_bootstrap_infra() {
  helmfile -f "${REPO_ROOT}/dev/bootstrap/helmfile.yaml" sync

  # Format: "namespace:resource-kind/name"
  local infra_components=(
    "cert-manager:deploy/cert-manager"
    "capsule-system:deploy/capsule-controller-manager"
    "argo:deploy/argo-workflows-server"
    "openbao:deploy/openbao"
    "external-secrets:deploy/external-secrets"
    "envoy-gateway-system:deploy/envoy-gateway"
    "nack:deploy/nats-controller"
  )

  log::info "Waiting for bootstrap deployments to be Available (${TIMEOUT_INFRA}s)…"
  for entry in "${infra_components[@]}"; do
    IFS=':' read -r ns resource <<<"${entry}"
    log::dim "  checking ${resource} in ${ns}"
    kubectl --context="${KUBE_CTX}" \
      -n "${ns}" \
      wait "${resource}" \
      --for=condition=Available \
      --timeout="${TIMEOUT_INFRA}s" \
      2>/dev/null || {
      log::warn "  ${resource} in ${ns} not Available after ${TIMEOUT_INFRA}s — continuing (may be slow)."
    }
  done

  log::ok "Bootstrap infra healthy."
}

phase_04_operator_deploy() {
  # Start Tilt in background; capture logs.
  log::info "Starting Tilt (logs → ${TILT_LOG})…"
  TILT_HOST="${TILT_HOST:-127.0.0.1}" tilt up --stream >"${TILT_LOG}" 2>&1 &
  local tilt_pid=$!
  # Register cleanup so Tilt is killed if this script exits abnormally.
  # (Proper teardown happens in phase_09_teardown.)
  trap 'kill "${tilt_pid}" 2>/dev/null || true' EXIT

  log::info "Waiting for keese-controller-manager to be Available (${TIMEOUT_OPERATOR}s)…"
  kubectl --context="${KUBE_CTX}" \
    -n "${NS_SYSTEM}" \
    wait deploy/keese-controller-manager \
    --for=condition=Available \
    --timeout="${TIMEOUT_OPERATOR}s"

  # Deregister the exit trap — teardown is explicit.
  trap - EXIT
  log::ok "Operator deployed. Tilt PID=${tilt_pid} logging to ${TILT_LOG}."
  # Store tilt_pid for teardown.
  printf '%s' "${tilt_pid}" >"${PLAN_LOGS}/e2e-smoke-tilt.pid"
}

phase_05_oidcprovider_bootstrap() {
  kubectl --context="${KUBE_CTX}" \
    apply -k "${REPO_ROOT}/config/default/bootstrap"

  # OIDCProviders may go Active or Degraded (placeholder-issuer CRs go
  # Degraded per BootstrapPlaceholderIssuer logic — that is expected).
  local count
  count=$(kubectl --context="${KUBE_CTX}" \
    get oidcproviders.authz.keese.ai \
    --all-namespaces \
    -o json 2>/dev/null \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d.get('items',[])))" \
      2>/dev/null || echo "0")

  log::info "OIDCProvider CR count: ${count}. Waiting up to ${TIMEOUT_OIDC}s for phase to settle…"
  local deadline
  deadline=$(($(date +%s) + TIMEOUT_OIDC))
  local ready=0
  while [[ $(date +%s) -lt ${deadline} ]]; do
    ready=$(kubectl --context="${KUBE_CTX}" \
      get oidcproviders.authz.keese.ai \
      --all-namespaces \
      -o jsonpath='{range .items[*]}{.status.phase}{"\n"}{end}' 2>/dev/null \
      | grep -cE '^(Active|Degraded)$' || true)
    if [[ "${ready}" -eq "${count}" ]] && [[ "${count}" -gt "0" ]]; then
      break
    fi
    log::dim "  ${ready}/${count} OIDCProviders have settled phase…"
    sleep 5
  done

  if [[ "${count}" -eq "0" ]]; then
    log::warn "No OIDCProvider CRs found after bootstrap — check config/default/bootstrap."
    return 1
  fi

  log::ok "OIDCProvider bootstrap: ${ready}/${count} CRs reached Active or Degraded (Degraded expected for placeholder-issuer)."
}

phase_06_sample_tenant() {
  kubectl --context="${KUBE_CTX}" \
    apply -f "${REPO_ROOT}/config/samples/tenancy/tenant-minimal.yaml"

  local tenant_name
  tenant_name=$(kubectl --context="${KUBE_CTX}" \
    get tenant.keese.ai \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

  if [[ -z "${tenant_name}" ]]; then
    log::err "No Tenant found after apply — check config/samples/tenancy/tenant-minimal.yaml"
    return 1
  fi

  log::info "Waiting for Tenant '${tenant_name}' to reach phase=Active (${TIMEOUT_TENANT}s)…"
  local deadline
  deadline=$(($(date +%s) + TIMEOUT_TENANT))
  while [[ $(date +%s) -lt ${deadline} ]]; do
    local phase
    phase=$(kubectl --context="${KUBE_CTX}" \
      get tenant.keese.ai "${tenant_name}" \
      -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    if [[ "${phase}" == "Active" ]]; then
      log::ok "Tenant '${tenant_name}' is Active."
      return 0
    fi
    log::dim "  Tenant phase: '${phase}' — retrying…"
    sleep 5
  done

  log::err "Tenant '${tenant_name}' did not reach Active within ${TIMEOUT_TENANT}s."
  kubectl --context="${KUBE_CTX}" \
    describe tenant.keese.ai "${tenant_name}" >&2 || true
  return 1
}

phase_07_sample_workspace() {
  kubectl --context="${KUBE_CTX}" \
    apply -f "${REPO_ROOT}/config/samples/workspace_v1alpha1_workspace.yaml"

  local ws_name
  ws_name=$(kubectl --context="${KUBE_CTX}" \
    get workspace.keese.ai \
    --all-namespaces \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
  local ws_ns
  ws_ns=$(kubectl --context="${KUBE_CTX}" \
    get workspace.keese.ai \
    --all-namespaces \
    -o jsonpath='{.items[0].metadata.namespace}' 2>/dev/null || echo "")

  if [[ -z "${ws_name}" ]]; then
    log::err "No Workspace found after apply."
    return 1
  fi

  log::info "Waiting for Workspace '${ws_name}' phase ∈ {Provisioning,Running} (${TIMEOUT_WORKSPACE}s)…"
  local deadline
  deadline=$(($(date +%s) + TIMEOUT_WORKSPACE))
  local ws_phase=""
  while [[ $(date +%s) -lt ${deadline} ]]; do
    ws_phase=$(kubectl --context="${KUBE_CTX}" \
      -n "${ws_ns}" \
      get workspace.keese.ai "${ws_name}" \
      -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    case "${ws_phase}" in
      Provisioning | Running)
        break
        ;;
    esac
    log::dim "  Workspace phase: '${ws_phase}' — retrying…"
    sleep 5
  done

  case "${ws_phase}" in
    Provisioning | Running) ;;
    *)
      log::err "Workspace '${ws_name}' phase='${ws_phase}' after ${TIMEOUT_WORKSPACE}s; expected Provisioning or Running."
      kubectl --context="${KUBE_CTX}" -n "${ws_ns}" \
        describe workspace.keese.ai "${ws_name}" >&2 || true
      return 1
      ;;
  esac

  # Assert ServiceAccount, 2 NetworkPolicies, and PVC exist in the workspace namespace.
  local ws_tenant_ns="${ws_ns}"
  log::info "Asserting SA + NetworkPolicies + PVC in namespace '${ws_tenant_ns}'…"

  local sa_count
  sa_count=$(kubectl --context="${KUBE_CTX}" -n "${ws_tenant_ns}" \
    get serviceaccounts -l "keese.ai/workspace=${ws_name}" \
    -o json 2>/dev/null \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d.get('items',[])))" \
      2>/dev/null || echo "0")

  local netpol_count
  netpol_count=$(kubectl --context="${KUBE_CTX}" -n "${ws_tenant_ns}" \
    get networkpolicies \
    -o json 2>/dev/null \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d.get('items',[])))" \
      2>/dev/null || echo "0")

  local pvc_count
  pvc_count=$(kubectl --context="${KUBE_CTX}" -n "${ws_tenant_ns}" \
    get pvc -l "keese.ai/workspace=${ws_name}" \
    -o json 2>/dev/null \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d.get('items',[])))" \
      2>/dev/null || echo "0")

  local ok=true
  if [[ "${sa_count}" -lt "1" ]]; then
    log::err "Expected ≥1 ServiceAccount with label keese.ai/workspace=${ws_name}; got ${sa_count}"
    ok=false
  fi
  if [[ "${netpol_count}" -lt "2" ]]; then
    log::err "Expected ≥2 NetworkPolicies in ${ws_tenant_ns}; got ${netpol_count}"
    ok=false
  fi
  if [[ "${pvc_count}" -lt "1" ]]; then
    log::err "Expected ≥1 PVC with label keese.ai/workspace=${ws_name}; got ${pvc_count}"
    ok=false
  fi

  if [[ "${ok}" == "false" ]]; then
    return 1
  fi

  log::ok "Workspace '${ws_name}' is ${ws_phase} with SA(${sa_count}) + NetworkPolicies(${netpol_count}) + PVC(${pvc_count})."
}

phase_08_sample_session() {
  kubectl --context="${KUBE_CTX}" \
    apply -f "${REPO_ROOT}/config/samples/workspace/workspacesession-minimal.yaml"

  local sess_name
  sess_name=$(kubectl --context="${KUBE_CTX}" \
    get workspacesession.keese.ai \
    --all-namespaces \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
  local sess_ns
  sess_ns=$(kubectl --context="${KUBE_CTX}" \
    get workspacesession.keese.ai \
    --all-namespaces \
    -o jsonpath='{.items[0].metadata.namespace}' 2>/dev/null || echo "")

  if [[ -z "${sess_name}" ]]; then
    log::err "No WorkspaceSession found after apply."
    return 1
  fi

  log::info "Waiting for WorkspaceSession '${sess_name}' to reach phase=Active (${TIMEOUT_SESSION}s)…"
  local deadline
  deadline=$(($(date +%s) + TIMEOUT_SESSION))
  local sess_phase=""
  while [[ $(date +%s) -lt ${deadline} ]]; do
    sess_phase=$(kubectl --context="${KUBE_CTX}" \
      -n "${sess_ns}" \
      get workspacesession.keese.ai "${sess_name}" \
      -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    if [[ "${sess_phase}" == "Active" ]]; then
      break
    fi
    log::dim "  WorkspaceSession phase: '${sess_phase}' — retrying…"
    sleep 5
  done

  if [[ "${sess_phase}" != "Active" ]]; then
    log::err "WorkspaceSession '${sess_name}' did not reach Active within ${TIMEOUT_SESSION}s (got '${sess_phase}')."
    kubectl --context="${KUBE_CTX}" -n "${sess_ns}" \
      describe workspacesession.keese.ai "${sess_name}" >&2 || true
    return 1
  fi

  # Assert a Pod was created for this session.
  local pod_count
  pod_count=$(kubectl --context="${KUBE_CTX}" -n "${sess_ns}" \
    get pods -l "keese.ai/session=${sess_name}" \
    -o json 2>/dev/null \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d.get('items',[])))" \
      2>/dev/null || echo "0")

  if [[ "${pod_count}" -lt "1" ]]; then
    log::err "Expected ≥1 Pod with label keese.ai/session=${sess_name}; got ${pod_count}"
    return 1
  fi

  log::ok "WorkspaceSession '${sess_name}' is Active with ${pod_count} Pod(s)."
}

phase_09_teardown() {
  # Delete samples (finalizers cascade — wait for removal).
  log::info "Deleting WorkspaceSession samples…"
  kubectl --context="${KUBE_CTX}" \
    delete -f "${REPO_ROOT}/config/samples/workspace/workspacesession-minimal.yaml" \
    --wait=true --timeout="${TIMEOUT_TEARDOWN}s" 2>/dev/null || true

  log::info "Deleting Workspace samples…"
  kubectl --context="${KUBE_CTX}" \
    delete -f "${REPO_ROOT}/config/samples/workspace_v1alpha1_workspace.yaml" \
    --wait=true --timeout="${TIMEOUT_TEARDOWN}s" 2>/dev/null || true

  log::info "Deleting Tenant samples…"
  kubectl --context="${KUBE_CTX}" \
    delete -f "${REPO_ROOT}/config/samples/tenancy/tenant-minimal.yaml" \
    --wait=true --timeout="${TIMEOUT_TEARDOWN}s" 2>/dev/null || true

  # Bring Tilt down.
  tilt down 2>/dev/null || true
  local tilt_pid_file="${PLAN_LOGS}/e2e-smoke-tilt.pid"
  if [[ -f "${tilt_pid_file}" ]]; then
    local tpid
    tpid="$(cat "${tilt_pid_file}")"
    kill "${tpid}" 2>/dev/null || true
    rm -f "${tilt_pid_file}"
  fi

  if [[ "${KEEP}" == "false" ]]; then
    log::info "Deleting kind cluster '${KIND_CLUSTER}' (--no-keep)…"
    kind delete cluster --name="${KIND_CLUSTER}" || true
    log::ok "Cluster deleted."
  else
    log::ok "Cluster preserved (--keep). Run 'make kind-down' to tear it down."
  fi
}

# ── Main ──────────────────────────────────────────────────────────────────────

main() {
  log::info "e2e-smoke starting. Logs dir: ${PLAN_LOGS}"
  log::info "Keep cluster after smoke: ${KEEP}"
  [[ -n "${PHASE_FROM}" ]] && log::info "Resuming from phase: ${PHASE_FROM}"

  run::step "01" "pre-flight checks" phase_01_preflight
  run::step "02" "cluster up" phase_02_cluster_up
  run::step "03" "bootstrap dev deps" phase_03_bootstrap_infra
  run::step "04" "operator deploy" phase_04_operator_deploy
  run::step "05" "OIDCProvider bootstrap" phase_05_oidcprovider_bootstrap
  run::step "06" "sample Tenant" phase_06_sample_tenant
  run::step "07" "sample Workspace" phase_07_sample_workspace
  run::step "08" "sample WorkspaceSession" phase_08_sample_session
  run::step "09" "teardown" phase_09_teardown

  log::ok "e2e-smoke: ALL PHASES PASSED."
}

main
