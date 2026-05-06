// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// BackendProvisioner abstracts provider-specific provisioning so that
// integration tests can inject a fake without a live Redis/Qdrant/etc.
//
// Each method is idempotent: calling Provision on an already-provisioned backend
// must be a no-op (converge in ≤3 reconciles, rule 04.16).
type BackendProvisioner interface {
	// Provision ensures the backend resource exists (PVC, collection, schema, etc.).
	// Returns true if a new resource was created (false = already existed).
	Provision(ctx context.Context, provider keesev1alpha1.MemoryProvider, name, namespace string) (created bool, err error)

	// Deprovision removes the backend resource.
	// Missing resources are silently ignored (idempotent).
	Deprovision(ctx context.Context, provider keesev1alpha1.MemoryProvider, name, namespace string) error

	// Healthy returns true if the backend can be reached.
	// Called during every reconcile to drive phase transitions.
	Healthy(ctx context.Context, provider keesev1alpha1.MemoryProvider, name, namespace string) (bool, error)
}

// FakeBackendProvisioner is an in-memory BackendProvisioner for tests.
// It records calls and returns configurable responses.
type FakeBackendProvisioner struct {
	// provisioned tracks names of provisioned backends.
	provisioned map[string]bool

	// ProvisionErr, if non-nil, is returned by Provision.
	ProvisionErr error

	// DeprovisionErr, if non-nil, is returned by Deprovision.
	DeprovisionErr error

	// HealthyResult is returned by Healthy.
	HealthyResult bool

	// HealthyErr, if non-nil, is returned by Healthy.
	HealthyErr error

	// ProvisionCalls records (name, namespace) pairs passed to Provision.
	ProvisionCalls []string

	// DeprovisionCalls records (name, namespace) pairs passed to Deprovision.
	DeprovisionCalls []string
}

// NewFakeBackendProvisioner returns a FakeBackendProvisioner ready for use.
// HealthyResult defaults to true so that happy-path tests don't need extra setup.
func NewFakeBackendProvisioner() *FakeBackendProvisioner {
	return &FakeBackendProvisioner{
		provisioned:   make(map[string]bool),
		HealthyResult: true,
	}
}

// Reset clears accumulated state and error overrides. Call in BeforeEach.
func (f *FakeBackendProvisioner) Reset() {
	f.provisioned = make(map[string]bool)
	f.ProvisionErr = nil
	f.DeprovisionErr = nil
	f.HealthyResult = true
	f.HealthyErr = nil
	f.ProvisionCalls = nil
	f.DeprovisionCalls = nil
}

func backendKey(name, namespace string) string {
	return namespace + "/" + name
}

// Provision implements BackendProvisioner.
func (f *FakeBackendProvisioner) Provision(_ context.Context, _ keesev1alpha1.MemoryProvider, name, namespace string) (bool, error) {
	if f.ProvisionErr != nil {
		return false, f.ProvisionErr
	}
	key := backendKey(name, namespace)
	f.ProvisionCalls = append(f.ProvisionCalls, key)
	if f.provisioned[key] {
		return false, nil
	}
	f.provisioned[key] = true
	return true, nil
}

// Deprovision implements BackendProvisioner.
func (f *FakeBackendProvisioner) Deprovision(_ context.Context, _ keesev1alpha1.MemoryProvider, name, namespace string) error {
	if f.DeprovisionErr != nil {
		return f.DeprovisionErr
	}
	key := backendKey(name, namespace)
	f.DeprovisionCalls = append(f.DeprovisionCalls, key)
	delete(f.provisioned, key)
	return nil
}

// Healthy implements BackendProvisioner.
func (f *FakeBackendProvisioner) Healthy(_ context.Context, _ keesev1alpha1.MemoryProvider, name, namespace string) (bool, error) {
	if f.HealthyErr != nil {
		return false, f.HealthyErr
	}
	if !f.HealthyResult {
		return false, nil
	}
	key := backendKey(name, namespace)
	return f.provisioned[key], nil
}

// validateHA returns an error if the provider requires HA replicas outside a dev
// namespace but is configured with replicas < 2. This is the controller-side
// enforcement that mirrors the MemoryHARequired VAP.
//
// TODO(spec-followup): Once the VAP is applied cluster-wide, this defence-in-depth
// check can be relaxed but should remain for belt-and-suspenders.
func validateHA(provider keesev1alpha1.MemoryProvider, namespace string) error {
	if isDevNamespace(namespace) {
		return nil
	}
	switch provider.Type {
	case keesev1alpha1.ProviderRedis:
		if provider.Redis != nil && provider.Redis.Replicas < 2 {
			return fmt.Errorf(
				"redis provider requires replicas ≥ 2 outside dev namespaces (MemoryHARequired), got %d",
				provider.Redis.Replicas,
			)
		}
	case keesev1alpha1.ProviderQdrant:
		if provider.Qdrant != nil && provider.Qdrant.Replicas < 2 {
			return fmt.Errorf(
				"qdrant provider requires replicas ≥ 2 outside dev namespaces (MemoryHARequired), got %d",
				provider.Qdrant.Replicas,
			)
		}
	}
	return nil
}

// isDevNamespace returns true for namespaces that are exempt from the HA requirement.
// Convention: a namespace is "dev" if it has the suffix "-dev" or is named "default".
//
// TODO(spec-followup): Replace heuristic with a namespace label selector once the
// Namespace labelling convention lands in a design doc.
func isDevNamespace(ns string) bool {
	return ns == "default" || len(ns) >= 4 && ns[len(ns)-4:] == "-dev"
}
