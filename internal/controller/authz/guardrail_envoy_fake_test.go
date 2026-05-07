// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package authz

import (
	"context"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
)

// FakeEnvoyProjector is a test double for EnvoySecurityPolicyProjector.
// It records Apply/Delete calls and returns configurable errors.
type FakeEnvoyProjector struct {
	Applied []string
	Deleted []string
	// ApplyErr, if non-nil, is returned from every Apply call.
	ApplyErr error
	// DeleteErr, if non-nil, is returned from every Delete call.
	DeleteErr error
}

func (f *FakeEnvoyProjector) Apply(_ context.Context, binding *authzv1alpha1.GuardrailBinding, _ *authzv1alpha1.EffectivePolicy) error {
	if f.ApplyErr != nil {
		return f.ApplyErr
	}
	f.Applied = append(f.Applied, binding.Namespace+"/"+binding.Name)
	return nil
}

func (f *FakeEnvoyProjector) Delete(_ context.Context, ns, name string) error {
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	f.Deleted = append(f.Deleted, ns+"/"+name)
	return nil
}

var _ EnvoySecurityPolicyProjector = &FakeEnvoyProjector{}
