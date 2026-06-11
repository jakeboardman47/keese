// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package adkpython

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// E1 T4 — fail-closed NetworkPolicy for the ADK Python session pod.
//
// The WorkspaceSession reconciler SSA-applies this policy ONLY on the adkPython
// branch (applySessionNetworkPolicy). The goose path is untouched — its network
// isolation is owned by the Workspace controller's default-deny + egress
// policies. This policy is a pure function of its inputs so SSA re-applies are
// byte-stable across reconciles (3-reconcile idempotency, rule 06.6).
//
// Security invariants enforced here (rule 04.17 + 05.4 + 05.5 — do not relax
// without an ADR):
//   - Default-deny BOTH ingress and egress: PolicyTypes lists Ingress+Egress so
//     an empty rule set fails closed (rule 04.17 + 05.4).
//   - EXACT allows only, no wildcards (rule 05.5): every rule enumerates a
//     namespace+pod selector and a port. NO empty podSelector:{} paired with an
//     open to:[]; NO open ipBlock 0.0.0.0/0. The asserted absence of wildcards
//     is covered by TestADKPythonProvider_NetworkPolicy.
//
// Allowed paths (and nothing else):
//   - egress → Envoy AI Gateway (keese-system) on 443 — all model traffic
//     (rule 05.4); the gateway swaps in the upstream credential (rule 05.6).
//   - egress → NATS (natsNamespace) on 4222 — JetStream messaging.
//   - ingress ← peer workspace pods on A2ABridgePort (8081) — inbound A2A to the
//     bridge sidecar from other keese workspace pods.
//   - egress → peer workspace pods on A2ABridgePort (8081) — outbound A2A to
//     peer bridges.

const (
	// gatewayNamespace is where the Envoy AI Gateway service runs. All model
	// egress targets this namespace on gatewayPort (rule 05.4).
	gatewayNamespace = "keese-system"

	// gatewayPort is the Envoy AI Gateway egress port (rule 05.4: 443 only).
	gatewayPort = 443

	// natsNamespace is where NATS JetStream runs in the default topology.
	natsNamespace = "keese-system"

	// natsPort is the NATS JetStream client port.
	natsPort = 4222

	// gatewayPodLabelKey/Value select the Envoy AI Gateway proxy pods. Matches
	// the managed-by label the upstream Envoy Gateway chart stamps on its pods.
	gatewayPodLabelKey   = "app.kubernetes.io/managed-by"
	gatewayPodLabelValue = "envoy-gateway"

	// natsPodLabelKey/Value select the NATS server pods.
	natsPodLabelKey   = "app.kubernetes.io/name"
	natsPodLabelValue = "nats"

	// workspaceManagedLabelKey/Value select keese-managed workspace pods —
	// the peer set for A2A ingress/egress. This is the label every session pod
	// (goose and ADK) carries via sessionPodObjectMeta.
	workspaceManagedLabelKey   = "app.kubernetes.io/managed-by"
	workspaceManagedLabelValue = "keese-workspacesession-controller"
)

// NetworkPolicyInput carries the fields BuildNetworkPolicy needs from the
// reconcile context. The controller marshals these from the WorkspaceSession /
// Workspace CRs so the provider stays free of CRD-fetch concerns.
type NetworkPolicyInput struct {
	// Name is the NetworkPolicy object name (deterministic per workspace).
	Name string
	// Namespace is the workspace namespace the policy lives in.
	Namespace string
	// WorkspaceName scopes the podSelector to this workspace's pods via the
	// keese.ai/workspace label, mirroring sessionPodObjectMeta.
	WorkspaceName string
	// Labels are the object labels applied to the NetworkPolicy (provenance).
	Labels map[string]string
}

// BuildNetworkPolicy renders the fail-closed ADK Python NetworkPolicy.
//
// The render is deterministic: identical input yields a deeply-equal object,
// so SSA re-applies never churn (asserted by the controller idempotency test).
func BuildNetworkPolicy(in NetworkPolicyInput) *networkingv1.NetworkPolicy {
	tcp := corev1.ProtocolTCP
	gwPort := intstr.FromInt(gatewayPort)
	jsPort := intstr.FromInt(natsPort)
	a2aPort := intstr.FromInt(A2ABridgePort)

	// Peer workspace pods are keese-managed session pods. Selected by the
	// managed-by label, scoped to the workspace namespace (the policy is
	// namespaced, so a pod-selector without a namespace-selector matches only
	// same-namespace pods — never an unbounded cross-namespace wildcard).
	peerWorkspacePeer := networkingv1.NetworkPolicyPeer{
		PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				workspaceManagedLabelKey: workspaceManagedLabelValue,
			},
		},
	}

	return &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      in.Name,
			Namespace: in.Namespace,
			Labels:    in.Labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			// Select this workspace's pods only — never an empty podSelector:{}
			// (which would capture every pod in the namespace). Rule 05.5.
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"keese.ai/workspace": in.WorkspaceName,
				},
			},
			// Default-deny BOTH directions: declaring both PolicyTypes with the
			// rules below being the ONLY allows means everything else fails
			// closed (rule 04.17 + 05.4).
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				// Ingress ← peer workspace pods on the A2A bridge port (8081).
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &a2aPort},
					},
					From: []networkingv1.NetworkPolicyPeer{peerWorkspacePeer},
				},
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				// Egress → Envoy AI Gateway (keese-system) on 443.
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &gwPort},
					},
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": gatewayNamespace,
								},
							},
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									gatewayPodLabelKey: gatewayPodLabelValue,
								},
							},
						},
					},
				},
				// Egress → NATS JetStream on 4222.
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &jsPort},
					},
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": natsNamespace,
								},
							},
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									natsPodLabelKey: natsPodLabelValue,
								},
							},
						},
					},
				},
				// Egress → peer workspace pods on the A2A bridge port (8081).
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &a2aPort},
					},
					To: []networkingv1.NetworkPolicyPeer{peerWorkspacePeer},
				},
			},
		},
	}
}
