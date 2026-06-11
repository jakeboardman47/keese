// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package adkpython

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// E1 T2 — single-container ADK Python pod template.
//
// The WorkspaceSession reconciler (T5 discriminator) calls BuildPodSpec when
// AgentRuntime.spec.implementation.adkPython is set, instead of the goose pod
// path. The render is a pure function of its inputs so it is exhaustively
// unit-/envtest-able without a live cluster (TestADKPythonProvider_PodRender)
// and so SSA re-applies are byte-stable across reconciles (3-reconcile
// idempotency).
//
// Security invariants enforced here (do not relax without an ADR):
//   - rule 05.2: NON-SECRET env only. No *API_KEY* / *SECRET* / bearer values.
//     The pod authenticates to upstreams solely via the projected SA token;
//     the Envoy AI Gateway swaps in the real upstream credential (rule 05.6).
//   - rule 05.7: secret material arrives as projected files, never env vars —
//     the egress SA token and the gateway CA bundle are file mounts.
//   - rule 05.11: hardened SecurityContext — runAsNonRoot, readOnlyRootFilesystem,
//     allowPrivilegeEscalation:false, drop ALL capabilities.

const (
	// ContainerName is the single ADK Python container's name in the pod.
	ContainerName = "adk-python"

	// A2APort is the inbound A2A port the ADK server listens on. The A2A
	// bridge sidecar (E1b) fronts this; at E1a the ADK server binds it
	// directly for the single-container increment.
	A2APort = 8080

	// EnvoyAIGatewayURL is the in-cluster Envoy AI Gateway service endpoint.
	// All model traffic egresses here; the gateway terminates the SA token,
	// evaluates OpenFGA, and injects the upstream credential (rule 05.6).
	EnvoyAIGatewayURL = "https://envoy-ai-gateway.keese-system.svc:443"

	// Mount paths (rule 05.7: projected files under /var/run/keese).
	sessionMountPath  = "/var/run/keese/session"
	tokenMountPath    = "/var/run/keese/tokens"
	tokenPathEgress   = tokenMountPath + "/egress"
	caBundleMountPath = "/var/run/keese/ca"
	scratchMountPath  = "/tmp"

	// caBundleFile is the gateway serving-CA cert the ADK runtime trusts so
	// TLS to the gateway succeeds without disabling verification.
	caBundleFile = caBundleMountPath + "/ca.crt"

	// saTokenExpirationSeconds is the projected egress SA token TTL (rule 05.7:
	// short-lived; matches the controller-side 600s ceiling).
	saTokenExpirationSeconds = int64(600)

	// gatewayCAConfigMapName is the in-namespace ConfigMap mirroring the AI
	// Gateway serving CA cert (kept in sync by infra-bootstrap). Optional so a
	// missing mirror doesn't block pod creation — the runtime surfaces a clear
	// TLS error in logs instead.
	gatewayCAConfigMapName = "keese-aigateway-ca"

	// terminationGracePeriodSeconds aligns with the agent-runtime drain budget
	// (rule 06.3: agent runtime 120s).
	terminationGracePeriodSeconds = int64(120)
)

// PodInput carries the fields BuildPodSpec needs from the reconcile context.
// The controller marshals these from the WorkspaceSession / Workspace /
// AgentRuntime CRs so the provider stays free of CRD-fetch concerns.
type PodInput struct {
	// Image is the ADK Python OCI reference (ADKPythonSpec.Image).
	Image string
	// WorkspaceName is the parent Workspace .metadata.name.
	WorkspaceName string
	// TenantName scopes the egress SA token audience: keese-egress-<tenant>.
	TenantName string
	// ServiceAccountName is the per-workspace SA the pod runs as.
	ServiceAccountName string
	// SessionPVCName is the session PVC mounted at /var/run/keese/session.
	SessionPVCName string
}

// BuildPodSpec renders the single-container ADK Python pod spec.
//
// Env policy (T2): plain, non-secret values only. The model-provider base URLs
// all point at the in-cluster gateway so the ADK SDK's per-provider HTTP
// clients route through it; the gateway — not the pod — holds upstream
// credentials.
func BuildPodSpec(in PodInput) corev1.PodSpec {
	tgps := terminationGracePeriodSeconds
	tokenExpiry := saTokenExpirationSeconds

	return corev1.PodSpec{
		RestartPolicy:                 corev1.RestartPolicyNever,
		ServiceAccountName:            in.ServiceAccountName,
		AutomountServiceAccountToken:  ptr(false), // SA token via projected volume only (rule 05.1/05.7)
		TerminationGracePeriodSeconds: &tgps,
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: ptr(true),
			// Distroless nonroot uid (gcr.io/distroless/python3-debian12:nonroot).
			RunAsUser:  ptr(int64(65532)),
			RunAsGroup: ptr(int64(65532)),
			FSGroup:    ptr(int64(65532)),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Volumes: podVolumes(in, tokenExpiry),
		Containers: []corev1.Container{
			{
				Name:            ContainerName,
				Image:           in.Image,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"/opt/venv/bin/python"},
				Args: []string{
					"-m", "adk.serve",
					"--workspace", "$(KEESE_WORKSPACE_NAME)",
					"--a2a-port", fmt.Sprintf("%d", A2APort),
					"--gateway", "$(ENVOY_AI_GATEWAY_URL)",
				},
				Ports: []corev1.ContainerPort{
					{Name: "a2a", ContainerPort: A2APort, Protocol: corev1.ProtocolTCP},
				},
				Env:          podEnv(),
				VolumeMounts: podVolumeMounts(),
				SecurityContext: &corev1.SecurityContext{
					RunAsNonRoot:             ptr(true),
					ReadOnlyRootFilesystem:   ptr(true),
					AllowPrivilegeEscalation: ptr(false),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
				// preStop drain: keese-drain checkpoints session state to the PVC
				// before the kubelet deletes the pod (rule 06.2). The binary ships
				// in the ADK image (Dockerfile.adk-python stage 2).
				Lifecycle: &corev1.Lifecycle{
					PreStop: &corev1.LifecycleHandler{
						Exec: &corev1.ExecAction{
							Command: []string{
								"/usr/local/bin/keese-drain",
								"--pvc-root=" + sessionMountPath,
								"--timeout=90s",
							},
						},
					},
				},
			},
		},
	}
}

// podEnv returns the ADK container env slice. EVERY entry is a plain,
// non-secret value (rule 05.2). The provider base-URLs route the ADK SDK's
// per-provider clients through the gateway; the gateway holds the real keys.
//
// Invariant asserted by TestADKPythonProvider_PodRender: no env name or value
// matches *API_KEY* or *SECRET*.
func podEnv() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: "KEESE_WORKSPACE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
		{
			Name: "KEESE_TENANT_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.labels['keese.ai/tenant']"},
			},
		},
		{Name: "ENVOY_AI_GATEWAY_URL", Value: EnvoyAIGatewayURL},
		// Per-provider base-URLs: ADK SDK reads these to point each upstream
		// HTTP client at the gateway. No keys — the gateway injects them.
		{Name: "OPENAI_BASE_URL", Value: EnvoyAIGatewayURL + "/openai/v1"},
		{Name: "ANTHROPIC_BASE_URL", Value: EnvoyAIGatewayURL + "/anthropic"},
		{Name: "GOOGLE_VERTEX_BASE_URL", Value: EnvoyAIGatewayURL + "/vertex"},
		// CA trust + egress token path arrive as files (rule 05.7); the env
		// only points at the file locations, never the material itself.
		{Name: "SSL_CERT_FILE", Value: caBundleFile},
		{Name: "KEESE_EGRESS_TOKEN_PATH", Value: tokenPathEgress},
		// HOME on the writable session PVC subdir so ADK can persist state
		// under readOnlyRootFilesystem.
		{Name: "HOME", Value: sessionMountPath + "/home"},
	}
}

// podVolumes builds the pod volume slice: session PVC, projected egress SA
// token (audience keese-egress-<tenant>, 600s — rule 05.7), gateway CA bundle
// (read-only), and a writable scratch EmptyDir for /tmp (required under
// readOnlyRootFilesystem).
func podVolumes(in PodInput, tokenExpiry int64) []corev1.Volume {
	return []corev1.Volume{
		{
			Name: "session",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: in.SessionPVCName,
				},
			},
		},
		{
			Name: "sa-token",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
								Audience:          fmt.Sprintf("keese-egress-%s", in.TenantName),
								ExpirationSeconds: &tokenExpiry,
								Path:              "egress",
							},
						},
					},
				},
			},
		},
		{
			Name: "scratch",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					SizeLimit: ptrQuantity("256Mi"),
				},
			},
		},
		{
			Name: "gateway-ca",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: gatewayCAConfigMapName,
					},
					Optional: ptr(true),
				},
			},
		},
	}
}

// podVolumeMounts pairs with podVolumes. The SA token + CA bundle are mounted
// read-only (rule 05.7).
func podVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: "session", MountPath: sessionMountPath},
		{Name: "sa-token", MountPath: tokenMountPath, ReadOnly: true},
		{Name: "gateway-ca", MountPath: caBundleMountPath, ReadOnly: true},
		{Name: "scratch", MountPath: scratchMountPath},
	}
}

// PodInputFromCRs marshals the SPI-render inputs from the reconcile CRs. Kept
// in the provider package so the env/volume contract lives in one place; the
// controller (T5) calls this then BuildPodSpec.
func PodInputFromCRs(
	ws *keesev1alpha1.Workspace,
	ar *keesev1alpha1.AgentRuntime,
	serviceAccountName, sessionPVCName string,
) PodInput {
	return PodInput{
		Image:              ar.Spec.Implementation.AdkPython.Image,
		WorkspaceName:      ws.Name,
		TenantName:         ws.Spec.TenantRef.Name,
		ServiceAccountName: serviceAccountName,
		SessionPVCName:     sessionPVCName,
	}
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }

// ptrQuantity parses s into a *resource.Quantity.
func ptrQuantity(s string) *resource.Quantity {
	q := resource.MustParse(s)
	return &q
}
