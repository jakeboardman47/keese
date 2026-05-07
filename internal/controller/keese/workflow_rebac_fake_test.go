// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// WorkflowFakeRebacWriter is a test-only WorkflowRebacWriter that records calls.
type WorkflowFakeRebacWriter struct {
	// WrittenWorkflowTuples accumulates WriteWorkflowOwner calls.
	WrittenWorkflowTuples []*keesev1alpha1.Workflow
	// DeletedWorkflowTuples accumulates DeleteWorkflowTuples calls.
	DeletedWorkflowTuples []*keesev1alpha1.Workflow
	// WrittenWorkflowRunTuples accumulates WriteWorkflowRunOwner calls.
	WrittenWorkflowRunTuples []*keesev1alpha1.WorkflowRun
	// DeletedWorkflowRunTuples accumulates DeleteWorkflowRunTuples calls.
	DeletedWorkflowRunTuples []*keesev1alpha1.WorkflowRun
	// TupleCount is the count returned by Write* calls.
	TupleCount int32
	// Err is returned by all calls when non-nil.
	Err error
}

// WriteWorkflowOwner records the call and returns WorkflowFakeRebacWriter.TupleCount.
func (f *WorkflowFakeRebacWriter) WriteWorkflowOwner(_ context.Context, wf *keesev1alpha1.Workflow) (int32, error) {
	if f.Err != nil {
		return 0, f.Err
	}
	f.WrittenWorkflowTuples = append(f.WrittenWorkflowTuples, wf)
	return f.TupleCount, nil
}

// DeleteWorkflowTuples records the call.
func (f *WorkflowFakeRebacWriter) DeleteWorkflowTuples(_ context.Context, wf *keesev1alpha1.Workflow) error {
	if f.Err != nil {
		return f.Err
	}
	f.DeletedWorkflowTuples = append(f.DeletedWorkflowTuples, wf)
	return nil
}

// WriteWorkflowRunOwner records the call and returns WorkflowFakeRebacWriter.TupleCount.
func (f *WorkflowFakeRebacWriter) WriteWorkflowRunOwner(_ context.Context, wfr *keesev1alpha1.WorkflowRun) (int32, error) {
	if f.Err != nil {
		return 0, f.Err
	}
	f.WrittenWorkflowRunTuples = append(f.WrittenWorkflowRunTuples, wfr)
	return f.TupleCount, nil
}

// DeleteWorkflowRunTuples records the call.
func (f *WorkflowFakeRebacWriter) DeleteWorkflowRunTuples(_ context.Context, wfr *keesev1alpha1.WorkflowRun) error {
	if f.Err != nil {
		return f.Err
	}
	f.DeletedWorkflowRunTuples = append(f.DeletedWorkflowRunTuples, wfr)
	return nil
}

// Verify WorkflowFakeRebacWriter satisfies the interface at compile time.
var _ WorkflowRebacWriter = (*WorkflowFakeRebacWriter)(nil)
