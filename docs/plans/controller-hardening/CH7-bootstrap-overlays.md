<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: plan
category: phase
depends:
  - README.md
  - ../../references/tilt-local-loop.md
  - ../../../dev/bootstrap/README.md
related_skills: [plan-management, infra-bootstrap]
status: planned
last_verified: 2026-06-10
phase: CH7
model_tier: sonnet
depends_on: []
agent: infra-bootstrap
outputs:
  - dev/bootstrap
  - config/cosign-webhook
  - Makefile
---

# CH7 — Bootstrap overlays (cosign-webhook + goose-runtime)

**Goal.** The local bootstrap (`make bootstrap-infra`) deploys **neither** the
cosign admission webhook (`config/cosign-webhook/` ships manifests but no overlay
applies them) **nor** loads the `goose-runtime` image — so the FeatureGate
admission-outcome flip (EH8) and the real-`keese-drain` run (EH10) stay gated. Wire
them so those e2e steps become runnable.

## Deliverables

1. **Apply the cosign-webhook in the dev bootstrap** — a kustomize overlay / helmfile
   release (or an `install-crds.sh`-style apply) that deploys
   `config/cosign-webhook/` (Deployment + `ValidatingWebhookConfiguration`) into the
   kind cluster as part of `make bootstrap-infra`. Pin images; fail-closed config.
2. **Seed FeatureGate CRs** — apply `config/featuregates/` (the gate-catalog seed CRs)
   so the `cosign-installplan-verify` gate exists (EH8's projection assert).
3. **`make goose-runtime-load`** — a Makefile target that builds/pulls the
   `goose-runtime` image and `kind load`s it into the e2e cluster (so EH10's real
   drain runs). Wire it (or document it) into the e2e bootstrap path.

## Acceptance

- `make bootstrap-infra` on kind brings up the cosign webhook + FeatureGate seeds
  with no error; `make goose-runtime-load` loads the image.
- Re-running EH8 (admission-flip step) and EH10 (drain step) no longer self-skip on
  the missing-precondition gate (verify the gate scripts find their preconditions).

## Notes for the agent

- Stay inside `dev/bootstrap/`, `config/cosign-webhook/`, and `Makefile`. Do **not**
  touch `config/default/bootstrap/` (CH8 owns the guardrail-binding rename),
  `internal/`, `.github/**`, conductor/, .claude/, scripts/lib/**, CLAUDE.md, MEMORY.md.
- Production-context `kubectl`/`helm install` are denied — target kind only.
- **Never run bare `git stash`/`pop`/`reset`/`checkout <branch>`** (hits the shared
  checkout). If a piece needs OLM and that's too heavy for the bootstrap, ship the
  cosign+featuregate parts, mark the OLM piece a documented follow-up, and set
  `status: shipped-with-stubs`.
