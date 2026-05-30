// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
	"github.com/keese-ai/keese/internal/rebac"
)

// WorkflowOpenFGARebacWriter is the Workflow + WorkflowRun ReBAC writer.
//
// Status: deferred — Workflow has no spec.workspaceRef in the current API
// (workflows route via TransportRef + NATS subjects, not Workspace
// ownership), and the OpenFGA model in dev/bootstrap/openfga/model.fga
// does not yet declare workflow / workflowrun types. Inventing a relation
// here without schema buy-in would write tuples nothing will Check.
//
// When designs 24 (Workflow) and 25 (WorkflowRun) finalize the ownership
// shape, replace these no-ops with real Write/Delete calls mirroring
// memory_rebac_openfga.go. Until then the operator runs without workflow
// authz tuples; cmd/main.go logs that the writer is wired so absence is
// observable. Tracked as a follow-on of TD-P2-02.
type WorkflowOpenFGARebacWriter struct {
	Client *rebac.Client
}

func (w *WorkflowOpenFGARebacWriter) WriteWorkflowOwner(_ context.Context, _ *keesev1alpha1.Workflow) (int32, error) {
	return 0, nil
}

func (w *WorkflowOpenFGARebacWriter) DeleteWorkflowTuples(_ context.Context, _ *keesev1alpha1.Workflow) error {
	return nil
}

func (w *WorkflowOpenFGARebacWriter) WriteWorkflowRunOwner(_ context.Context, _ *keesev1alpha1.WorkflowRun) (int32, error) {
	return 0, nil
}

func (w *WorkflowOpenFGARebacWriter) DeleteWorkflowRunTuples(_ context.Context, _ *keesev1alpha1.WorkflowRun) error {
	return nil
}

var _ WorkflowRebacWriter = (*WorkflowOpenFGARebacWriter)(nil)
