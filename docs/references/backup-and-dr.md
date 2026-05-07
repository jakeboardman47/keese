<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: operations
depends:
  - docs/references/backup-and-dr-openbao.md
  - docs/references/backup-and-dr-openfga.md
  - docs/references/backup-and-dr-nats.md
related_skills: []
status: current
last_verified: 2026-05-07
---

# Backup and disaster recovery — index

Recovery runbooks for stateful components managed by keese.
Each component's runbook is in a dedicated file (200-line rule).

## Summary matrix

| Component | RPO | RTO | Backup tool | Restore tool |
|---|---|---|---|---|
| OpenBao | 24 h | 30 m | CSI VolumeSnapshot | CSI restore + `bao unseal` |
| OpenFGA | 1 w | 15 m | `fga store export` | `fga store import` |
| NATS streams | per-stream policy | varies | `nats stream backup` | `nats stream restore` |

## Component runbooks

| Runbook | Covers |
|---|---|
| [backup-and-dr-openbao.md](backup-and-dr-openbao.md) | PVC snapshots, unseal-key custody, cloud KMS auto-unseal rotation |
| [backup-and-dr-openfga.md](backup-and-dr-openfga.md) | Tuple + model export/import, ConfigMap mirror update |
| [backup-and-dr-nats.md](backup-and-dr-nats.md) | Per-stream backup/restore, cadence trade-offs |

## Guiding principles

1. **Off-cluster custody.** Unseal keys, export files, and NATS backup archives
   are stored outside the cluster (object storage, HSM, or password manager).
   Never store unseal keys as K8s Secrets in the same cluster.
2. **Idempotent restore.** Every restore procedure is re-runnable. Follow the
   verification step in each runbook before declaring recovery complete.
3. **Test quarterly.** Run a full restore drill in a scratch kind cluster every
   quarter to keep RTO estimates accurate.

## See also

- [docs/designs/11-secrets-pluggable-vault.md](../designs/11-secrets-pluggable-vault.md) — OpenBao design
- [docs/designs/04a-openfga-authz-model.md](../designs/04a-openfga-authz-model.md) — OpenFGA model
- [docs/designs/09-transport-crd.md](../designs/09-transport-crd.md) — NATS stream naming
