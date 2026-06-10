// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package keese

// newFakes creates fresh fake dependencies for each Workflow / WorkflowRun spec.
// The envtest harness itself lives in suite_test.go (shared across all keese
// reconcilers).
func newFakes() (*FakeArgoProjector, *FakeNatsStreamProvisioner, *FakeNatsStreamDeleter, *WorkflowFakeRebacWriter, *FakeWorkflowCTAResolver) {
	return &FakeArgoProjector{
			StatusByName: map[string]*ArgoWorkflowStatus{},
		},
		&FakeNatsStreamProvisioner{},
		&FakeNatsStreamDeleter{},
		&WorkflowFakeRebacWriter{TupleCount: 2},
		&FakeWorkflowCTAResolver{}
}
