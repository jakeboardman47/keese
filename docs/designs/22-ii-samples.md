<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright (c) 2026 keese-ai -->

---
scope: design
category: workflow
depends: [22-workflow-composition-examples.md]
related_skills: [doc-authoring]
status: current
last_verified: 2026-04-21
rollback: samples only; no architecture changes; update in tandem with 22.
---

# 22-ii — Workflow Composition Examples: Full YAML Samples

Overflow from [22-workflow-composition-examples.md](22-workflow-composition-examples.md).
Each sample is annotated with cross-dep flags where a stub design (03, 09, 11)
must confirm field names.

## Sample A — Cron-triggered autonomous-dev pipeline (full)

```yaml
apiVersion: workflow.operator.keese.ai/v1alpha1
kind: Workflow
metadata:
  name: autonomous-dev-nightly
  namespace: keese-acme
  annotations:
    keese.ai/description: "Nightly autonomous-dev: pull, analyze, implement, test, PR"
spec:
  # FLAG(03): field name 'workflowTemplateRef' must be confirmed by 03 iter-1.
  workflowTemplateRef: nightly-dev-template
  timeout: 2h
  concurrencyPolicy: Forbid           # no parallel runs of this Workflow
  triggers:
    - type: cron
      cron:
        schedule: "0 2 * * *"
        timezone: UTC
  outputs:
    - type: slack
      slack:
        secretRef:
          name: slack-webhook-dev     # projected from OpenBao; rule 05.7
        channel: "#dev-autonomous"
      on: [Succeeded, Failed, Timeout]
    - type: gh-pr
      ghPR:
        repo: "keese-ai/keese"
        credentialRef:
          name: github-pat-bsp        # BackendSecurityPolicy name (05b)
        baseBranch: main
      on: [Succeeded]
---
# Argo WorkflowTemplate (managed outside keese CRD; referenced by name)
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: nightly-dev-template
  namespace: keese-acme
spec:
  entrypoint: nightly-dev
  activeDeadlineSeconds: 7200        # mirrors Workflow.spec.timeout
  templates:
    - name: nightly-dev
      steps:
        - - name: git-pull
            template: git-pull-tpl
        - - name: analyze-issues
            template: agent-step-tpl
            arguments:
              parameters:
                - name: recipe
                  value: analyze-issues-recipe
        - - name: implement-fix
            template: agent-step-tpl
            arguments:
              parameters:
                - name: recipe
                  value: implement-fix-recipe
        - - name: run-tests
            template: shell-step-tpl
            arguments:
              parameters:
                - name: command
                  value: "make test"
        - - name: open-pr
            template: agent-step-tpl
            when: "{{steps.run-tests.status}} == Succeeded"
            arguments:
              parameters:
                - name: recipe
                  value: open-pr-recipe
    - name: agent-step-tpl
      inputs:
        parameters:
          - name: recipe
      container:
        image: ghcr.io/keese-ai/goose-runner:latest@sha256:abc123  # digest-pinned
        env:
          - name: RECIPE_NAME
            value: "{{inputs.parameters.recipe}}"
          - name: ARGO_TRACE_CONTEXT
            valueFrom:
              fieldRef:
                fieldPath: metadata.annotations['keese.ai/traceparent']
        # No API keys; credentials flow via BackendSecurityPolicy (05b)
```

## Sample B — NATS-fanout WorkflowRun (trigger side)

```yaml
# Workflow definition
apiVersion: workflow.operator.keese.ai/v1alpha1
kind: Workflow
metadata:
  name: article-summarizer
  namespace: keese-acme
spec:
  workflowTemplateRef: article-summarize-template  # FLAG(03)
  triggers:
    - type: nats
      nats:
        # FLAG(09): Transport CRD wraps Consumer; field names pending 09 iter-1.
        # Until 09 is current, direct Consumer reference:
        consumerRef:
          stream: article-ingest
          consumer: summarizer-consumer
        dedupBucket: keese-wf-delivered-article-summarizer  # NATS KV bucket name
        dedupTTL: 24h
        maxConcurrent: 10
  outputs:
    - type: nats-stream
      natsStream:
        stream: article-summaries
        subject: "summaries.{{workflow.parameters.batchId}}"
      on: [Succeeded]
---
# WorkflowRun (created by trigger controller per message)
apiVersion: workflow.operator.keese.ai/v1alpha1
kind: WorkflowRun
metadata:
  name: article-summarizer-run-abc123   # derived from Nats-Msg-Id
  namespace: keese-acme
  labels:
    keese.ai/workflow: article-summarizer
    keese.ai/nats-msg-id: abc123
spec:
  workflowRef: article-summarizer
  parameters:
    batchId: abc123
    articleUrls: "[\"https://...\",\"https://...\"]"
status:
  phase: Succeeded
  outputs:
    - index: 0
      type: nats-stream
      status: Succeeded
      deliveredAt: "2026-04-21T02:15:42Z"
  observedGeneration: 1
```

## Sample C — Webhook-triggered PR review (full)

```yaml
apiVersion: workflow.operator.keese.ai/v1alpha1
kind: Workflow
metadata:
  name: pr-reviewer
  namespace: keese-acme
spec:
  workflowTemplateRef: pr-review-template   # FLAG(03)
  triggers:
    - type: webhook
      webhook:
        path: /webhook/github/pr-reviewer
        events:
          - pull_request.opened
          - pull_request.synchronize
        # FLAG(11): secretRef projected from OpenBao via ExternalSecret;
        # rotation path confirmed pending 11 iter-1.
        secretRef:
          name: github-hmac-secret     # K8s Secret; projected file in receiver pod
  outputs:
    - type: gh-pr
      ghPR:
        repo: "{{workflow.parameters.repo}}"
        credentialRef:
          name: github-comment-bsp     # posts review comment via BSP (05b)
        action: review-comment
      on: [Succeeded, Failed]
---
# WorkflowRun created by keese-trigger-receiver on valid webhook event
apiVersion: workflow.operator.keese.ai/v1alpha1
kind: WorkflowRun
metadata:
  name: pr-reviewer-run-pr-4242
  namespace: keese-acme
spec:
  workflowRef: pr-reviewer
  parameters:
    prNumber: "4242"
    sha: "d3adb33f"
    repo: "keese-ai/keese"
  # Selective output retry example:
  # retryOutputs: [0]
status:
  phase: PartialSuccess
  outputs:
    - index: 0
      type: gh-pr
      status: Failed
      lastError: "GitHub API rate limit"
      retryCount: 3
  observedGeneration: 2
```

## Notes on cross-dep flags

| Flag | Blocking design | Risk if not resolved before spec |
|---|---|---|
| `workflowTemplateRef` field name | 03 iter-1 | Spec field name drift; one-time migration script |
| NATS `consumerRef` vs `transportRef` | 09 iter-1 | Trigger controller references wrong abstraction |
| `secretRef` rotation path | 11 iter-1 | Manual rotation only; no automated ExternalSecret bridge |
