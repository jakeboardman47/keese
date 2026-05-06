// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/log"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
	"github.com/keese-ai/keese/internal/rebac"
)

// WorkflowOpenFGARebacWriter is a real-OpenFGA WorkflowRebacWriter for the workflow
// controllers. See original rebac_openfga.go for stub rationale.
type WorkflowOpenFGARebacWriter struct {
	Client *rebac.Client
}

func (w *WorkflowOpenFGARebacWriter) WriteWorkflowOwner(ctx context.Context, wf *keesev1alpha1.Workflow) (int32, error) {
	log.FromContext(ctx).V(1).Info("workflow rebac stubbed",
		"op", "WriteWorkflowOwner",
		"workflow", wf.Namespace+"/"+wf.Name,
		"reason", "OpenFGA model lacks workflow type — see rebac_openfga.go")
	return 0, nil
}

func (w *WorkflowOpenFGARebacWriter) DeleteWorkflowTuples(ctx context.Context, wf *keesev1alpha1.Workflow) error {
	log.FromContext(ctx).V(1).Info("workflow rebac stubbed",
		"op", "DeleteWorkflowTuples",
		"workflow", wf.Namespace+"/"+wf.Name)
	return nil
}

func (w *WorkflowOpenFGARebacWriter) WriteWorkflowRunOwner(ctx context.Context, wfr *keesev1alpha1.WorkflowRun) (int32, error) {
	log.FromContext(ctx).V(1).Info("workflow rebac stubbed",
		"op", "WriteWorkflowRunOwner",
		"workflowrun", wfr.Namespace+"/"+wfr.Name,
		"reason", "OpenFGA model lacks workflowrun type — see rebac_openfga.go")
	return 0, nil
}

func (w *WorkflowOpenFGARebacWriter) DeleteWorkflowRunTuples(ctx context.Context, wfr *keesev1alpha1.WorkflowRun) error {
	log.FromContext(ctx).V(1).Info("workflow rebac stubbed",
		"op", "DeleteWorkflowRunTuples",
		"workflowrun", wfr.Namespace+"/"+wfr.Name)
	return nil
}

var _ WorkflowRebacWriter = (*WorkflowOpenFGARebacWriter)(nil)
