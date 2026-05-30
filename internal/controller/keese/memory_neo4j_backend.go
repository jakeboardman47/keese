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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

const (
	neo4jFieldOwner     = "keese-memory-controller"
	neo4jImage          = "neo4j:5-community"
	neo4jBoltPort       = int32(7687)
	neo4jHTTPPort       = int32(7474)
	neo4jDataVolumeName = "data"
	neo4jDefaultStorage = "5Gi"
)

// Neo4jBackend provisions a Neo4j community graph database per Memory CR.
//
// If spec.provider.neo4j.uri is non-empty the backend is treated as externally
// managed (no in-cluster resource projected). Otherwise a plain apps/v1.StatefulSet
// is projected with the neo4j:5-community image.
//
// SSA is used for all writes (rule 04.7). fieldOwner = keese-memory-controller.
// Credential references are never logged or surfaced in events (rule 02).
type Neo4jBackend struct {
	Client client.Client
}

// NewNeo4jBackend constructs a Neo4jBackend bound to the given controller client.
func NewNeo4jBackend(c client.Client) *Neo4jBackend {
	return &Neo4jBackend{Client: c}
}

// Provision implements BackendProvisioner.
// External URI set → no-op. Otherwise SSA-applies a StatefulSet + headless Service.
func (b *Neo4jBackend) Provision(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	if provider.Type != keesev1alpha1.ProviderNeo4j {
		return false, nil
	}
	cfg := provider.Neo4j
	if cfg == nil {
		return false, fmt.Errorf("neo4j provider config is nil")
	}

	// External mode.
	if cfg.URI != "" {
		return false, nil
	}

	stsName := name + "-neo4j"

	svc := buildNeo4jSvc(name, namespace, stsName)
	if err := b.Client.Patch(ctx, svc, client.Apply,
		client.FieldOwner(neo4jFieldOwner), client.ForceOwnership,
	); err != nil {
		return false, fmt.Errorf("apply neo4j Service %s/%s: %w", namespace, stsName, err)
	}

	storageQ := resource.MustParse(neo4jDefaultStorage)
	desired := buildNeo4jStatefulSet(name, namespace, stsName, storageQ, cfg.CredentialSecretRef)

	var existing appsv1.StatefulSet
	getErr := b.Client.Get(ctx, types.NamespacedName{Name: stsName, Namespace: namespace}, &existing)
	created := errors.IsNotFound(getErr)

	if patchErr := b.Client.Patch(ctx, desired, client.Apply,
		client.FieldOwner(neo4jFieldOwner), client.ForceOwnership,
	); patchErr != nil {
		return false, fmt.Errorf("apply neo4j StatefulSet %s/%s: %w", namespace, stsName, patchErr)
	}
	return created, nil
}

// Deprovision implements BackendProvisioner.
func (b *Neo4jBackend) Deprovision(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) error {
	if provider.Type != keesev1alpha1.ProviderNeo4j {
		return nil
	}
	cfg := provider.Neo4j
	if cfg != nil && cfg.URI != "" {
		return nil // external
	}

	stsName := name + "-neo4j"
	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: stsName, Namespace: namespace}}
	if err := b.Client.Delete(ctx, sts); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete neo4j StatefulSet %s/%s: %w", namespace, stsName, err)
	}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: stsName, Namespace: namespace}}
	if err := b.Client.Delete(ctx, svc); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete neo4j Service %s/%s: %w", namespace, stsName, err)
	}
	return nil
}

// Healthy implements BackendProvisioner.
// External URI: healthy when URI is non-empty (no TCP probe at controller tier — rule 05.4).
// In-cluster: healthy when at least one StatefulSet replica is Ready.
func (b *Neo4jBackend) Healthy(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	if provider.Type != keesev1alpha1.ProviderNeo4j {
		return false, nil
	}
	cfg := provider.Neo4j
	if cfg == nil {
		return false, fmt.Errorf("neo4j provider config is nil")
	}

	if cfg.URI != "" {
		return true, nil
	}

	var sts appsv1.StatefulSet
	if err := b.Client.Get(ctx, types.NamespacedName{
		Name: name + "-neo4j", Namespace: namespace,
	}, &sts); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return sts.Status.ReadyReplicas >= 1, nil
}

func buildNeo4jSvc(memoryName, namespace, svcName string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: namespace,
			Labels: map[string]string{
				"keese.ai/memory":              memoryName,
				"app.kubernetes.io/managed-by": neo4jFieldOwner,
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector:  map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "neo4j"},
			Ports: []corev1.ServicePort{
				{Name: "bolt", Port: neo4jBoltPort, Protocol: corev1.ProtocolTCP},
				{Name: "http", Port: neo4jHTTPPort, Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

// buildNeo4jStatefulSet builds the desired StatefulSet for an in-cluster Neo4j instance.
//
// When credSecretRef is non-empty the Secret is mounted as a projected file at
// /var/run/keese/secrets/neo4j (rule 05.7) and the entrypoint reads the "password"
// key to set NEO4J_AUTH at runtime.  The hardcoded NEO4J_AUTH=none env var is
// suppressed in this path — it must not appear alongside authenticated mode.
// When credSecretRef is empty the server starts unauthenticated (dev/test only).
func buildNeo4jStatefulSet(
	memoryName, namespace, stsName string,
	storageQ resource.Quantity,
	credSecretRef string,
) *appsv1.StatefulSet {
	replicas := int32(1)

	container := corev1.Container{
		Name:  "neo4j",
		Image: neo4jImage,
		Ports: []corev1.ContainerPort{
			{Name: "bolt", ContainerPort: neo4jBoltPort, Protocol: corev1.ProtocolTCP},
			{Name: "http", ContainerPort: neo4jHTTPPort, Protocol: corev1.ProtocolTCP},
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: boolPtr(false),
		},
		VolumeMounts: []corev1.VolumeMount{{
			Name: neo4jDataVolumeName, MountPath: "/data",
		}},
	}

	podSpec := corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPtr(true)},
	}

	if credSecretRef != "" {
		// Mount credential Secret as a projected file (rule 05.7).
		// Never use env.valueFrom.secretKeyRef or envFrom (rule 05.7).
		// NEO4J_AUTH=none must NOT be set when authentication is enabled.
		vol, mount := credProjectionVolume("neo4j-creds", credSecretRef, "/var/run/keese/secrets/neo4j")
		podSpec.Volumes = []corev1.Volume{vol}
		container.VolumeMounts = append(container.VolumeMounts, mount)
		// Read password from projected file; exec preserves PID 1.
		container.Command = []string{"sh", "-c"}
		container.Args = []string{
			`export NEO4J_AUTH="neo4j/$(cat /var/run/keese/secrets/neo4j/password)"; exec /startup/docker-entrypoint.sh neo4j`,
		}
	} else {
		// Dev/test only: disable authentication.
		container.Env = []corev1.EnvVar{{
			Name:  "NEO4J_AUTH",
			Value: "none",
		}}
	}

	podSpec.Containers = []corev1.Container{container}

	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      stsName,
			Namespace: namespace,
			Labels: map[string]string{
				"keese.ai/memory":              memoryName,
				"app.kubernetes.io/managed-by": neo4jFieldOwner,
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: stsName,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "neo4j"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "neo4j"},
				},
				Spec: podSpec,
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: neo4jDataVolumeName},
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
