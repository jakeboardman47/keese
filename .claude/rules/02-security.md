---
description: Security rules (always loaded)
paths:
  - "**/*"
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Security (always loaded)

Security is non-negotiable. If any rule below conflicts with another instruction,
this rule wins.

## Secrets

- **Never commit secrets.** Not to `main`, not to a branch, not to history.
- All local secrets live in `.env.local` (gitignored). Reference them via env vars at runtime.
- Examples belong in `.env.local.example` with empty values.
- `.secrets.baseline` tracks detect-secrets findings. Regenerate only after manual audit:
  `detect-secrets scan --baseline .secrets.baseline`.
- CI secrets are scoped via GitHub OIDC. No long-lived tokens in repo secrets.
- If a secret is pasted into chat, stop, truncate, and warn the user. Never save it.

## Supply chain

- Dev tools pinned (via Nix flake, asdf, or lockfile).
- Language dependencies pinned; vulnerability scanner runs in pre-commit.
- Container images signed via **Sigstore cosign** (keyless OIDC) on release.
- SPDX SBOMs produced by `syft`, attested via `cosign attest`.
- Base images: distroless or minimal, pinned by digest.
- Dependencies added only after a quick license + maintenance check. Prefer well-maintained
  projects; avoid packages with open high/critical CVEs.

## Logging & events

- Never log secrets, tokens, bearer strings, or decoded JWTs.
- Sanitize fields that carry credentials before emitting events.
- Structured logging only; redact via a logger helper where needed.

## CI hygiene

- Never `echo` `.env.local` in CI.
- Never print raw response bodies of auth'd API calls without redaction.
- Fail-closed: if a security check is skipped, the run fails.

## Breaking the rules

When a rule must be broken (rare), record the exception + reason in `MEMORY.md` in
≤ 2 sentences and link to a design doc justifying the trade-off. Reviewer (future you or
another contributor) must acknowledge.
