// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Package extauth implements the keese-authz Envoy ext_authz path:
// HTTP request → ToolBinding/WorkspaceTool match → OpenFGA Check →
// allow/deny + injected response headers.
//
// The package splits responsibility:
//
//   - resolver.go: compiles ToolBinding + WorkspaceTool CRs into an
//     in-memory routing trie. Held in atomic.Value so the gRPC
//     server reads lock-free.
//   - match.go: HTTPRouteMatch subset evaluator (path / method /
//     headers / query params + body discriminator).
//   - subject.go: extracts user + workspace from the Envoy
//     CheckRequest (SA token sub claim or named JWT claim).
//   - check.go: orchestrates Resolve + Subject + rebac.Client.Check
//     and returns the Envoy CheckResponse.
//   - audit.go: structured logging with strict redaction (rule 02
//     + spec §10: never tokens, never bodies).
//
// Spec: docs/specs/egress-authz-protocol.md.
// Design: docs/designs/22-egress-toolbinding.md.
package extauth
