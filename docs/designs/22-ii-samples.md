<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: workflow
depends:
  - 22-workflow-composition-examples.md
  - 02-workspace-model.md
  - 03-workflow-argo-delegation.md
related_skills: [doc-authoring]
status: current
last_verified: 2026-04-21
rollback: samples only; no architecture changes; update in tandem with 22.
---

# 22-ii — Workflow Composition Examples: Full YAML Samples

Overflow from [22-workflow-composition-examples.md](22-workflow-composition-examples.md).
All samples target non-interactive Workspaces (`spec.interactive: false`). Argo Workflow
CRs and step pods run in the **Workspace's namespace** (03 iter-2). Per-run Secret
`keese-wf-<run-id>-creds` in Workspace namespace; owner-ref'd to Argo Workflow for GC.
Artifact path: `keese/<workspace-uid>/<run-id>/<step>/`.

## Sample A — Cron-triggered autonomous-dev (full)

```yaml
apiVersion: keese.ai/v1alpha1
kind: Workflow
metadata:
  name: autonomous-dev-nightly
  namespace: keese-acme
spec:
  workflowTemplateRef:
    name: nightly-dev-template         # Argo WorkflowTemplate in same namespace
  timeout: 2h
  triggers:
    - type: cron
      cron: { schedule: "0 2 * * *", timezone: UTC }
  outputs:
    - type: slack
      slack: { secretRef: { name: slack-webhook-dev }, channel: "#dev-autonomous" }
      on: [Succeeded, Failed, Timeout]
    - type: gh-pr
      ghPR: { repo: "keese-ai/keese", credentialRef: { name: github-pat-bsp }, baseBranch: main }
      on: [Succeeded]
---
# Argo WorkflowTemplate — same namespace (keese-acme); owner-ref'd to Workflow CR
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: nightly-dev-template
  namespace: keese-acme
spec:
  entrypoint: nightly-dev
  activeDeadlineSeconds: 7200          # mirrors spec.timeout
  ttlStrategy:
    secondsAfterCompletion: 604800     # 7-day cleanup; no namespace delete
  templates:
    - name: nightly-dev
      steps:
        - - { name: git-pull,       template: git-pull-tpl }
        - - { name: analyze-issues, template: agent-step-tpl,
              arguments: { parameters: [{ name: recipe, value: analyze-issues-recipe }] } }
        - - { name: implement-fix,  template: agent-step-tpl,
              arguments: { parameters: [{ name: recipe, value: implement-fix-recipe }] } }
        - - { name: run-tests,      template: shell-step-tpl,
              arguments: { parameters: [{ name: command, value: "make test" }] } }
        - - { name: open-pr,        template: agent-step-tpl,
              when: "{{steps.run-tests.status}} == Succeeded",
              arguments: { parameters: [{ name: recipe, value: open-pr-recipe }] } }
    - name: agent-step-tpl
      inputs:
        parameters: [{ name: recipe }]
      container:
        image: ghcr.io/keese-ai/goose-runner:latest@sha256:abc123  # digest-pinned
        env:
          - { name: RECIPE_NAME,        value: "{{inputs.parameters.recipe}}" }
          - name: ARGO_TRACE_CONTEXT    # trace propagation; see 22 §Observability
            valueFrom: { fieldRef: { fieldPath: "metadata.annotations['keese.ai/traceparent']" } }
        # No API keys — credentials via BackendSecurityPolicy (05b; rule 05.2)
```

## Sample B — NATS-fanout WorkflowRun (trigger + run)

```yaml
apiVersion: keese.ai/v1alpha1
kind: Workflow
metadata:
  name: article-summarizer
  namespace: keese-acme
spec:
  workflowTemplateRef:
    name: article-summarize-template
  triggers:
    - type: nats
      nats:
        consumerRef: { stream: article-ingest, consumer: summarizer-consumer }
        # FLAG(09): transportRef replaces consumerRef when 09 reaches current.
        dedupBucket: keese-wf-delivered-article-summarizer
        dedupTTL: 24h
        maxConcurrent: 10
  outputs:
    - type: nats-stream
      natsStream: { stream: article-summaries, subject: "summaries.{{workflow.parameters.batchId}}" }
      on: [Succeeded]
---
# WorkflowRun — created by trigger controller per Nats-Msg-Id; Workspace namespace
apiVersion: keese.ai/v1alpha1
kind: WorkflowRun
metadata:
  name: article-summarizer-run-abc123
  namespace: keese-acme
  labels: { keese.ai/workflow: article-summarizer, keese.ai/nats-msg-id: abc123 }
spec:
  workflowRef: article-summarizer
  workflowTemplateRef:
    name: article-summarize-template   # projected → Argo Workflow.spec.workflowTemplateRef.name
  parameters: { batchId: abc123, articleUrls: "[\"https://...\"]" }
status:
  phase: Succeeded
  outputs:
    - { index: 0, type: nats-stream, status: Succeeded, deliveredAt: "2026-04-21T02:15:42Z" }
  observedGeneration: 1
```

## Sample C — Webhook-triggered PR review (full)

```yaml
apiVersion: keese.ai/v1alpha1
kind: Workflow
metadata:
  name: pr-reviewer
  namespace: keese-acme
spec:
  workflowTemplateRef:
    name: pr-review-template
  triggers:
    - type: webhook
      webhook:
        path: /webhook/github/pr-reviewer
        events: [pull_request.opened, pull_request.synchronize]
        secretRef: { name: github-hmac-secret }  # FLAG(11): ExternalSecret rotation pending
  outputs:
    - type: gh-pr
      ghPR: { repo: "{{workflow.parameters.repo}}", credentialRef: { name: github-comment-bsp },
               action: review-comment }
      on: [Succeeded, Failed]
---
# WorkflowRun — created by keese-trigger-receiver in Workspace namespace
apiVersion: keese.ai/v1alpha1
kind: WorkflowRun
metadata:
  name: pr-reviewer-run-pr-4242
  namespace: keese-acme              # Workspace namespace; no ephemeral per-run namespace
spec:
  workflowRef: pr-reviewer
  workflowTemplateRef:
    name: pr-review-template
  parameters: { prNumber: "4242", sha: "d3adb33f", repo: "keese-ai/keese" }
  # retryOutputs: [0]                # selective output retry
status:
  phase: PartialSuccess              # top-level enum; not a sub-phase of Succeeded
  outputs:
    - { index: 0, type: gh-pr, status: Failed, lastError: "GitHub API rate limit", retryCount: 3 }
  observedGeneration: 2
```

## Cross-dep flag status

| Flag | Design | Status |
|---|---|---|
| `workflowTemplateRef.name` field name | 03 iter-2 | Closed — confirmed |
| Operator RBAC no namespace verbs | 03 iter-2 | Closed — confirmed |
| Same-namespace + `ttlStrategy` cleanup | 03 iter-2 | Closed — confirmed |
| NATS `consumerRef` → `transportRef` | 09 iter-1 | Open |
| `secretRef` ExternalSecret rotation | 11 iter-1 | Open |
