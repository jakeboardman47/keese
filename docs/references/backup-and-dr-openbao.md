<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: operations
depends:
  - docs/designs/11-secrets-pluggable-vault.md
  - dev/bootstrap/values/openbao-prod.yaml.example
related_skills: []
status: current
last_verified: 2026-05-07
---

# OpenBao — backup and DR runbook

**RPO:** 24 h | **RTO:** 30 m

## Prerequisites

- CSI driver with snapshot support installed (e.g., AWS EBS CSI, GCP PD CSI).
- `VolumeSnapshotClass` named `csi-snapclass` present in the cluster.
- Unseal keys and root token stored **off-cluster** (password manager, HSM, or cloud
  secrets manager). Never store unseal keys as K8s Secrets in the same cluster.

## Backup — daily PVC snapshot

Apply daily via a CronJob or GitOps pipeline:

```yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: openbao-data-YYYYMMDD   # substitute date
  namespace: openbao
spec:
  volumeSnapshotClassName: csi-snapclass
  source:
    persistentVolumeClaimName: data-openbao-0
```

Verification: `kubectl get volumesnapshot -n openbao` — confirm
`readyToUse: true` within 5 minutes.

## Restore

```bash
# 1. Scale down to prevent write conflicts
kubectl scale statefulset openbao -n openbao --replicas=0

# 2. Delete existing PVC (data is in the snapshot)
kubectl delete pvc data-openbao-0 -n openbao

# 3. Create PVC from snapshot
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: data-openbao-0
  namespace: openbao
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: standard
  dataSource:
    name: openbao-data-<YYYYMMDD>
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io
  resources:
    requests:
      storage: 10Gi
EOF

# 4. Scale back up
kubectl scale statefulset openbao -n openbao --replicas=1

# 5. Unseal (3 of 5 key shares required; retrieve from off-cluster store)
kubectl exec -n openbao openbao-0 -- bao operator unseal <key-1>
kubectl exec -n openbao openbao-0 -- bao operator unseal <key-2>
kubectl exec -n openbao openbao-0 -- bao operator unseal <key-3>

# 6. Verify
kubectl exec -n openbao openbao-0 -- bao status | grep Sealed
# Expected: Sealed: false
kubectl exec -n openbao openbao-0 -- \
  bao kv get keese/tenants/<tenant>/<provider>
# Expected: key-value pairs for the provider credential
```

## Cloud KMS auto-unseal (production)

Replace the file storage block in `openbao-prod.yaml.example` with a KMS seal
stanza (`seal "awskms"` / `seal "gcpckms"` / `seal "azurekeyvault"`).

**KMS rotation policy:** yearly. Proof-of-rotation runbook:

```bash
# After rotating the KMS key to a new version:
# 1. Confirm OpenBao rewraps (check logs)
kubectl logs -n openbao openbao-0 | grep "re-wrap"
# 2. Restart and verify auto-unseal
kubectl rollout restart statefulset/openbao -n openbao
kubectl exec -n openbao openbao-0 -- bao status | grep Sealed
# Expected: Sealed: false
```

## Most likely failure mode

KMS key deleted or IAM role detached — OpenBao starts sealed and all secret
lookups fail. Fix: re-grant IAM access and restart the pod. No snapshot restore
needed unless data is also lost.
