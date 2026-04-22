// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package workflow

import (
	"context"

	workflowv1alpha1 "github.com/keese-ai/keese/api/workflow/v1alpha1"
)

// OpenFGATuple is a minimal representation of an OpenFGA relationship tuple.
type OpenFGATuple struct {
	// User is the OpenFGA user string (e.g. "user:<uid>").
	User string
	// Relation is the OpenFGA relation name (e.g. "owner").
	Relation string
	// Object is the OpenFGA object string (e.g. "workflow:<uid>").
	Object string
}

// RebacWriter syncs OpenFGA tuples for Workflow and WorkflowRun resources.
// The real implementation calls the OpenFGA gRPC API; tests use FakeRebacWriter.
type RebacWriter interface {
	// WriteWorkflowOwner writes (or idempotently updates) the owner tuple
	// for a Workflow resource.
	WriteWorkflowOwner(ctx context.Context, wf *workflowv1alpha1.Workflow) (int32, error)

	// DeleteWorkflowTuples removes all tuples associated with a Workflow.
	DeleteWorkflowTuples(ctx context.Context, wf *workflowv1alpha1.Workflow) error

	// WriteWorkflowRunOwner writes (or idempotently updates) the owner tuple
	// for a WorkflowRun resource.
	WriteWorkflowRunOwner(ctx context.Context, wfr *workflowv1alpha1.WorkflowRun) (int32, error)

	// DeleteWorkflowRunTuples removes all tuples associated with a WorkflowRun.
	DeleteWorkflowRunTuples(ctx context.Context, wfr *workflowv1alpha1.WorkflowRun) error
}

// FakeRebacWriter is a test-only RebacWriter that records calls.
type FakeRebacWriter struct {
	// WrittenWorkflowTuples accumulates WriteWorkflowOwner calls.
	WrittenWorkflowTuples []*workflowv1alpha1.Workflow
	// DeletedWorkflowTuples accumulates DeleteWorkflowTuples calls.
	DeletedWorkflowTuples []*workflowv1alpha1.Workflow
	// WrittenWorkflowRunTuples accumulates WriteWorkflowRunOwner calls.
	WrittenWorkflowRunTuples []*workflowv1alpha1.WorkflowRun
	// DeletedWorkflowRunTuples accumulates DeleteWorkflowRunTuples calls.
	DeletedWorkflowRunTuples []*workflowv1alpha1.WorkflowRun
	// TupleCount is the count returned by Write* calls.
	TupleCount int32
	// Err is returned by all calls when non-nil.
	Err error
}

// WriteWorkflowOwner records the call and returns FakeRebacWriter.TupleCount.
func (f *FakeRebacWriter) WriteWorkflowOwner(_ context.Context, wf *workflowv1alpha1.Workflow) (int32, error) {
	if f.Err != nil {
		return 0, f.Err
	}
	f.WrittenWorkflowTuples = append(f.WrittenWorkflowTuples, wf)
	return f.TupleCount, nil
}

// DeleteWorkflowTuples records the call.
func (f *FakeRebacWriter) DeleteWorkflowTuples(_ context.Context, wf *workflowv1alpha1.Workflow) error {
	if f.Err != nil {
		return f.Err
	}
	f.DeletedWorkflowTuples = append(f.DeletedWorkflowTuples, wf)
	return nil
}

// WriteWorkflowRunOwner records the call and returns FakeRebacWriter.TupleCount.
func (f *FakeRebacWriter) WriteWorkflowRunOwner(_ context.Context, wfr *workflowv1alpha1.WorkflowRun) (int32, error) {
	if f.Err != nil {
		return 0, f.Err
	}
	f.WrittenWorkflowRunTuples = append(f.WrittenWorkflowRunTuples, wfr)
	return f.TupleCount, nil
}

// DeleteWorkflowRunTuples records the call.
func (f *FakeRebacWriter) DeleteWorkflowRunTuples(_ context.Context, wfr *workflowv1alpha1.WorkflowRun) error {
	if f.Err != nil {
		return f.Err
	}
	f.DeletedWorkflowRunTuples = append(f.DeletedWorkflowRunTuples, wfr)
	return nil
}

// Verify FakeRebacWriter satisfies the interface at compile time.
var _ RebacWriter = (*FakeRebacWriter)(nil)
