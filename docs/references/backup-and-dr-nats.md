<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: reference
category: operations
depends:
  - docs/designs/09-transport-crd.md
related_skills: []
status: current
last_verified: 2026-05-07
---

# NATS JetStream — backup and DR runbook

**RPO:** per-stream policy (see cadence trade-offs below) | **RTO:** varies

## Prerequisites

- `nats` CLI installed — ships in the Nix devshell via `flake.nix`.
- NATS server URL available (adjust for TLS environments):

```bash
export NATS_URL=nats://nats.nats.svc:4222
```

- Streams follow the naming convention from design 09:
  - Transport streams: `keese.tenant.<t>.transport.<name>.*`
  - Workflow streams: `keese.tenant.<t>.wf.<r>.*`

## Backup

```bash
# List streams to identify targets
nats --server "$NATS_URL" stream list

# Backup one stream (creates a directory of files)
nats --server "$NATS_URL" stream backup \
  "keese.tenant.<t>.transport.<name>" \
  ./nats-backup/$(date +%Y%m%d)/keese.tenant.<t>.transport.<name>/
```

Store the backup directory off-cluster (object storage).

### Cadence trade-offs

| Stream type | Recommended cadence | Rationale |
|---|---|---|
| Transport streams (low-volume) | Daily | Feasible; supports full replay |
| Transport streams (high-volume) | Consumer replay only | Multi-GB backup exceeds RTO |
| Workflow streams | Consumer replay only | Downstream controllers are idempotent (rule 06.6); backup not cost-effective for ephemeral workflow state |

For streams where backup is skipped, document the decision in the stream's
`description` field so operators know the intent.

## Restore

```bash
nats --server "$NATS_URL" stream restore \
  "keese.tenant.<t>.transport.<name>" \
  ./nats-backup/<YYYYMMDD>/keese.tenant.<t>.transport.<name>/

# Verify
nats --server "$NATS_URL" stream info \
  "keese.tenant.<t>.transport.<name>"
# Check: messages count, first/last sequence, consumer count
nats --server "$NATS_URL" stream report
```

## Most likely failure mode

Consumer sequence offsets ahead of the restored stream sequence — consumers
error with `sequence not found`. Fix: reset the consumer to the first available
sequence.

```bash
# Reset consumer to deliver from the beginning of the restored stream
nats --server "$NATS_URL" consumer edit \
  "keese.tenant.<t>.transport.<name>" \
  <consumer-name> \
  --deliver-first
```

## Idempotency requirement

Restored consumer state may cause message replay. All keese Workflow controllers
are required to be idempotent (rule 06.6 — every reconciler converges in ≤ 3
reconciles; no side-effect doubling on replay). Verify downstream idempotency
before relying on consumer replay as a substitute for backup.
