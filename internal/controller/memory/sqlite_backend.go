// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package memory

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	memoryv1alpha1 "github.com/keese-ai/keese/api/memory/v1alpha1"
)

const (
	sqliteFieldOwner   = "keese-memory-controller"
	sqlitePVCSuffix    = "-memory"
	sqliteDefaultSize  = "1Gi"
	sqliteDefaultClass = "" // empty == cluster default
)

// SQLiteBackend provisions a single ReadWriteOnce PVC per Memory CR for the
// sqlite backend. Non-sqlite providers are deferred to TD-P2-12.
type SQLiteBackend struct {
	Client client.Client
}

// NewSQLiteBackend constructs a SQLiteBackend bound to the given controller client.
func NewSQLiteBackend(c client.Client) *SQLiteBackend {
	return &SQLiteBackend{Client: c}
}

// Provision implements BackendProvisioner. For sqlite it SSA-applies a PVC
// named "<memory-name>-memory" sized per spec.provider.sqlite.storageSize
// (default 1Gi). For other providers it returns a "not yet implemented" error
// so the controller can mark Degraded and the user sees a clear reason.
func (s *SQLiteBackend) Provision(
	ctx context.Context,
	provider memoryv1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	if provider.Type != memoryv1alpha1.ProviderSQLite {
		return false, fmt.Errorf(
			"backend provisioner only supports sqlite at v1alpha1 demo; got %q (TD-P2-12)",
			provider.Type)
	}
	cfg := provider.SQLite
	storageSize := sqliteDefaultSize
	if cfg != nil && cfg.StorageSize != "" {
		storageSize = cfg.StorageSize
	}
	q, err := resource.ParseQuantity(storageSize)
	if err != nil {
		return false, fmt.Errorf("invalid sqlite storageSize %q: %w", storageSize, err)
	}

	pvcName := name + sqlitePVCSuffix
	desired := buildSQLitePVC(name, namespace, pvcName, q, cfg)

	// Check if it already exists to compute the created return value.
	var existing corev1.PersistentVolumeClaim
	err = s.Client.Get(ctx, client.ObjectKey{Name: pvcName, Namespace: namespace}, &existing)
	created := errors.IsNotFound(err)

	if patchErr := s.Client.Patch(ctx, desired, client.Apply,
		client.FieldOwner(sqliteFieldOwner),
		client.ForceOwnership); patchErr != nil {
		return false, fmt.Errorf("apply sqlite PVC %s/%s: %w", namespace, pvcName, patchErr)
	}
	return created, nil
}

// Deprovision implements BackendProvisioner. Honors spec.provider.sqlite.reclaimPolicy:
//   - Retain (default) — leaves the PVC in place; only the Memory CR is removed.
//   - Delete — removes the PVC.
func (s *SQLiteBackend) Deprovision(
	ctx context.Context,
	provider memoryv1alpha1.MemoryProvider,
	name, namespace string,
) error {
	if provider.Type != memoryv1alpha1.ProviderSQLite {
		return nil // non-sqlite never provisioned anything via this backend
	}
	policy := "Retain"
	if provider.SQLite != nil && provider.SQLite.ReclaimPolicy != "" {
		policy = provider.SQLite.ReclaimPolicy
	}
	if policy == "Retain" {
		return nil
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + sqlitePVCSuffix,
			Namespace: namespace,
		},
	}
	if err := s.Client.Delete(ctx, pvc); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete sqlite PVC %s/%s: %w", namespace, pvc.Name, err)
	}
	return nil
}

// Healthy implements BackendProvisioner. The PVC is "healthy" once it has
// reached phase Bound; before that we report unhealthy so the controller
// keeps the Memory CR in Provisioning/Degraded.
func (s *SQLiteBackend) Healthy(
	ctx context.Context,
	provider memoryv1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	if provider.Type != memoryv1alpha1.ProviderSQLite {
		return false, nil
	}
	var pvc corev1.PersistentVolumeClaim
	err := s.Client.Get(ctx, client.ObjectKey{
		Name:      name + sqlitePVCSuffix,
		Namespace: namespace,
	}, &pvc)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return pvc.Status.Phase == corev1.ClaimBound, nil
}

// buildSQLitePVC constructs the desired PVC object in apply form.
func buildSQLitePVC(
	memoryName, namespace, pvcName string,
	storage resource.Quantity,
	cfg *memoryv1alpha1.SQLiteConfig,
) *corev1.PersistentVolumeClaim {
	storageClass := sqliteDefaultClass
	if cfg != nil && cfg.StorageClassName != "" {
		storageClass = cfg.StorageClassName
	}
	pvc := &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "PersistentVolumeClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespace,
			Labels: map[string]string{
				"keese.ai/memory":              memoryName,
				"app.kubernetes.io/managed-by": sqliteFieldOwner,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: storage},
			},
		},
	}
	// Empty StorageClassName means "use default" — leave nil so SSA does not pin it.
	if storageClass != "" {
		pvc.Spec.StorageClassName = &storageClass
	}
	return pvc
}
