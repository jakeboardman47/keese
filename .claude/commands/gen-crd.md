---
description: Scaffold a new keese CRD via operator-sdk
argument-hint: <group> <Kind>
model: sonnet
allowed-tools:
  - Read
  - Bash
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# /gen-crd \<group\> \<Kind\>

Scaffolds a new CRD + controller stub under the keese API surface.

## What it does

1. Validates `<group>` is one of: `workspace`, `workflow`, `runtime`,
   `memory`, `recipe`, `guardrail`, `observability`, `transport`.
   Other groups require an ADR (rule 04.1) — escalate to the
   `architect` agent instead.
2. Validates the kind is not already scaffolded via
   `scripts/guard-create-api.sh`.
3. Runs:
   ```
   operator-sdk create api \
     --group=<group> \
     --version=v1alpha1 \
     --kind=<Kind> \
     --resource \
     --controller
   ```
4. Inserts the `TODO(design-gate)` sentinel and a `// +keese:rebac-tuple=...`
   marker placeholder into the generated `*_types.go`.
5. Runs `make manifests generate fmt vet` and reports drift.

## What it does NOT do

- Fill fields on the CRD — that's the `crd-author` agent's job, and
  only after `docs/designs/` for that kind is `status: current`.
- Implement the reconciler — `controller-author` after design gate.
- Write samples — `crd-author` with the skill loaded.

## Exit

Prints the PROJECT diff and the generated file list. Hands off to
the user to dispatch a `crd-author` for real population.
