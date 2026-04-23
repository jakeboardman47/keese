// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package workspace

// Event reason constants for the Workspace and WorkspaceShare controllers.
// All recorder.Eventf calls MUST use one of these constants — no free-text reasons.
// See .claude/rules/04-kubernetes.md §11.
const (
	// Workspace lifecycle events.
	ReasonWorkspaceProvisioned = "WorkspaceProvisioned"
	ReasonWorkspaceReady       = "WorkspaceReady"
	ReasonWorkspaceIdle        = "WorkspaceIdle"
	ReasonWorkspaceEvicted     = "WorkspaceEvicted"
	ReasonWorkspaceTerminating = "WorkspaceTerminating"

	// Sub-resource provisioning events.
	ReasonNetworkPolicyEnsured  = "NetworkPolicyEnsured"
	ReasonServiceAccountEnsured = "ServiceAccountEnsured"
	ReasonPVCEnsured            = "PVCEnsured"

	// ReBAC events.
	ReasonRebacTupleWritten     = "RebacTupleWritten"
	ReasonRebacTupleDeleteFailed = "RebacTupleDeleteFailed"

	// Runtime bootstrap.
	ReasonRuntimeBootstrapFailed = "RuntimeBootstrapFailed"

	// WorkspaceShare events.
	ReasonShareReferenceGrantEnsured  = "ShareReferenceGrantEnsured"
	ReasonShareRebacTupleWritten      = "ShareRebacTupleWritten"
	ReasonShareRebacTupleDeleteFailed = "ShareRebacTupleDeleteFailed"

	// WorkspaceSession lifecycle events.
	ReasonSessionAttaching                          = "SessionAttaching"
	ReasonSessionActive                             = "SessionActive"
	ReasonSessionDraining                           = "SessionDraining"
	ReasonSessionEvicted                            = "SessionEvicted"
	ReasonSessionAttachRejectedNonInteractive       = "SessionAttachRejectedNonInteractive"
	ReasonSessionDuplicate                          = "SessionDuplicate"
	ReasonSessionPodProvisioned                     = "SessionPodProvisioned"
	ReasonSessionPodTornDown                        = "SessionPodTornDown"
	ReasonSessionAttachedByTupleWritten             = "SessionAttachedByTupleWritten"
)
