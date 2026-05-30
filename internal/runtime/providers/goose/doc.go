// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Package goose is the reference AgentRuntime SPI provider, wrapping
// the Block goose CLI (https://github.com/block/goose) shipped as the
// keese-goose:1.33.1 image (built from source — see
// dev/runtimes/goose-from-source/).
//
// This provider implements Bootstrap, Drain, Resume, Run, Attach,
// InjectPrompt, and Health. The sub-agent and streaming methods —
// InvokeSubAgent, CleanupSubAgents, and StreamEvents — return
// v1alpha1.ErrUnsupported (Goose advertises SupportsSubAgents=false and
// SupportsStreaming=false in its CapabilityMatrix) and are tracked as
// follow-on TD items.
//
// Bootstrap, Drain, and Resume work over the workspace PVC layout
// shared with the operator's WorkspaceSession reconciler:
//
//   /var/run/keese/session/                — session PVC mount
//     home/                                — $HOME for the goose container
//       .config/goose/                     — provider config + custom_providers/
//       .local/share/goose/sessions/       — session SQLite files
//     keese-checkpoints/<wsuid>/           — keese-owned checkpoint dir
//       sessions.db, sessions.db-shm, sessions.db-wal
//                                          — drain-time copy of goose's
//                                            session SQLite triple
//       last-step.json                     — last_committed_step_id (D24)
//
// preStop on the session pod writes /tmp/draining; the kubelet
// readiness probe flips NotReady (rule 06.9) so the Service stops
// routing before the drain proceeds.
package goose
