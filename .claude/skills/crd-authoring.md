---
name: crd-authoring
description: Authoring and revising keese CRDs (load before editing api/)
status: current
last_verified: 2026-04-19
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# CRD authoring (on-demand skill)

Load this before creating or revising a CRD under `api/**`. Pairs
with the `crd-author` agent and the
`docs/references/crd-design-checklist.md` reference.

## Naming

- **Group**: one of `keese.ai`, `authz.keese.ai`, or `policy.keese.ai`; mapping in
  `docs/designs/20a-api-group-layout.md`.
- **Kind**: PascalCase; singular. `WorkspaceShare` not
  `WorkspacesShare`.
- **Version**: `v1alpha1` first. Promotion rules in rule 04.2.
- **Plural**: lowercase; `workspaces`, `agentruntimes`. Controller-gen
  computes from Kind; override only if defaults are awkward.
- **Short name**: ≤ 4 chars, unique across groups. Set with
  `// +kubebuilder:resource:shortName=ws`.

## openAPIV3Schema discipline

- Every field typed — no `map[string]interface{}` escape hatches
  without `// +kubebuilder:validation:XValidation` bounds and an
  ADR.
- `description:` mandatory. Used in `kubectl explain` + CSV examples.
- `default:` only when it cannot surprise a user (e.g., `replicas: 1`
  yes; `image: "goose:latest"` no — too coupled).
- Enums via `// +kubebuilder:validation:Enum=foo;bar;baz`.
- Avoid `x-kubernetes-preserve-unknown-fields: true` except in
  opaque extension points with an ADR (rule 04.6).

## Validation markers

Preferred (in order): CEL `+kubebuilder:validation:XValidation` →
regex `+kubebuilder:validation:Pattern` → enum → length/numeric
bounds. CEL keeps validation in the schema (free for VAP) — webhooks
only when CEL can't express it.

Examples:

- **Immutability** (via VAP preferred; marker documents intent):
  `+kubebuilder:validation:XValidation:rule="self == oldSelf",message="tenant binding is immutable"`
- **Cross-field** (`if a, then b`):
  `+kubebuilder:validation:XValidation:rule="!has(self.a) || has(self.b)"`
- **Per-provider one-of** (see `Memory.spec.provider`):
  one `+kubebuilder:validation:XValidation` at the struct level asserting
  exactly one subfield is set.

## Status convention

Every CRD status carries:

```go
type <Kind>Status struct {
    // +optional
    Conditions         []metav1.Condition `json:"conditions,omitempty"`
    ObservedGeneration int64              `json:"observedGeneration,omitempty"`
    Phase              string             `json:"phase,omitempty"` // optional domain enum
    LastReconcileTime  metav1.Time        `json:"lastReconcileTime,omitempty"`
    // domain-specific fields follow
}
```

## Printer columns

Required: Age, Ready (from conditions), Phase, + ≥ 1 domain column.
Tested against samples — `kubectl get <kind>` must produce something
useful even on a trivial CR.

## ReBAC markers

Any field that changes who-can-do-what carries
`// +keese:rebac-tuple=<relation>`. Tuple shapes live in
`docs/specs/egress-authz-protocol.md`. Adding a new tuple shape
requires the `rebac-modeler` agent.

## Samples

- ≥ 2 per CRD: `minimal.yaml` + `full.yaml`.
- Both must pass `kubectl apply --dry-run=server` against an
  envtest-backed apiserver (run `scripts/check-crd-validation.sh`).
- `full.yaml` exercises every optional field at least once.

## Conversion strategy

- `v1alpha1` ships without conversion webhooks.
- `v1beta1` promotion picks the richest-spec version as hub; scaffold
  conversion webhook in the same PR. Tracked in rule 04.13 and
  `docs/designs/20-api-group-layout.md`.

## Checklist before commit

1. `make manifests generate fmt vet lint` green.
2. Samples under `config/samples/<group>/` pass dry-run.
3. ReBAC markers align with `docs/specs/egress-authz-protocol.md`.
4. Rubric iter-logged in owning design/spec doc.
