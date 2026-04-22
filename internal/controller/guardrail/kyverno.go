// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package guardrail

import "context"

// KyvernoPolicyProjector is the interface for applying Kyverno ClusterPolicy
// objects via SSA. The real implementation (once kyverno.io/v1 is in go.mod)
// will use the Kyverno API types; this interface keeps the controller testable
// without that dependency.
//
// TODO(spec-followup): implement RealKyvernoProjector once kyverno.io/v1 is in go.mod.
type KyvernoPolicyProjector interface {
	// Apply SSA-patches a ClusterPolicy for the given binding+policyRef pair.
	// The fieldOwner must be "keese-guardrailbinding-controller".
	// Apply is idempotent: called with the same policyRef → no-op.
	Apply(ctx context.Context, bindingNamespace, bindingName, policyRef string) error

	// Delete removes the SSA-owned ClusterPolicy copy for the given binding.
	// Missing objects are silently ignored (idempotent).
	Delete(ctx context.Context, bindingNamespace, bindingName, policyRef string) error
}

// FakeKyvernoProjector is a no-op KyvernoPolicyProjector used in tests.
type FakeKyvernoProjector struct {
	Applied []string
	Deleted []string
	// ApplyErr, if non-nil, is returned from every Apply call.
	ApplyErr error
	// DeleteErr, if non-nil, is returned from every Delete call.
	DeleteErr error
}

func (f *FakeKyvernoProjector) Apply(_ context.Context, _, _, policyRef string) error {
	if f.ApplyErr != nil {
		return f.ApplyErr
	}
	f.Applied = append(f.Applied, policyRef)
	return nil
}

func (f *FakeKyvernoProjector) Delete(_ context.Context, _, _, policyRef string) error {
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	f.Deleted = append(f.Deleted, policyRef)
	return nil
}

var _ KyvernoPolicyProjector = &FakeKyvernoProjector{}
