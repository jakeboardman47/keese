<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: spec
category: contract
depends:
  - ../designs/24-tenant-crd.md
  - ../designs/24b-tenant-crd.md
  - ../designs/01-tenancy-capsule.md
  - ../designs/04a-openfga-authz-model.md
related_skills: [crd-authoring, controller-authoring]
status: current
last_verified: 2026-04-21
regression_lock: false
---

# tenancy.operator.keese.ai v1alpha1 — Tenant CRD

Companion to [`tenancy.operator.keese.ai-v1alpha1.md`](tenancy.operator.keese.ai-v1alpha1.md).

## Spec schema

```yaml
apiVersion: tenancy.operator.keese.ai/v1alpha1
kind: Tenant
metadata:
  name: <name>   # cluster-scoped; name IS the OpenFGA identity key
spec:
  # Mode B: Capsule delegates namespace aggregation; non-empty = Mode B.
  capsuleTenantRef:
    name: <capsule-tenant-name>   # must resolve (webhook); immutable while namespaces live
  # Mode A: label-selector aggregation (ignored if capsuleTenantRef set).
  namespaceSelector:
    matchLabels:
      keese.ai/tenant: <name>
  # +keese:rebac-tuple=tenant:T#admin@user:U
  adminSubjects:                  # required; non-empty (VAP)
    - kind: User
      name: <oidc-email>
  defaultGuardrailBindings:       # names of GuardrailBinding CRs to inherit
    - default-rate-limit
  tokenBudgetRef:
    name: <budget-name>
    namespace: <ns>               # must resolve when set (webhook)
  credentialPoolRef:
    name: <pool-name>
    namespace: <ns>               # must resolve when set (webhook)
  defaultWorkspaceQuota:          # ResourceList; dims ≥ 0 (VAP)
    requests.cpu: "4"
    requests.memory: "8Gi"
  dedicatedGateway: false         # cannot toggle while status.namespaces[] non-empty (VAP)
  oidc:
    allowedProviders:             # D28 cross-cut; empty = allow all configured providers
      - https://accounts.google.com
  security:
    allowUnsafeTransports: false  # 09 cross-cut; enables non-TLS transport (break-glass)
  defaultRetryBudget:
    maxRetries: 3
    perCallTimeout: 30s
  artifactStoreRef:
    name: <store-name>
    namespace: <ns>
  jwksCacheFailOpenSeconds: 300   # int [30,600]; default 300 (60 if dedicatedGateway)
  auditArgumentsRedacted: false   # explicit opt-in; default false (PII-safe)
status:
  observedGeneration: 0
  phase: Pending                  # Pending|Provisioning|Active|Suspended|Terminating
  conditions:
    - type: Ready
      status: "False"
      reason: Provisioning
      lastTransitionTime: "..."
  namespaces: []                  # observed namespace list (Mode A: label-derived; Mode B: Capsule-mirrored)
  capsuleTenantResolved: false    # Mode B only
```

## VAP CEL invariants

Named: `tenant-policy.tenancy.operator.keese.ai/v1alpha1`

```cel
# adminSubjects non-empty
size(self.spec.adminSubjects) > 0

# dedicatedGateway immutable while namespaces live
oldSelf.spec.dedicatedGateway == self.spec.dedicatedGateway ||
  size(self.status.namespaces) == 0

# jwksCacheFailOpenSeconds range (0 = unset; webhook defaults)
self.spec.jwksCacheFailOpenSeconds == 0 ||
  (self.spec.jwksCacheFailOpenSeconds >= 30 &&
   self.spec.jwksCacheFailOpenSeconds <= 600)

# Warn if both capsuleTenantRef and namespaceSelector set (non-blocking)
# [admission warning: NamespaceSelectorIgnoredInModeB]
```

## Mutating webhook defaulting

- `jwksCacheFailOpenSeconds` unset (0) + `dedicatedGateway: false` → 300
- `jwksCacheFailOpenSeconds` unset (0) + `dedicatedGateway: true` → 60

## Validating webhook (cross-resource)

- `capsuleTenantRef` resolves to existing `capsule.clastix.io/v1beta2/Tenant`
- `namespaceSelector` does not overlap another Tenant's selector
- `tokenBudgetRef`, `credentialPoolRef`, `artifactStoreRef` resolve when set

## Mode A reconcile

Controller watches Namespaces matching `spec.namespaceSelector`; maintains
`status.namespaces[]`. On `keese.ai/tenant` label removal with live Workspaces:
installs finalizer `finalizers.tenant.operator.keese.ai/namespaces` on namespace;
VAP blocks label removal while finalizer present; finalizer removed only after all
Workspaces are Terminating. Event: `TenantLabelLocked`.

## Mode B reconcile

Controller mirrors Capsule `status.namespaces[]` via informer on
`capsule.clastix.io/v1beta2/Tenant`. `status.capsuleTenantResolved` flips `true`
when Capsule Tenant found. `namespaceSelector` ignored.

## ReBAC markers

```go
// +keese:rebac-tuple=tenant:T#admin@user:U     (spec.adminSubjects[])
// +keese:rebac-tuple=tenant:T#member@service_account:SA  (written by Workspace controller on create)
```

Operator bootstrap Job writes `tenant:T#admin@user:U` tuples from `spec.adminSubjects[]`.

## OwnerRef chain

| Object | OwnerRef to Tenant | Rationale |
|---|---|---|
| `TokenBudget` (via `tokenBudgetRef`) | Yes | Budget is tenant-lifecycle-coupled |
| `GuardrailBinding` (default) | No | Bindings may be shared across tenants |
| `Workspace` | No | Association is label-based; cascade would destroy user data |

## Samples

Minimum viable:
```yaml
apiVersion: tenancy.operator.keese.ai/v1alpha1
kind: Tenant
metadata:
  name: alpha
spec:
  adminSubjects:
    - kind: User
      name: alice@example.com
```

Fully populated: `config/samples/tenancy/tenant-full.yaml`
(Mode B, quota, OIDC filter, dedicated gateway, audit redaction).

Both pass `kubectl apply --dry-run=server` against envtest (rule 04.15).

## Acceptance tests

| ID | Name | File | Description |
|---|---|---|---|
| T-01 | ModeAToModeBSwitch | `test/kuttl/tenancy/tenant-mode-switch/` | Apply `capsuleTenantRef`; verify `status.namespaces` mirrors Capsule; VAP warns on dual fields |
| T-02 | OIDCAllowlistEnforcement | `internal/controller/tenancy/tenant/admission_test.go` | Workspace in unlisted OIDC provider rejected at admission; allowed provider passes |
| T-03 | DefaultBindingInheritance | `internal/controller/tenancy/tenant/reconcile_test.go` | Workspace inherits `defaultGuardrailBindings`; removed binding does not cascade to Workspace |
| T-04 | FinalizerCascade | `test/kuttl/tenancy/tenant-finalizer-cascade/` | Delete Tenant with live Workspace; finalizer blocks; drain Workspace; finalizer releases; Tenant deleted |
| T-05 | JWKSDefaulting | `test/envtest/admission/tenant_fields_test.go` | `jwksCacheFailOpenSeconds` unset + `dedicatedGateway: false` → 300; unset + `true` → 60; out-of-range rejected |
| T-06 | EnvtestIdempotency | `internal/controller/tenancy/tenant/suite_test.go` | ≥ 3 reconciles with no spec change; `observedGeneration` stable; no duplicate events |
| T-07 | DefaultRetryBudgetAdmission | `test/envtest/admission/tenant_retry_budget_test.go` | `maxRetries: 0` admitted (floor 0 valid); `perCallTimeout: 0s` rejected (VAP floor 1s); omitted field defaults via webhook; verify CEL rule in VAP manifest |
| T-08 | ArtifactStoreRefResolutionFailure | `test/envtest/admission/tenant_artifact_store_ref_test.go` | `artifactStoreRef` pointing at non-existent ConfigMap/Secret → webhook rejects with `RefNotResolved` reason; valid ref admits; `credentialPoolRef` same path |
| T-09 | ModeAToModeBWithActiveWorkspaces | `internal/controller/tenancy/tenant/mode_transition_test.go` | Add `capsuleTenantRef` to Active Tenant that has Workspaces; verify: `namespaceSelector` silently ignored (warning event `NamespaceSelectorIgnoredInModeB`); `status.namespaces` transitions to Capsule-mirrored list; no Workspace interruption |
