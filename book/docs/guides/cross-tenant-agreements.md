<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Cross-tenant agreements

A `CrossTenantAgreement` (CRA) is the bilateral contract that must exist — and be
approved by both sides — before any agent in one tenant can exchange messages with an
agent in another tenant.

!!! info "Audience"
    Tenant admins who need to open a cross-tenant NATS messaging channel or A2A role
    between two existing tenants. · **Prerequisites:**
    [Provision a tenant](provision-tenant.md) ·
    [Concepts: cross-tenant collaboration](../concepts/cross-tenant.md) ·
    [Concepts: authorization (ReBAC)](../concepts/authorization-rebac.md)

---

## Overview

By default every workspace namespace is fail-closed: agents can only reach their own
tenant's Envoy AI Gateway endpoint. To let agents in tenant **A** talk to agents in
tenant **B** you need a `CrossTenantAgreement` that:

1. Names both tenants and an optional workspace selector for each side.
2. Lists the NATS subject patterns that will be shared (prefix `keese.cta.`).
3. Declares the A2A roles (`reader`, `writer`, or `bidirectional`).
4. Carries approval signatures from both tenant admins before any OpenFGA tuple is
   written.

The CRA is a **cluster-scoped** resource (short name `cra`). Only the
`crosstenanagreement-controller` writes the corresponding OpenFGA tuples; no other actor
may bypass the handshake.

---

## Two-party approval sequence

```mermaid
sequenceDiagram
    actor AdminA as Tenant-A admin
    actor AdminB as Tenant-B admin
    participant API as Kubernetes API
    participant Webhook as Admission webhook
    participant FGA as OpenFGA
    participant NATS as NATS JetStream
    participant Ctrl as CRA controller

    AdminA->>API: kubectl create cra agreement-ab.yaml
    API-->>AdminA: CRA created (phase: Pending)

    Note over Ctrl: Ensure NATS finalizer; initialize phase

    AdminA->>API: kubectl annotate cra agreement-ab<br/> keese.ai/cra-approve=true<br/> + keese.ai/cra-approving-tenant=tenant-a<br/> + keese.ai/cra-approver=alice<br/> + keese.ai/cra-signature=<sig><br/> + keese.ai/cra-signature-type=oidc-keyless
    API->>Webhook: AdmissionRequest (UPDATE)
    Webhook->>FGA: Check(tenant:tenant-a#can_approve_cra@user:alice)
    FGA-->>Webhook: allowed
    Webhook->>Webhook: Verify cosign/OIDC signature over (cra-uid || tenant-uid || expiresAt)
    Webhook-->>API: admission allowed

    Ctrl->>Ctrl: validateApprovalAnnotation → CRAApproval{tenant-a, alice}
    Ctrl->>API: Patch metadata (remove approval annotations)
    Ctrl->>API: Status patch: append approval[0]

    AdminB->>API: kubectl annotate cra agreement-ab<br/> keese.ai/cra-approve=true<br/> + keese.ai/cra-approving-tenant=tenant-b<br/> + keese.ai/cra-approver=bob<br/> + keese.ai/cra-signature=<sig><br/> + keese.ai/cra-signature-type=sa-token
    API->>Webhook: AdmissionRequest (UPDATE)
    Webhook->>FGA: Check(tenant:tenant-b#can_approve_cra@serviceaccount:bob)
    FGA-->>Webhook: allowed
    Webhook-->>API: admission allowed

    Ctrl->>Ctrl: validateApprovalAnnotation → CRAApproval{tenant-b, bob}
    Ctrl->>Ctrl: bothTenantsApproved() = true
    Ctrl->>Ctrl: resolveWorkspaces (freeze snapshot — TOFU)
    Ctrl->>FGA: Write tuple tenant:tenant-b#allows_messaging@tenant:tenant-a
    Ctrl->>FGA: Write tuple workspace:ws-b#messageable_from@workspace:ws-a
    Ctrl->>NATS: (stream provisioned at first transportRef use)
    Ctrl->>API: Status patch: phase=Approved, workspaceSnapshot frozen
    API-->>AdminA: Event CRAApproved
    API-->>AdminB: Event CRAApproved
```

---

## Step 1 — Author the CRA manifest

```yaml
# config/samples/tenancy/cra-ab-example.yaml
apiVersion: authz.keese.ai/v1alpha1
kind: CrossTenantAgreement
metadata:
  name: agreement-ab
spec:
  from:
    tenantRef:
      name: tenant-a            # must resolve to an existing Tenant CR
    workspaceSelector:          # optional; omit to cover all workspaces
      matchLabels:
        keese.ai/purpose: data-pipeline
  to:
    tenantRef:
      name: tenant-b
    workspaceSelector:
      matchLabels:
        keese.ai/purpose: analytics
  scope:
    natsSubjects:
      - keese.cta.events.ingest   # every subject MUST start with keese.cta.
      - keese.cta.results.deliver
    a2aRoles:
      - bidirectional              # reader | writer | bidirectional
  expiresAt: "2027-01-01T00:00:00Z"   # RFC3339; immutable after create
```

```bash
kubectl apply -f config/samples/tenancy/cra-ab-example.yaml
kubectl get cra agreement-ab
# NAME            AGE   READY   PHASE     FROM       TO
# agreement-ab    3s    False   Pending   tenant-a   tenant-b
```

!!! note "Validation constraints"
    - `spec.from.tenantRef.name` must differ from `spec.to.tenantRef.name` — self-agreements
      are rejected by a CEL `XValidation` rule on the spec.
    - `spec.scope.natsSubjects` must all start with `keese.cta.` and the list is capped at 50
      entries (enforced by a second `XValidation` rule).
    - `spec.expiresAt` must be in the future on create and is immutable thereafter (CRD XValidation CEL rule).

---

## Step 2 — Tenant-A admin approves

Use five annotations together. The admission webhook validates `can_approve_cra`
permission in OpenFGA before the controller ever sees the annotation.

```bash
kubectl annotate crosstenanagreement agreement-ab \
  keese.ai/cra-approve=true \
  keese.ai/cra-approving-tenant=tenant-a \
  keese.ai/cra-approver=alice@example.com \
  keese.ai/cra-signature="$(cat /tmp/approval-sig-a.b64)" \
  keese.ai/cra-signature-type=oidc-keyless
```

**Signature computation (human / OIDC-keyless path):**

The signature is a cosign keyless OIDC signature over the concatenated bytes of
`(cra-uid || tenant-uid || expiresAt)`. Generate it with:

```bash
PAYLOAD="$(kubectl get cra agreement-ab -o jsonpath='{.metadata.uid}')\
$(kubectl get tenant tenant-a -o jsonpath='{.metadata.uid}')\
2027-01-01T00:00:00Z"

echo -n "$PAYLOAD" | cosign sign-blob \
  --output-signature /tmp/approval-sig-a.b64 \
  --identity-token "$(kubectl auth token --audience keese-egress-tenant-a)" \
  -
```

!!! warning "Planned — cosign integration not yet wired"
    The `CosignVerifier` in the controller is currently backed by a fake stub
    (`FakeCosignVerifier`) in alpha. Signature bytes are stored and the field is
    structurally validated, but cryptographic verification is not performed in the
    running controller. The webhook admission check for `can_approve_cra` is also
    stubbed — full enforcement lands in a later phase. **Do not rely on signature
    verification for security in the current alpha build.**

**Signature computation (CI / ServiceAccount path):**

For automated approvals, use an HMAC-SHA256 signature where the **key** is the raw
bytes of `keese-cra-hmac[secret]` (in `keese-system`) and the **message** is only the
approval audience string `keese-egress-<tenant>`. The result is hex-encoded (not
base64). The SA's projected token is the approver's *identity* annotation, not the
HMAC key.

```bash
AUDIENCE="keese-egress-tenant-a"
SECRET=$(kubectl get secret keese-cra-hmac -n keese-system \
  -o jsonpath='{.data.secret}' | base64 -d)
SIG=$(printf '%s' "$AUDIENCE" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $2}')   # hex HMAC over the audience

kubectl annotate crosstenanagreement agreement-ab \
  keese.ai/cra-approve=true \
  keese.ai/cra-approving-tenant=tenant-a \
  keese.ai/cra-approver="system:serviceaccount:tenant-a:pipeline-sa" \
  keese.ai/cra-signature="$SIG" \
  keese.ai/cra-signature-type=sa-token
```

After annotation the controller strips the approval annotations from metadata and
appends a `CRAApproval` entry to `status.approvals`. The CRA stays in `Pending`
until the second approval arrives.

```bash
kubectl get cra agreement-ab -o jsonpath='{.status.approvals}' | jq .
# [{"tenant":"tenant-a","approvedBy":"alice@example.com","approvedAt":"...","signatureType":"oidc-keyless",...}]
```

---

## Step 3 — Tenant-B admin approves

Repeat the same annotation pattern for tenant-b:

```bash
kubectl annotate crosstenanagreement agreement-ab \
  keese.ai/cra-approve=true \
  keese.ai/cra-approving-tenant=tenant-b \
  keese.ai/cra-approver=bob@example.com \
  keese.ai/cra-signature="$(cat /tmp/approval-sig-b.b64)" \
  keese.ai/cra-signature-type=oidc-keyless
```

Once both approvals are recorded the controller calls `bothTenantsApproved()`, freezes
the workspace snapshot, writes the OpenFGA tuples, and transitions the CRA to
`Approved`.

```bash
kubectl get cra agreement-ab
# NAME            AGE   READY   PHASE      FROM       TO
# agreement-ab    4m    True    Approved   tenant-a   tenant-b
```

---

## Step 4 — Verify OpenFGA tuples

Once the CRA is `Approved` you can inspect the tuples the controller wrote:

```bash
# tenant-level messaging tuple
fga tuple read \
  --store-id "$FGA_STORE_ID" \
  --object "tenant:tenant-b" \
  --relation "allows_messaging" \
  --user "tenant:tenant-a"

# workspace-level messageable_from tuple (one per snapshot pair)
fga tuple read \
  --store-id "$FGA_STORE_ID" \
  --object "workspace:ws-b" \
  --relation "messageable_from" \
  --user "workspace:ws-a"
```

You can also inspect the snapshot the controller froze at approval time:

```bash
kubectl get cra agreement-ab \
  -o jsonpath='{.status.workspaceSnapshot}' | jq .
# [{"fromWorkspace":"ws-a","toWorkspace":"ws-b","snapshotAt":"..."}]
```

!!! warning "Workspace snapshot uses synthetic names in alpha"
    `resolveWorkspaces` in the controller currently returns a synthetic name
    `ws-<tenantName>` rather than enumerating real `Workspace` CRs. A
    `TODO(spec-followup)` comment in
    [`internal/controller/authz/crosstenanagreement_controller.go:478`](https://github.com/keese-ai/keese/blob/main/internal/controller/authz/crosstenanagreement_controller.go#L478)
    tracks the real implementation (unstructured + field index on `tenantRef.name`).
    The tuples and snapshot entries you see will carry these synthetic names until
    that work lands. **Do not build automation that parses `workspaceSnapshot`
    workspace names against real Workspace CRs in the current alpha build.**

---

## Step 5 — Verify NATS subjects are provisioned

Cross-tenant NATS streams are provisioned lazily by the Workflow controller at the
first `transportRef` use. The stream name is derived from the CRA UID:

```bash
# Stream name format: keese-cta-<cra-uid>
CRA_UID=$(kubectl get cra agreement-ab -o jsonpath='{.metadata.uid}')
STREAM="keese-cta-${CRA_UID}"

nats stream info "$STREAM" --server "$NATS_URL"
# Stream info will show subjects: ["keese.cta.<cra-uid>.>"]
```

!!! note "Stream provisioned on first use"
    If the stream is not yet present, trigger a cross-tenant workflow step that
    references the `transportRef` in your `Workflow` spec. The Workflow controller
    will provision the stream on first use. The CRA finalizer
    `finalizers.crosstenanagreement.keese.ai/nats` ensures the stream is deleted
    when the CRA is deleted.

---

## Step 6 — Test cross-tenant messaging

Once the CRA is `Approved` and the stream exists, test a publish/subscribe round-trip:

```bash
# Publish from a pod in tenant-a
kubectl exec -n tenant-a deploy/test-sender -- \
  nats pub "keese.cta.events.ingest" "hello from tenant-a" \
  --server "$NATS_URL" \
  --creds /var/run/keese/secrets/nats-creds

# Subscribe from a pod in tenant-b
kubectl exec -n tenant-b deploy/test-receiver -- \
  nats sub "keese.cta.events.ingest" \
  --server "$NATS_URL" \
  --creds /var/run/keese/secrets/nats-creds
```

!!! danger "Cross-tenant NATS credentials not automatically provisioned"
    The CRA controller gates the *authorization* model via OpenFGA tuples. It does
    **not** automatically provision NATS credentials or NetworkPolicy rules for
    cross-tenant pods. Egress to the NATS broker must be explicitly permitted in each
    tenant's `NetworkPolicy` (see [Concepts: network isolation](../concepts/network-isolation.md))
    and the NATS credentials must be mounted via ExternalSecrets from OpenBao.

---

## Rejecting an agreement

Either tenant admin can reject a CRA at any time while it is `Pending`:

```bash
kubectl annotate crosstenanagreement agreement-ab \
  keese.ai/cra-reject=true \
  keese.ai/cra-approving-tenant=tenant-b
```

The controller transitions the CRA to `Rejected`, deletes any tuples that were
written, and emits the `CRARejected` event. `Rejected` is a terminal phase — to
re-establish the channel, create a new CRA.

---

## Expiry and renewal

When `spec.expiresAt` is reached the controller:

1. Deletes all OpenFGA tuples recorded in `status.workspaceSnapshot`.
2. Transitions `status.phase` to `Expired`.
3. Emits a `CRAExpired` event.
4. Removes the NATS finalizer only after stream deletion succeeds (fail-closed).

The expired CRA is retained for audit. To renew, create a new CRA object:

```bash
# Bump the expiry in your manifest, then:
kubectl create -f config/samples/tenancy/cra-ab-renewed.yaml
```

!!! note "Tuple delete failure on expiry"
    If OpenFGA is unavailable when the CRA expires, the controller emits
    `TupleSyncFailed` and retries with exponential backoff. The phase stays
    `Approved` past the deadline (fail-closed) and transitions to `Expired` only
    once the delete call succeeds. If the cluster is unrecoverable, use
    `fga delete` manually, then annotate the CRA with `keese.ai/cra-reject=true`
    to force the transition.

---

## Workspace selector drift

After approval, if new Workspaces are created in either tenant that match
`spec.*.workspaceSelector`, the controller emits a `WorkspaceSnapshotDrift` warning
event but does **not** automatically extend the OpenFGA tuples. This is intentional
TOFU (trust-on-first-use) semantics: the bilateral approval covers the exact workspace
set frozen at approval time.

```bash
kubectl get events --field-selector reason=WorkspaceSnapshotDrift
```

To extend coverage to new workspaces, create a new CRA that explicitly selects them and
obtain fresh bilateral approvals.

---

## Monitoring

!!! note "Planned — not yet implemented"
    The CRA controller does not currently register any Prometheus metrics or emit OTEL
    spans. The metrics and span names below are the planned observability surface
    (design 25); they will be added in a future phase. Until then, use `kubectl get events`
    and the structured audit log from `keese-authz` to observe CRA activity.

    Planned metrics:

    | Metric | Type | Description |
    |---|---|---|
    | `keese_cta_phase_total{phase}` | Gauge | Count of CRAs per lifecycle phase |
    | `keese_cta_approval_latency_seconds` | Histogram | Wall-clock time from CRA creation to both approvals |
    | `keese_cta_tuple_sync_duration_seconds` | Histogram | Duration of the OpenFGA tuple sync on Approved transition |

    Planned OTEL spans: `keese.cta.approve`, `keese.cta.sync`, `keese.cta.expire`.

Event reasons to watch in `kubectl get events`:

| Reason | Severity | Meaning |
|---|---|---|
| `CRAApproved` | Normal | Both tenants approved; tuples written |
| `CRAExpired` | Normal | CRA expired; tuples deleted |
| `CRARejected` | Normal | CRA rejected by a tenant admin |
| `CRAApprovalInvalid` | Warning | Approval annotation present but signature invalid or approver lacks `can_approve_cra` |
| `SignatureVerificationFailed` | Warning | cosign or SA-token HMAC check failed |
| `TupleSyncFailed` | Warning | OpenFGA write or delete failed; controller retrying |
| `WorkspaceSnapshotDrift` | Warning | Selector now matches workspaces not in the frozen snapshot |
| `CRAConflict` | Warning | A conflicting Approved CRA already covers this tenant pair |
| `OutOfBandTupleObserved` | Warning | A pre-existing tuple was found for a workspace pair; sync skipped for that pair |

---

## Conflict detection

On each new CRA's first reconcile in `Pending` phase the controller lists all
`CrossTenantAgreement` CRs and scans for any in `Approved` phase that covers the same
`(from-tenant, to-tenant)` pair. If one is found the incoming CRA is transitioned to
`Rejected` and a `CRAConflict` event is emitted on the new object.

To replace an existing agreement, expire or delete the old one before creating the new
one.

---

## Quick reference — `kubectl` commands

```bash
# List all CRAs
kubectl get cra

# Describe a specific CRA including status, approvals, and snapshot
kubectl describe cra agreement-ab

# Approve (replace values for your environment)
kubectl annotate cra <name> \
  keese.ai/cra-approve=true \
  keese.ai/cra-approving-tenant=<tenant> \
  keese.ai/cra-approver=<identity> \
  keese.ai/cra-signature=<base64-sig> \
  keese.ai/cra-signature-type=oidc-keyless   # or sa-token

# Reject
kubectl annotate cra <name> \
  keese.ai/cra-reject=true \
  keese.ai/cra-approving-tenant=<tenant>

# Delete (triggers NATS stream GC via finalizer)
kubectl delete cra <name>
```

---

## See also

- [Concepts: cross-tenant collaboration](../concepts/cross-tenant.md) — design rationale,
  TOFU semantics, expiry lifecycle
- [Concepts: authorization (ReBAC)](../concepts/authorization-rebac.md) — OpenFGA tuple
  shapes, `can_approve_cra` relation
- [Concepts: transports & messaging](../concepts/transports.md) — NATS JetStream stream
  naming and cross-tenant subject prefix
- [Guides: provision a tenant](provision-tenant.md) — creating the Tenant CRs that a CRA
  references
