// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Package goose is the reference AgentRuntime SPI provider, wrapping
// the Block goose CLI (https://github.com/block/goose) shipped as the
// keese-goose:1.33.1 image (built from source — see
// dev/runtimes/goose-from-source/).
//
// Today this provider implements the lifecycle SPI methods Bootstrap,
// Drain, and Resume — the TD-P1-02 minimum needed for the
// preStop-hook + checkpoint-on-SIGTERM contract from D18 process-
// lifecycle. Run, Attach, InjectPrompt, InvokeSubAgent, Health, and
// StreamEvents return v1alpha1.ErrUnsupported and are tracked as
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
