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

// +kubebuilder:rbac:groups=external-secrets.io,resources=externalsecrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

const (
	zepFieldOwner       = "keese-memory-controller"
	zepImage            = "ghcr.io/getzep/zep:latest"
	zepHTTPPort         = int32(8000)
	zepDataVolumeName   = "data"
	zepDefaultStorage   = "2Gi"
	zepDefaultEndpoint  = "https://api.getzep.com"
)

// ZepBackend provisions a Zep memory service per Memory CR.
//
// Mode selection (from spec.provider.zep):
//   - apiEndpoint non-empty → Zep Cloud (external SaaS). Projects an ExternalSecret
//     to bridge credentialSecretRef from OpenBao (rule 05.8). No in-cluster workload.
//   - apiEndpoint empty → self-hosted. Projects a plain apps/v1.StatefulSet with the
//     official Zep image.
//
// Rule 05.7: credentials are mounted as projected files — never as env vars.
// Rule 05.8: OpenBao is the source of truth for upstream credentials.
type ZepBackend struct {
	Client client.Client
}

// NewZepBackend constructs a ZepBackend bound to the given controller client.
func NewZepBackend(c client.Client) *ZepBackend {
	return &ZepBackend{Client: c}
}

// Provision implements BackendProvisioner.
func (b *ZepBackend) Provision(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	if provider.Type != keesev1alpha1.ProviderZep {
		return false, nil
	}
	cfg := provider.Zep
	if cfg == nil {
		return false, fmt.Errorf("zep provider config is nil")
	}
	if cfg.CredentialSecretRef == "" {
		return false, fmt.Errorf("zep provider requires credentialSecretRef")
	}

	// External mode (Zep Cloud): project ExternalSecret for credential bridging.
	if cfg.APIEndpoint != "" {
		return applyExternalSecret(ctx, b.Client, name, namespace, cfg.CredentialSecretRef, "zep", zepFieldOwner)
	}

	// Self-hosted mode: project StatefulSet + headless Service.
	return b.applyZepStatefulSet(ctx, name, namespace, cfg)
}

// applyZepStatefulSet provisions a self-hosted Zep StatefulSet.
func (b *ZepBackend) applyZepStatefulSet(
	ctx context.Context,
	memoryName, namespace string,
	cfg *keesev1alpha1.ZepConfig,
) (bool, error) {
	stsName := memoryName + "-zep"

	svc := buildZepSvc(memoryName, namespace, stsName)
	if err := b.Client.Patch(ctx, svc, client.Apply,
		client.FieldOwner(zepFieldOwner), client.ForceOwnership,
	); err != nil {
		return false, fmt.Errorf("apply zep Service %s/%s: %w", namespace, stsName, err)
	}

	storageQ := resource.MustParse(zepDefaultStorage)
	desired := buildZepStatefulSet(memoryName, namespace, stsName, cfg.CredentialSecretRef, storageQ)

	var existing appsv1.StatefulSet
	getErr := b.Client.Get(ctx, types.NamespacedName{Name: stsName, Namespace: namespace}, &existing)
	created := errors.IsNotFound(getErr)

	if patchErr := b.Client.Patch(ctx, desired, client.Apply,
		client.FieldOwner(zepFieldOwner), client.ForceOwnership,
	); patchErr != nil {
		return false, fmt.Errorf("apply zep StatefulSet %s/%s: %w", namespace, stsName, patchErr)
	}
	return created, nil
}

// Deprovision implements BackendProvisioner.
func (b *ZepBackend) Deprovision(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) error {
	if provider.Type != keesev1alpha1.ProviderZep {
		return nil
	}
	cfg := provider.Zep
	if cfg != nil && cfg.APIEndpoint != "" {
		// Cloud mode: remove ExternalSecret.
		return deleteExternalSecret(ctx, b.Client, name+"-zep-es", namespace)
	}

	// Self-hosted mode: remove StatefulSet + Service.
	stsName := name + "-zep"
	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: stsName, Namespace: namespace}}
	if err := b.Client.Delete(ctx, sts); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete zep StatefulSet %s/%s: %w", namespace, stsName, err)
	}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: stsName, Namespace: namespace}}
	if err := b.Client.Delete(ctx, svc); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete zep Service %s/%s: %w", namespace, stsName, err)
	}
	return nil
}

// Healthy implements BackendProvisioner.
func (b *ZepBackend) Healthy(
	ctx context.Context,
	provider keesev1alpha1.MemoryProvider,
	name, namespace string,
) (bool, error) {
	if provider.Type != keesev1alpha1.ProviderZep {
		return false, nil
	}
	cfg := provider.Zep
	if cfg == nil || cfg.CredentialSecretRef == "" {
		return false, fmt.Errorf("zep provider config or credentialSecretRef is missing")
	}

	if cfg.APIEndpoint != "" {
		// Cloud: healthy when the credential Secret exists.
		return secretExists(ctx, b.Client, cfg.CredentialSecretRef, namespace)
	}

	// Self-hosted: healthy when at least one replica is Ready.
	var sts appsv1.StatefulSet
	if err := b.Client.Get(ctx, types.NamespacedName{
		Name: name + "-zep", Namespace: namespace,
	}, &sts); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return sts.Status.ReadyReplicas >= 1, nil
}

func buildZepSvc(memoryName, namespace, svcName string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: namespace,
			Labels: map[string]string{
				"keese.ai/memory":              memoryName,
				"app.kubernetes.io/managed-by": zepFieldOwner,
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector:  map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "zep"},
			Ports: []corev1.ServicePort{{
				Name: "http", Port: zepHTTPPort, Protocol: corev1.ProtocolTCP,
			}},
		},
	}
}

// buildZepStatefulSet builds the desired StatefulSet for self-hosted Zep.
// Credentials are mounted as projected files from the credentialSecretRef Secret
// at /var/run/keese/secrets/zep — never as env vars (rule 05.7).
func buildZepStatefulSet(
	memoryName, namespace, stsName, credSecretRef string,
	storageQ resource.Quantity,
) *appsv1.StatefulSet {
	replicas := int32(1)
	sts := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      stsName,
			Namespace: namespace,
			Labels: map[string]string{
				"keese.ai/memory":              memoryName,
				"app.kubernetes.io/managed-by": zepFieldOwner,
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: stsName,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "zep"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"keese.ai/memory": memoryName, "keese.ai/backend": "zep"},
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPtr(true)},
					Volumes: []corev1.Volume{{
						// Mount credential Secret as projected file (rule 05.7).
						Name: "zep-creds",
						VolumeSource: corev1.VolumeSource{
							Projected: &corev1.ProjectedVolumeSource{
								Sources: []corev1.VolumeProjection{{
									Secret: &corev1.SecretProjection{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: credSecretRef,
										},
									},
								}},
							},
						},
					}},
					Containers: []corev1.Container{{
						Name:  "zep",
						Image: zepImage,
						Ports: []corev1.ContainerPort{{
							Name: "http", ContainerPort: zepHTTPPort, Protocol: corev1.ProtocolTCP,
						}},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: boolPtr(false),
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "zep-creds",
								MountPath: "/var/run/keese/secrets/zep",
								ReadOnly:  true,
							},
							{
								Name:      zepDataVolumeName,
								MountPath: "/app/data",
							},
						},
					}},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: zepDataVolumeName},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceStorage: storageQ},
					},
				},
			}},
		},
	}
	return sts
}
