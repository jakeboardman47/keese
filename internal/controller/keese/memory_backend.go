// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// namespaceLabels fetches the labels of the named Namespace. Returns nil on
// any error; callers fall back to the name-suffix heuristic in isDevNamespace
// rather than failing the reconcile on a transient Namespace Get.
func namespaceLabels(ctx context.Context, c client.Client, name string) map[string]string {
	var ns corev1.Namespace
	if err := c.Get(ctx, client.ObjectKey{Name: name}, &ns); err != nil {
		return nil
	}
	return ns.Labels
}

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
// enforcement that mirrors the MemoryHARequired VAP — a defence-in-depth check
// that stays in even after the VAP is cluster-wide.
//
// nsLabels are the Namespace's labels (pass nil to skip label inspection and
// fall back to the name heuristic).
func validateHA(provider keesev1alpha1.MemoryProvider, namespace string, nsLabels map[string]string) error {
	if isDevNamespace(nsLabels, namespace) {
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

// devNamespaceLabel is the namespace label whose value="dev" exempts the
// namespace from the HA requirement. The label is set by Capsule's
// namespaceOptions.additionalMetadata on tenant namespaces (see
// dev/samples/tenant-alpha.yaml).
const devNamespaceLabel = "keese.ai/env"

// isDevNamespace reports dev-ness from the namespace's labels map, falling
// back to a name heuristic when the label is absent. Reconcilers fetch the
// Namespace object and pass labels directly; tests can pass nil to exercise
// the name-suffix path.
func isDevNamespace(labels map[string]string, name string) bool {
	if v, ok := labels[devNamespaceLabel]; ok {
		return v == "dev"
	}
	return name == "default" || (len(name) >= 4 && name[len(name)-4:] == "-dev")
}
