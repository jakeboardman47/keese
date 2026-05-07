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
	redisFieldOwner     = "keese-memory-controller"
	redisImage          = "redis:7"
	redisPort           = int32(6379)
	redisDataVolumeName = "data"
	redisDefaultStorage = "1Gi"
)

// RedisBackend provisions an in-cluster Redis StatefulSet per Memory CR when the
// spec does not specify an external address. If spec.provider.redis.address is set,
// the backend is treated as externally-managed: Provision is a no-op (returns
// created=false) and Healthy probes by verifying the address is non-empty.
//
// SSA is used for all writes (rule 04.7). fieldOwner = keese-memory-controller.
type RedisBackend struct {
	Client client.Client
}

// NewRedisBackend constructs a RedisBackend bound to the given controller client.
func NewRedisBackend(c client.Client) *RedisBackend {
	return &RedisBackend{Client: c}
}

// Provision implements BackendProvisioner.
// - External address set: validates config and returns (false, nil) — no resource projected.
// - No address: SSA-applies a StatefulSet + headless Service named "<memory-name>-redis".
func (b *RedisBackend) Provision(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	if provider.Type != keesev1alpha1.ProviderRedis {
		return false, nil
	}
	cfg := provider.Redis
	if cfg == nil {
		return false, fmt.Errorf("redis provider config is nil")
	}

	// External mode: user supplied an address; no in-cluster resource needed.
	if cfg.Address != "" {
		return false, nil
	}

	// In-cluster mode: project StatefulSet + headless Service.
	svcName := name + "-redis"
	svc := buildRedisSvc(name, namespace, svcName)
	if err := b.Client.Patch(ctx, svc, client.Apply,
		client.FieldOwner(redisFieldOwner), client.ForceOwnership,
	); err != nil {
		return false, fmt.Errorf("apply redis Service %s/%s: %w", namespace, svcName, err)
	}

	replicas := cfg.Replicas
	if replicas <= 0 {
		replicas = 1
	}
	desired := buildRedisStatefulSet(name, namespace, svcName, replicas)

	var existing appsv1.StatefulSet
	err := b.Client.Get(ctx, types.NamespacedName{Name: svcName, Namespace: namespace}, &existing)
	created := errors.IsNotFound(err)

	if patchErr := b.Client.Patch(ctx, desired, client.Apply,
		client.FieldOwner(redisFieldOwner), client.ForceOwnership,
	); patchErr != nil {
		return false, fmt.Errorf("apply redis StatefulSet %s/%s: %w", namespace, svcName, patchErr)
	}
	return created, nil
}

// Deprovision implements BackendProvisioner.
// Removes the Redis StatefulSet and Service. Missing resources are silently ignored.
func (b *RedisBackend) Deprovision(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) error {
	if provider.Type != keesev1alpha1.ProviderRedis {
		return nil
	}
	cfg := provider.Redis
	// External mode: nothing to deprovision.
	if cfg != nil && cfg.Address != "" {
		return nil
	}

	svcName := name + "-redis"
	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: namespace}}
	if err := b.Client.Delete(ctx, sts); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete redis StatefulSet %s/%s: %w", namespace, svcName, err)
	}

	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: namespace}}
	if err := b.Client.Delete(ctx, svc); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete redis Service %s/%s: %w", namespace, svcName, err)
	}
	return nil
}

// Healthy implements BackendProvisioner.
// - External address: healthy when address is non-empty (no live probe at controller tier).
// - In-cluster: healthy when at least one StatefulSet replica is Ready.
func (b *RedisBackend) Healthy(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	if provider.Type != keesev1alpha1.ProviderRedis {
		return false, nil
	}
	cfg := provider.Redis
	if cfg == nil {
		return false, fmt.Errorf("redis provider config is nil")
	}

	if cfg.Address != "" {
		// External: address is the DSN; controller does not TCP-probe (no egress rules at
		// controller tier — rule 05.4). Treat configured address as healthy.
		return true, nil
	}

	// In-cluster: check StatefulSet readyReplicas.
	var sts appsv1.StatefulSet
	if err := b.Client.Get(ctx, types.NamespacedName{
		Name: name + "-redis", Namespace: namespace,
	}, &sts); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return sts.Status.ReadyReplicas >= 1, nil
}

func buildRedisSvc(memoryName, namespace, svcName string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: namespace,
			Labels: map[string]string{
				"keese.ai/memory":              memoryName,
				"app.kubernetes.io/managed-by": redisFieldOwner,
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None", // headless
			Selector:  map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "redis"},
			Ports: []corev1.ServicePort{{
				Name: "redis", Port: redisPort, Protocol: corev1.ProtocolTCP,
			}},
		},
	}
}

func buildRedisStatefulSet(memoryName, namespace, stsName string, replicas int32) *appsv1.StatefulSet {
	storageQ := resource.MustParse(redisDefaultStorage)
	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      stsName,
			Namespace: namespace,
			Labels: map[string]string{
				"keese.ai/memory":              memoryName,
				"app.kubernetes.io/managed-by": redisFieldOwner,
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: stsName,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "redis"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "redis"},
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: boolPtr(true),
					},
					Containers: []corev1.Container{{
						Name:  "redis",
						Image: redisImage,
						Ports: []corev1.ContainerPort{{Name: "redis", ContainerPort: redisPort, Protocol: corev1.ProtocolTCP}},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: boolPtr(false),
							ReadOnlyRootFilesystem:   boolPtr(true),
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name: redisDataVolumeName, MountPath: "/data",
						}},
					}},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: redisDataVolumeName},
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

