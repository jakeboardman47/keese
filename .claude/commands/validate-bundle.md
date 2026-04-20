---
description: Regenerate + validate the OLM bundle
model: sonnet
allowed-tools:
  - Read
  - Bash
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# /validate-bundle

Regenerates the OLM bundle and runs the full `bundle validate` suite.

## What it does

1. `make manifests generate` — fails if drift.
2. `make bundle` — runs `operator-sdk generate bundle` (channels
   alpha) and commits nothing.
3. `make bundle-validate` — runs
   `operator-sdk bundle validate --select-optional
   suite=operatorframework`.
4. Reports every warning or error with file + line pointer and exits
   non-zero if validation fails.

## When to run

- Before every OLM-related commit.
- Before opening a release PR (release-please triggers this via
  `bundle.yaml` workflow, but local runs catch drift earlier).
- After any edit to `api/**`, `config/rbac/**`,
  `config/manifests/**`, or `config/webhook/**` — each can change the
  generated CSV.

## What it does NOT do

- Push the bundle image (CI only).
- Sign the bundle image with cosign (CI only).
- Promote channels — that's the `olm-author` agent via the release
  pipeline.
