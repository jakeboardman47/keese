// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete

const (
	pgvectorFieldOwner        = "keese-memory-controller"
	pgvectorFallbackImage     = "pgvector/pgvector:pg17"
	pgvectorPort              = int32(5432)
	pgvectorDataVolumeName    = "pgdata"
	pgvectorDefaultStorage    = "5Gi"
	pgvectorDefaultTableName  = "keese_memory"
)

// cnpgClusterGVK is the GVK for a CloudNativePG Cluster (postgresql.cnpg.io/v1.Cluster).
// Used when the CNPG operator is installed; otherwise we fall back to a StatefulSet.
var cnpgClusterGVK = schema.GroupVersionKind{
	Group:   "postgresql.cnpg.io",
	Version: "v1",
	Kind:    "Cluster",
}

// PGVectorBackend provisions a PostgreSQL+pgvector instance per Memory CR.
//
// Preference order:
//  1. postgresql.cnpg.io/v1.Cluster (CloudNativePG) with the pgvector extension enabled,
//     if the CNPG operator is installed.
//  2. Plain apps/v1.StatefulSet with the pgvector/pgvector:pg17 image as fallback.
//
// The spec's dsnSecretRef is user-provided for external PostgreSQL. When set, Provision
// is a no-op (external mode). Healthy verifies the Secret exists.
//
// SSA is used for all writes (rule 04.7). fieldOwner = keese-memory-controller.
// Credentials are never logged or surfaced in events (rule 02).
type PGVectorBackend struct {
	Client client.Client
}

// NewPGVectorBackend constructs a PGVectorBackend bound to the given controller client.
func NewPGVectorBackend(c client.Client) *PGVectorBackend {
	return &PGVectorBackend{Client: c}
}

// Provision implements BackendProvisioner.
func (b *PGVectorBackend) Provision(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	if provider.Type != keesev1alpha1.ProviderPGVector {
		return false, nil
	}
	cfg := provider.PGVector
	if cfg == nil {
		return false, fmt.Errorf("pgvector provider config is nil")
	}

	// External mode: user supplied a DSN secret — no in-cluster resource projected.
	if cfg.DSNSecretRef != "" {
		return false, nil
	}

	clusterName := name + "-pgvector"

	// Attempt CNPG Cluster path first.
	created, err := b.applyCNPGCluster(ctx, name, namespace, clusterName)
	if err == nil {
		return created, nil
	}
	// Fall back to StatefulSet.
	return b.applyPGVectorStatefulSet(ctx, name, namespace, clusterName)
}

// applyCNPGCluster SSA-applies a postgresql.cnpg.io/v1.Cluster with pgvector extension.
func (b *PGVectorBackend) applyCNPGCluster(
	ctx context.Context,
	memoryName, namespace, objName string,
) (bool, error) {
	// Probe CRD availability.
	probe := &unstructured.Unstructured{}
	probe.SetGroupVersionKind(cnpgClusterGVK)
	err := b.Client.Get(ctx, types.NamespacedName{Name: objName, Namespace: namespace}, probe)
	if err != nil && !errors.IsNotFound(err) {
		return false, fmt.Errorf("CNPG Cluster CRD not available: %w", err)
	}
	created := errors.IsNotFound(err)

	desired := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "postgresql.cnpg.io/v1",
			"kind":       "Cluster",
			"metadata": map[string]interface{}{
				"name":      objName,
				"namespace": namespace,
				"labels": map[string]interface{}{
					"keese.ai/memory":              memoryName,
					"app.kubernetes.io/managed-by": pgvectorFieldOwner,
				},
			},
			"spec": map[string]interface{}{
				"instances": int64(1),
				"postgresql": map[string]interface{}{
					"parameters": map[string]interface{}{
						"shared_buffers": "256MB",
					},
					"shared_preload_libraries": toInterfaceSlice([]string{"vector"}),
				},
				"storage": map[string]interface{}{
					"size": pgvectorDefaultStorage,
				},
				"bootstrap": map[string]interface{}{
					"initdb": map[string]interface{}{
						"database": "keese",
						"owner":    "keese",
						"postInitSQL": toInterfaceSlice([]string{
							"CREATE EXTENSION IF NOT EXISTS vector",
						}),
					},
				},
			},
		},
	}
	desired.SetCreationTimestamp(metav1.Time{})

	if patchErr := b.Client.Patch(ctx, desired, client.Apply,
		client.FieldOwner(pgvectorFieldOwner), client.ForceOwnership,
	); patchErr != nil {
		return false, fmt.Errorf("apply CNPG Cluster %s/%s: %w", namespace, objName, patchErr)
	}
	return created, nil
}

// applyPGVectorStatefulSet projects a plain StatefulSet using pgvector/pgvector:pg17.
func (b *PGVectorBackend) applyPGVectorStatefulSet(
	ctx context.Context,
	memoryName, namespace, stsName string,
) (bool, error) {
	svc := buildPGVectorSvc(memoryName, namespace, stsName)
	if err := b.Client.Patch(ctx, svc, client.Apply,
		client.FieldOwner(pgvectorFieldOwner), client.ForceOwnership,
	); err != nil {
		return false, fmt.Errorf("apply pgvector Service %s/%s: %w", namespace, stsName, err)
	}

	storageQ := resource.MustParse(pgvectorDefaultStorage)
	desired := buildPGVectorStatefulSet(memoryName, namespace, stsName, storageQ)

	var existing appsv1.StatefulSet
	getErr := b.Client.Get(ctx, types.NamespacedName{Name: stsName, Namespace: namespace}, &existing)
	created := errors.IsNotFound(getErr)

	if patchErr := b.Client.Patch(ctx, desired, client.Apply,
		client.FieldOwner(pgvectorFieldOwner), client.ForceOwnership,
	); patchErr != nil {
		return false, fmt.Errorf("apply pgvector StatefulSet %s/%s: %w", namespace, stsName, patchErr)
	}
	return created, nil
}

// Deprovision implements BackendProvisioner.
func (b *PGVectorBackend) Deprovision(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) error {
	if provider.Type != keesev1alpha1.ProviderPGVector {
		return nil
	}
	cfg := provider.PGVector
	if cfg != nil && cfg.DSNSecretRef != "" {
		return nil // external
	}

	clusterName := name + "-pgvector"

	// Try CNPG Cluster first.
	cnpg := &unstructured.Unstructured{}
	cnpg.SetGroupVersionKind(cnpgClusterGVK)
	cnpg.SetName(clusterName)
	cnpg.SetNamespace(namespace)
	if err := b.Client.Delete(ctx, cnpg); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete CNPG Cluster %s/%s: %w", namespace, clusterName, err)
	}

	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace}}
	if err := b.Client.Delete(ctx, sts); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete pgvector StatefulSet %s/%s: %w", namespace, clusterName, err)
	}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace}}
	if err := b.Client.Delete(ctx, svc); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete pgvector Service %s/%s: %w", namespace, clusterName, err)
	}
	return nil
}

// Healthy implements BackendProvisioner.
func (b *PGVectorBackend) Healthy(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	if provider.Type != keesev1alpha1.ProviderPGVector {
		return false, nil
	}
	cfg := provider.PGVector
	if cfg == nil {
		return false, fmt.Errorf("pgvector provider config is nil")
	}

	// External mode: verify DSN secret exists.
	if cfg.DSNSecretRef != "" {
		var secret corev1.Secret
		if err := b.Client.Get(ctx, types.NamespacedName{
			Name: cfg.DSNSecretRef, Namespace: namespace,
		}, &secret); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}

	clusterName := name + "-pgvector"

	// Check CNPG Cluster phase.
	cnpg := &unstructured.Unstructured{}
	cnpg.SetGroupVersionKind(cnpgClusterGVK)
	if err := b.Client.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, cnpg); err == nil {
		phase, _, _ := unstructured.NestedString(cnpg.Object, "status", "phase")
		return phase == "Cluster in healthy state", nil
	}

	// Fall back to StatefulSet.
	var sts appsv1.StatefulSet
	if err := b.Client.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, &sts); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return sts.Status.ReadyReplicas >= 1, nil
}

func buildPGVectorSvc(memoryName, namespace, svcName string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: namespace,
			Labels: map[string]string{
				"keese.ai/memory":              memoryName,
				"app.kubernetes.io/managed-by": pgvectorFieldOwner,
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector:  map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "pgvector"},
			Ports: []corev1.ServicePort{{
				Name: "postgres", Port: pgvectorPort, Protocol: corev1.ProtocolTCP,
			}},
		},
	}
}

func buildPGVectorStatefulSet(
	memoryName, namespace, stsName string,
	storageQ resource.Quantity,
) *appsv1.StatefulSet {
	replicas := int32(1)
	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      stsName,
			Namespace: namespace,
			Labels: map[string]string{
				"keese.ai/memory":              memoryName,
				"app.kubernetes.io/managed-by": pgvectorFieldOwner,
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: stsName,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "pgvector"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "pgvector"},
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPtr(true)},
					Containers: []corev1.Container{{
						Name:  "postgres",
						Image: pgvectorFallbackImage,
						Ports: []corev1.ContainerPort{{
							Name: "postgres", ContainerPort: pgvectorPort, Protocol: corev1.ProtocolTCP,
						}},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: boolPtr(false),
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name: pgvectorDataVolumeName, MountPath: "/var/lib/postgresql/data",
						}},
					}},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: pgvectorDataVolumeName},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceStorage: storageQ},
					},
				},
			}},
		},
	}
}
