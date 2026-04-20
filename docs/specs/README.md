<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) {{YEAR}} {{ORG_NAME}} -->

---
scope: reference
category: index
depends: [../designs/README.md]
related_skills: [doc-authoring]
status: draft
last_verified: {{LAST_VERIFIED}}
---

# specs/ — WHAT (testable)

Specs describe **what** the project does, in terms concrete enough to be parsed by
test harnesses. Each spec is keyed to a design doc and consumed by one or more plans.

## Spec format

Each spec has YAML frontmatter and a machine-parsable body. Reachability matrices,
contract tables, and other structured data should be formatted so automated
harnesses can consume them directly — see
[../references/documentation-system.md](../references/documentation-system.md).

## Lifecycle

Testable. A spec whose test does not exist is **draft**. Once the test exists and
passes, the spec is **implemented**.
