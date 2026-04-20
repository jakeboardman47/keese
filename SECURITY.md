<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

# Security Policy

## Reporting a Vulnerability

Please report security issues privately to **security@keese.ai**. Do not open
a public GitHub issue for vulnerabilities.

Include:

- A description of the issue.
- Reproduction steps or proof-of-concept.
- The affected version, commit, or branch.
- Any known mitigations.

We'll acknowledge within **3 business days** and send a triage update within **7
business days**.

## Severity

| Severity | Example | Initial response |
|---|---|---|
| Critical | Remote code execution, privilege escalation, secret exposure | 24h |
| High | Authenticated RCE, broken auth, data leak | 3 business days |
| Medium | Denial-of-service, info disclosure with limited blast radius | 7 business days |
| Low | Best-practice drift, minor config hardening | Next scheduled release |

## Secrets

- Never commit secrets. Every repo uses `.env.local` (gitignored) for local values;
  `.secrets.baseline` + `detect-secrets` + `gitleaks` enforce this pre-commit.
- If you find a leaked secret in git history, **rotate the credential first**, then open a
  private report — do not force-push without coordination.

## Supported Versions

See the project's tagged releases. Security fixes are backported to the current minor and
the previous minor.

## Disclosure

Once a fix ships, we publish a CVE (when applicable) and credit the reporter unless they
request anonymity.
