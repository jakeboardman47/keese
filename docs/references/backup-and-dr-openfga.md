<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: operations
depends:
  - docs/designs/04a-openfga-authz-model.md
  - docs/designs/04c-openfga-store-bootstrap.md
related_skills: []
status: current
last_verified: 2026-05-07
---

# OpenFGA — backup and DR runbook

**RPO:** 1 week | **RTO:** 15 m

## Prerequisites

- OpenFGA CLI installed: `go install github.com/openfga/cli/cmd/fga@latest`
  (version ≥ 0.4; `fga version` to confirm).
- Environment variables exported:

```bash
export OPENFGA_API_URL=http://openfga.openfga.svc:8080
export OPENFGA_STORE_ID=$(kubectl get cm openfga-config -n keese-system \
  -o jsonpath='{.data.OPENFGA_STORE_ID}')
export OPENFGA_AUTHORIZATION_MODEL_ID=$(kubectl get cm openfga-config \
  -n keese-system \
  -o jsonpath='{.data.OPENFGA_AUTHORIZATION_MODEL_ID}')
```

## Backup — weekly + before any model change

```bash
# Export tuples + authorization model
fga store export --store-id "$OPENFGA_STORE_ID" \
  --api-url "$OPENFGA_API_URL" \
  > "openfga-backup-$(date +%Y%m%d).json"
```

Store the resulting JSON off-cluster (object storage, encrypted at rest).
Take an extra backup immediately before applying any authorization model change.

## Restore

```bash
# 1. Create a fresh store (do not re-use the old store ID)
NEW_STORE=$(fga store create \
  --name "keese-restored-$(date +%Y%m%d)" \
  --api-url "$OPENFGA_API_URL" | jq -r '.id')

# 2. Import tuples + model from backup
fga store import \
  --store-id "$NEW_STORE" \
  --api-url "$OPENFGA_API_URL" \
  --file openfga-backup-<YYYYMMDD>.json

# 3. Retrieve the new authorization model ID
NEW_MODEL=$(fga model list \
  --store-id "$NEW_STORE" \
  --api-url "$OPENFGA_API_URL" | jq -r '.[0].id')

# 4. Update the ConfigMap mirror so keese controllers use the new store
#    (TD-P1-03 follow-on: this will be automated via the seed Job)
kubectl patch cm openfga-config -n keese-system \
  --type merge \
  -p "{\"data\":{
    \"OPENFGA_STORE_ID\":\"$NEW_STORE\",
    \"OPENFGA_AUTHORIZATION_MODEL_ID\":\"$NEW_MODEL\"
  }}"

# 5. Restart keese-authz to pick up the new store ID
kubectl rollout restart deployment/keese-authz -n keese-system
kubectl rollout status deployment/keese-authz -n keese-system
```

## Verification

```bash
# Run the keese-authz audit smoke
make smoke-authz
# Expected: all check-tuple assertions pass; no 403s on known-allowed pairs.
```

## Most likely failure mode

ConfigMap not updated after restore — keese-authz resolves the old store ID and
all authz checks return `permission_denied`. Fix: apply step 4 above and restart.

## Notes

- `keese-system/openfga-config` mirrors `OPENFGA_STORE_ID` and
  `OPENFGA_AUTHORIZATION_MODEL_ID`. Restore must update this ConfigMap or the
  keese-authz extauth server will query the wrong store.
- Automating the CM mirror update is tracked as a TD-P1-03 follow-on in
  [docs/plans/demo/tech-debt.md](../plans/demo/tech-debt.md).
