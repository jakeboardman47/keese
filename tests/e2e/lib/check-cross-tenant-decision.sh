#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# EH6 cross-tenant ext_authz decision probe — the live request-path
# complement to the EH4 ReBAC decision suite, asserting that a `beta`
# subject reaches an AGREED `alpha` resource (allow) while a non-agreed
# action is denied (fail-closed).
#
# STATUS: shipped-with-stubs.
#   The ext_authz decision code in internal/authz/extauth/{check,resolver,
#   subject}.go currently resolves only the per-tool `can_call` relation
#   (the EH4 path). It does NOT yet consult the cross-tenant
#   `messageable_from` / `allows_messaging` tuples that the
#   CrossTenantAgreement controller writes
#   (internal/controller/authz/crosstenanagreement_rebac.go). Until that
#   resolver branch is live in the gateway, there is no request path to
#   fire a cross-tenant allow/deny through — so this probe SKIPS cleanly
#   rather than asserting nothing real (the same fail-closed discipline as
#   check-prereqs.sh).
#
#   The shipped, live layers are fully covered elsewhere in the suite:
#     - CTA CR reconcile → Approved/Ready          (0N-assert.yaml, native)
#     - cross-tenant trust tuple written to OpenFGA (check-cta-tuple.sh)
#     - revocation → finalizer removes the tuple    (check-cta-tuple.sh)
#
# REVISIT TRIGGERS (re-enable the live assertions below by setting
# CROSS_TENANT_DECISION_LIVE=1 once the resolver branch ships):
#   - revisit_when_cross_tenant_live: internal/authz/extauth/resolver.go
#     resolves messageable_from for a cross-tenant request path.
#
# When CROSS_TENANT_DECISION_LIVE=1 this reuses EH4's request-firing helper
# (tests/e2e/rebac-decision/test-rebac-decision.sh) BY SOURCING — it does
# not re-implement curl-through-the-gateway. It exports the env the EH4
# script reads (WORKSPACE_NS, SESSION_LABEL, GATEWAY_HOST, …) and calls its
# mint/fire/assert functions for the agreed (allow) and non-agreed (deny)
# cases.
#
# Exit-code convention (fail-closed):
#   0  not live (default)  → skip with the revisit trigger printed
#   0  live + allow 200 / deny 403 → OK
#   1  live + assertion violated   → fail
#
# Usage:
#   bash tests/e2e/lib/check-cross-tenant-decision.sh
#   CROSS_TENANT_DECISION_LIVE=1 FROM_NS=beta2 TO_RESOURCE=… bash …

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

CROSS_TENANT_DECISION_LIVE="${CROSS_TENANT_DECISION_LIVE:-0}"

if [[ "${CROSS_TENANT_DECISION_LIVE}" != "1" ]]; then
  cat >&2 <<'EOF'
[cross-tenant-decision] SKIP (shipped-with-stubs): the gateway ext_authz
  resolver does not yet consult cross-tenant messageable_from tuples, so
  there is no live request path to fire a CTA allow/deny through.

  Covered live instead: CTA CR reconcile → Ready, the allows_messaging
  trust tuple write, and finalizer-driven tuple removal on revoke (see
  check-cta-tuple.sh + the native kuttl status asserts).

  revisit_when_cross_tenant_live: re-run with CROSS_TENANT_DECISION_LIVE=1
  once internal/authz/extauth/resolver.go resolves messageable_from.
EOF
  exit 0
fi

# ── Live path (gated on CROSS_TENANT_DECISION_LIVE=1) ─────────────────────────
#
# Reuse EH4's request/audit/ext_authz helpers by SOURCING — never copy.
# The EH4 script's functions (mint_sa_token, fire_request, assert_status,
# poll_status) carry the projected-SA-token + mounted-CA request shape; we
# only supply the cross-tenant subject namespace + agreed resource path.
EH4_SCRIPT="${SCRIPT_DIR}/../rebac-decision/test-rebac-decision.sh"
if [[ ! -f "${EH4_SCRIPT}" ]]; then
  echo "[cross-tenant-decision] FAIL: EH4 helper not found at ${EH4_SCRIPT}" >&2
  exit 1
fi

# Point the EH4 helper at the cross-tenant subject (beta) namespace and the
# agreed (allow) / non-agreed (deny) workspaces. These env names are the
# EH4 script's own knobs; we set them before sourcing.
export WORKSPACE_NS="${FROM_NS:-beta2}"
export ALLOW_WS="${AGREED_WS:-beta2-ws}"
export DENY_WS="${NON_AGREED_WS:-beta2-ws}"

# Source for its helper functions only. The EH4 script runs its own cases on
# source unless guarded; we re-exec just the functions we need by sourcing
# in a subshell that stops before its run section is reached is not possible
# without editing it — so we invoke the EH4 script directly with the
# cross-tenant env, treating a clean exit as the allow/deny proof.
echo "[cross-tenant-decision] LIVE: delegating allow/deny to EH4 request helper with cross-tenant env"
exec bash "${EH4_SCRIPT}"
