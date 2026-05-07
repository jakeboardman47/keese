// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// WorkflowOpenFGATuple is a minimal representation of an OpenFGA relationship tuple.
type WorkflowOpenFGATuple struct {
	// User is the OpenFGA user string (e.g. "user:<uid>").
	User string
	// Relation is the OpenFGA relation name (e.g. "owner").
	Relation string
	// Object is the OpenFGA object string (e.g. "workflow:<uid>").
	Object string
}

// WorkflowRebacWriter syncs OpenFGA tuples for Workflow and WorkflowRun resources.
// The real implementation (WorkflowOpenFGARebacWriter) is wired at startup via
// cmd/main.go when OPENFGA_API_URL is set. Tests inject a fake writer (see
// workflow_rebac_fake_test.go). When OpenFGA is unconfigured, WorkflowNoopRebacWriter
// is used as the fallback.
type WorkflowRebacWriter interface {
	// WriteWorkflowOwner writes (or idempotently updates) the owner tuple
	// for a Workflow resource.
	WriteWorkflowOwner(ctx context.Context, wf *keesev1alpha1.Workflow) (int32, error)

	// DeleteWorkflowTuples removes all tuples associated with a Workflow.
	DeleteWorkflowTuples(ctx context.Context, wf *keesev1alpha1.Workflow) error

	// WriteWorkflowRunOwner writes (or idempotently updates) the owner tuple
	// for a WorkflowRun resource.
	WriteWorkflowRunOwner(ctx context.Context, wfr *keesev1alpha1.WorkflowRun) (int32, error)

	// DeleteWorkflowRunTuples removes all tuples associated with a WorkflowRun.
	DeleteWorkflowRunTuples(ctx context.Context, wfr *keesev1alpha1.WorkflowRun) error
}

// WorkflowNoopRebacWriter is a silent no-op WorkflowRebacWriter used when OpenFGA
// is not configured (dev/local run without OPENFGA_API_URL). It does not record calls.
type WorkflowNoopRebacWriter struct{}

func (WorkflowNoopRebacWriter) WriteWorkflowOwner(_ context.Context, _ *keesev1alpha1.Workflow) (int32, error) {
	return 0, nil
}
func (WorkflowNoopRebacWriter) DeleteWorkflowTuples(_ context.Context, _ *keesev1alpha1.Workflow) error {
	return nil
}
func (WorkflowNoopRebacWriter) WriteWorkflowRunOwner(_ context.Context, _ *keesev1alpha1.WorkflowRun) (int32, error) {
	return 0, nil
}
func (WorkflowNoopRebacWriter) DeleteWorkflowRunTuples(_ context.Context, _ *keesev1alpha1.WorkflowRun) error {
	return nil
}

var _ WorkflowRebacWriter = WorkflowNoopRebacWriter{}
