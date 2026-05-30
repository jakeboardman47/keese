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
// +kubebuilder:rbac:groups=qdrant.io,resources=qdrantclusters,verbs=get;list;watch;create;update;patch;delete

const (
	qdrantFieldOwner     = "keese-memory-controller"
	qdrantImage          = "qdrant/qdrant:v1.13.0"
	qdrantGRPCPort       = int32(6334)
	qdrantHTTPPort       = int32(6333)
	qdrantDataVolumeName = "storage"
	qdrantDefaultStorage = "2Gi"
)

// qdrantOperatorGVK is the GVK for the Qdrant operator CRD (qdrant.io/v1.QdrantCluster).
// Used only when the operator is installed; otherwise we fall back to a StatefulSet.
var qdrantOperatorGVK = schema.GroupVersionKind{
	Group:   "qdrant.io",
	Version: "v1",
	Kind:    "QdrantCluster",
}

// QdrantBackend provisions a Qdrant vector store per Memory CR.
//
// If spec.provider.qdrant.endpoint is non-empty the backend is treated as externally
// managed (no in-cluster resource projected). Otherwise the provisioner attempts to
// create a qdrant.io/v1.QdrantCluster via the Qdrant operator; on CRD-not-found it
// falls back to a plain apps/v1.StatefulSet.
//
// SSA is used for all writes (rule 04.7). fieldOwner = keese-memory-controller.
type QdrantBackend struct {
	Client client.Client
}

// NewQdrantBackend constructs a QdrantBackend bound to the given controller client.
func NewQdrantBackend(c client.Client) *QdrantBackend {
	return &QdrantBackend{Client: c}
}

// Provision implements BackendProvisioner.
func (b *QdrantBackend) Provision(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	if provider.Type != keesev1alpha1.ProviderQdrant {
		return false, nil
	}
	cfg := provider.Qdrant
	if cfg == nil {
		return false, fmt.Errorf("qdrant provider config is nil")
	}

	// External mode.
	if cfg.Endpoint != "" {
		return false, nil
	}

	// Attempt QdrantCluster via operator CRD; fall back to StatefulSet.
	svcName := name + "-qdrant"
	replicas := cfg.Replicas
	if replicas <= 0 {
		replicas = 1
	}

	// Try the Qdrant operator CRD path (unstructured, no typed import).
	created, err := b.applyQdrantCluster(ctx, name, namespace, svcName, replicas)
	if err == nil {
		return created, nil
	}
	// Fall back to StatefulSet when the operator CRD is not installed.
	return b.applyQdrantStatefulSet(ctx, name, namespace, svcName, replicas, cfg.CredentialSecretRef)
}

// applyQdrantCluster SSA-applies a qdrant.io/v1.QdrantCluster via unstructured.
func (b *QdrantBackend) applyQdrantCluster(
	ctx context.Context,
	memoryName, namespace, objName string,
	replicas int32,
) (bool, error) {
	// Probe whether the CRD exists by attempting a Get with the unstructured GVK.
	probe := &unstructured.Unstructured{}
	probe.SetGroupVersionKind(qdrantOperatorGVK)
	err := b.Client.Get(ctx, types.NamespacedName{Name: objName, Namespace: namespace}, probe)
	if err != nil && !errors.IsNotFound(err) {
		// Any error other than NotFound (e.g. no-kind-match) means CRD absent — fall back.
		return false, fmt.Errorf("qdrant operator CRD not available: %w", err)
	}
	created := errors.IsNotFound(err)

	desired := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "qdrant.io/v1",
			"kind":       "QdrantCluster",
			"metadata": map[string]interface{}{
				"name":      objName,
				"namespace": namespace,
				"labels": map[string]interface{}{
					"keese.ai/memory":              memoryName,
					"app.kubernetes.io/managed-by": qdrantFieldOwner,
				},
			},
			"spec": map[string]interface{}{
				"replicas": int64(replicas),
				"image": map[string]interface{}{
					"repository": "qdrant/qdrant",
					"tag":        "v1.13.0",
				},
				"storage": map[string]interface{}{
					"size": qdrantDefaultStorage,
				},
			},
		},
	}
	desired.SetCreationTimestamp(metav1.Time{})

	if patchErr := b.Client.Patch(ctx, desired, client.Apply,
		client.FieldOwner(qdrantFieldOwner), client.ForceOwnership,
	); patchErr != nil {
		return false, fmt.Errorf("apply QdrantCluster %s/%s: %w", namespace, objName, patchErr)
	}
	return created, nil
}

// applyQdrantStatefulSet falls back to a plain StatefulSet when the Qdrant operator is absent.
func (b *QdrantBackend) applyQdrantStatefulSet(
	ctx context.Context,
	memoryName, namespace, stsName string,
	replicas int32,
	credSecretRef string,
) (bool, error) {
	svc := buildQdrantSvc(memoryName, namespace, stsName)
	if err := b.Client.Patch(ctx, svc, client.Apply,
		client.FieldOwner(qdrantFieldOwner), client.ForceOwnership,
	); err != nil {
		return false, fmt.Errorf("apply qdrant Service %s/%s: %w", namespace, stsName, err)
	}

	storageQ := resource.MustParse(qdrantDefaultStorage)
	desired := buildQdrantStatefulSet(memoryName, namespace, stsName, replicas, storageQ, credSecretRef)

	var existing appsv1.StatefulSet
	getErr := b.Client.Get(ctx, types.NamespacedName{Name: stsName, Namespace: namespace}, &existing)
	created := errors.IsNotFound(getErr)

	if patchErr := b.Client.Patch(ctx, desired, client.Apply,
		client.FieldOwner(qdrantFieldOwner), client.ForceOwnership,
	); patchErr != nil {
		return false, fmt.Errorf("apply qdrant StatefulSet %s/%s: %w", namespace, stsName, patchErr)
	}
	return created, nil
}

// Deprovision implements BackendProvisioner.
func (b *QdrantBackend) Deprovision(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) error {
	if provider.Type != keesev1alpha1.ProviderQdrant {
		return nil
	}
	cfg := provider.Qdrant
	if cfg != nil && cfg.Endpoint != "" {
		return nil // external
	}

	svcName := name + "-qdrant"

	// Attempt to delete QdrantCluster (no-op if CRD absent or resource missing).
	qc := &unstructured.Unstructured{}
	qc.SetGroupVersionKind(qdrantOperatorGVK)
	qc.SetName(svcName)
	qc.SetNamespace(namespace)
	if err := b.Client.Delete(ctx, qc); err != nil && !errors.IsNotFound(err) {
		// Best-effort; log through caller.
		return fmt.Errorf("delete QdrantCluster %s/%s: %w", namespace, svcName, err)
	}

	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: namespace}}
	if err := b.Client.Delete(ctx, sts); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete qdrant StatefulSet %s/%s: %w", namespace, svcName, err)
	}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: namespace}}
	if err := b.Client.Delete(ctx, svc); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete qdrant Service %s/%s: %w", namespace, svcName, err)
	}
	return nil
}

// Healthy implements BackendProvisioner.
func (b *QdrantBackend) Healthy(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	if provider.Type != keesev1alpha1.ProviderQdrant {
		return false, nil
	}
	cfg := provider.Qdrant
	if cfg == nil {
		return false, fmt.Errorf("qdrant provider config is nil")
	}

	if cfg.Endpoint != "" {
		return true, nil // external endpoint — treat as healthy at controller tier
	}

	stsName := name + "-qdrant"

	// Check QdrantCluster status first (unstructured).
	qc := &unstructured.Unstructured{}
	qc.SetGroupVersionKind(qdrantOperatorGVK)
	if err := b.Client.Get(ctx, types.NamespacedName{Name: stsName, Namespace: namespace}, qc); err == nil {
		phase, _, _ := unstructured.NestedString(qc.Object, "status", "phase")
		return phase == "Running" || phase == "Ready", nil
	}

	// Fall back to StatefulSet.
	var sts appsv1.StatefulSet
	if err := b.Client.Get(ctx, types.NamespacedName{Name: stsName, Namespace: namespace}, &sts); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return sts.Status.ReadyReplicas >= 1, nil
}

func buildQdrantSvc(memoryName, namespace, svcName string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: namespace,
			Labels: map[string]string{
				"keese.ai/memory":              memoryName,
				"app.kubernetes.io/managed-by": qdrantFieldOwner,
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector:  map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "qdrant"},
			Ports: []corev1.ServicePort{
				{Name: "grpc", Port: qdrantGRPCPort, Protocol: corev1.ProtocolTCP},
				{Name: "http", Port: qdrantHTTPPort, Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

// buildQdrantStatefulSet builds the desired StatefulSet for an in-cluster Qdrant instance.
//
// When credSecretRef is non-empty the Secret is mounted as a projected file at
// /var/run/keese/secrets/qdrant (rule 05.7) and the entrypoint reads the "api-key"
// key to set QDRANT__SERVICE__API_KEY at runtime.
// When credSecretRef is empty the server starts with no API key (dev/test only).
func buildQdrantStatefulSet(
	memoryName, namespace, stsName string,
	replicas int32,
	storageQ resource.Quantity,
	credSecretRef string,
) *appsv1.StatefulSet {
	container := corev1.Container{
		Name:  "qdrant",
		Image: qdrantImage,
		Ports: []corev1.ContainerPort{
			{Name: "grpc", ContainerPort: qdrantGRPCPort, Protocol: corev1.ProtocolTCP},
			{Name: "http", ContainerPort: qdrantHTTPPort, Protocol: corev1.ProtocolTCP},
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: boolPtr(false),
		},
		VolumeMounts: []corev1.VolumeMount{{
			Name: qdrantDataVolumeName, MountPath: "/qdrant/storage",
		}},
	}

	podSpec := corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPtr(true)},
	}

	if credSecretRef != "" {
		// Mount credential Secret as a projected file (rule 05.7).
		// Never use env.valueFrom.secretKeyRef or envFrom (rule 05.7).
		vol, mount := credProjectionVolume("qdrant-creds", credSecretRef, "/var/run/keese/secrets/qdrant")
		podSpec.Volumes = []corev1.Volume{vol}
		container.VolumeMounts = append(container.VolumeMounts, mount)
		// Read API key from projected file; exec preserves PID 1.
		container.Command = []string{"sh", "-c"}
		container.Args = []string{
			`export QDRANT__SERVICE__API_KEY="$(cat /var/run/keese/secrets/qdrant/api-key)"; exec ./entrypoint.sh`,
		}
		container.WorkingDir = "/qdrant"
	}

	podSpec.Containers = []corev1.Container{container}

	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      stsName,
			Namespace: namespace,
			Labels: map[string]string{
				"keese.ai/memory":              memoryName,
				"app.kubernetes.io/managed-by": qdrantFieldOwner,
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: stsName,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "qdrant"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "qdrant"},
				},
				Spec: podSpec,
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: qdrantDataVolumeName},
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
