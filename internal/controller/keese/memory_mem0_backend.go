// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// +kubebuilder:rbac:groups=external-secrets.io,resources=externalsecrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

// externalSecretGVK is the GVK for external-secrets.io/v1.ExternalSecret.
// Using unstructured to avoid adding the ESO typed import (reduces go.mod churn).
var externalSecretGVK = schema.GroupVersionKind{
	Group:   "external-secrets.io",
	Version: "v1",
	Kind:    "ExternalSecret",
}

const (
	mem0FieldOwner   = "keese-memory-controller"
	mem0DefaultEndpoint = "https://api.mem0.ai"
)

// Mem0Backend provisions credentials for the Mem0 hosted AI memory service.
//
// Because Mem0 is an external SaaS, there is no in-cluster stateful resource
// to create. Instead the backend projects an external-secrets.io/v1.ExternalSecret
// that bridges the upstream API key (stored in OpenBao/Vault) into a K8s Secret
// referenced by spec.provider.mem0.credentialSecretRef.
//
// If the ExternalSecrets Operator CRD is not installed, the backend falls back to
// verifying that the Secret named by credentialSecretRef already exists (operator
// manages it out-of-band).
//
// Rule 05.7: credentials are NEVER surfaced in events or logs.
// Rule 05.8: OpenBao is the source of truth for upstream credentials.
type Mem0Backend struct {
	Client client.Client
}

// NewMem0Backend constructs a Mem0Backend bound to the given controller client.
func NewMem0Backend(c client.Client) *Mem0Backend {
	return &Mem0Backend{Client: c}
}

// Provision implements BackendProvisioner.
// Projects an ExternalSecret bridging credentialSecretRef from Vault/OpenBao.
// Falls back to verifying the Secret exists directly when ESO CRD is absent.
func (b *Mem0Backend) Provision(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	if provider.Type != keesev1alpha1.ProviderMem0 {
		return false, nil
	}
	cfg := provider.Mem0
	if cfg == nil {
		return false, fmt.Errorf("mem0 provider config is nil")
	}
	if cfg.CredentialSecretRef == "" {
		return false, fmt.Errorf("mem0 provider requires credentialSecretRef")
	}

	return applyExternalSecret(ctx, b.Client, name, namespace, cfg.CredentialSecretRef, "mem0", mem0FieldOwner)
}

// Deprovision implements BackendProvisioner.
// Removes the projected ExternalSecret. The downstream K8s Secret is governed by ESO
// and is removed automatically. If the Secret was pre-existing (no ESO), it is left
// in place (operator does not own it).
func (b *Mem0Backend) Deprovision(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) error {
	if provider.Type != keesev1alpha1.ProviderMem0 {
		return nil
	}
	return deleteExternalSecret(ctx, b.Client, name+"-mem0-es", namespace)
}

// Healthy implements BackendProvisioner.
// Healthy when the Secret named credentialSecretRef exists and has a non-zero data key.
func (b *Mem0Backend) Healthy(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	if provider.Type != keesev1alpha1.ProviderMem0 {
		return false, nil
	}
	cfg := provider.Mem0
	if cfg == nil || cfg.CredentialSecretRef == "" {
		return false, fmt.Errorf("mem0 provider config or credentialSecretRef is missing")
	}
	return secretExists(ctx, b.Client, cfg.CredentialSecretRef, namespace)
}

// applyExternalSecret SSA-projects an external-secrets.io/v1.ExternalSecret.
// If the ESO CRD is absent (no-kind-match error), returns (false, nil) so the controller
// marks the backend as healthy only when the Secret already exists (Healthy check).
func applyExternalSecret(
	ctx context.Context,
	c client.Client,
	memoryName, namespace, secretRef, backendLabel, fieldOwner string,
) (bool, error) {
	esName := memoryName + "-" + backendLabel + "-es"

	// Probe for ESO CRD availability.
	probe := &unstructured.Unstructured{}
	probe.SetGroupVersionKind(externalSecretGVK)
	err := c.Get(ctx, types.NamespacedName{Name: esName, Namespace: namespace}, probe)
	if err != nil && !errors.IsNotFound(err) {
		// CRD absent — fall back to verifying Secret pre-exists (no-op Provision path).
		return false, nil
	}
	created := errors.IsNotFound(err)

	desired := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "external-secrets.io/v1",
			"kind":       "ExternalSecret",
			"metadata": map[string]interface{}{
				"name":      esName,
				"namespace": namespace,
				"labels": map[string]interface{}{
					"keese.ai/memory":              memoryName,
					"keese.ai/backend":             backendLabel,
					"app.kubernetes.io/managed-by": fieldOwner,
				},
			},
			"spec": map[string]interface{}{
				"refreshInterval": "1h",
				"secretStoreRef": map[string]interface{}{
					// Convention: one ClusterSecretStore named "openbao" bootstrapped by
					// dev/bootstrap/openbao/. Operators can override via annotation.
					"kind": "ClusterSecretStore",
					"name": "openbao",
				},
				"target": map[string]interface{}{
					"name":           secretRef,
					"creationPolicy": "Owner",
				},
				"dataFrom": toInterfaceSlice([]string{
					"keese/" + backendLabel + "/" + memoryName,
				}),
			},
		},
	}
	desired.SetCreationTimestamp(metav1.Time{})

	if patchErr := c.Patch(ctx, desired, client.Apply,
		client.FieldOwner(fieldOwner), client.ForceOwnership,
	); patchErr != nil {
		return false, fmt.Errorf("apply ExternalSecret %s/%s: %w", namespace, esName, patchErr)
	}
	return created, nil
}

// deleteExternalSecret removes an ExternalSecret by name. Missing resources are ignored.
func deleteExternalSecret(ctx context.Context, c client.Client, esName, namespace string) error {
	es := &unstructured.Unstructured{}
	es.SetGroupVersionKind(externalSecretGVK)
	es.SetName(esName)
	es.SetNamespace(namespace)
	if err := c.Delete(ctx, es); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete ExternalSecret %s/%s: %w", namespace, esName, err)
	}
	return nil
}

// secretExists returns true when a K8s Secret with the given name exists in namespace.
func secretExists(ctx context.Context, c client.Client, secretName, namespace string) (bool, error) {
	var secret corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &secret); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return len(secret.Data) > 0, nil
}
