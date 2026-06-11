// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package adkpython

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func testNetworkPolicyInput() NetworkPolicyInput {
	return NetworkPolicyInput{
		Name:          "keese-ws-uid-1234-adk-netpol",
		Namespace:     "default",
		WorkspaceName: "ws-demo",
		Labels: map[string]string{
			"keese.ai/workspace": "ws-demo",
		},
	}
}

// TestADKPythonProvider_NetworkPolicy asserts the fail-closed network posture of
// the rendered policy: default-deny BOTH directions (rule 04.17 + 05.4), each
// EXACT allow present (gateway:443, NATS:4222, peer A2A ingress/egress :8081),
// and NO wildcard anywhere (rule 05.5 — no empty podSelector:{} with open to:[],
// no open ipBlock).
func TestADKPythonProvider_NetworkPolicy(t *testing.T) {
	np := BuildNetworkPolicy(testNetworkPolicyInput())

	t.Run("default-deny base: both PolicyTypes, scoped podSelector (rule 04.17+05.4)", func(t *testing.T) {
		gotTypes := map[networkingv1.PolicyType]bool{}
		for _, pt := range np.Spec.PolicyTypes {
			gotTypes[pt] = true
		}
		if !gotTypes[networkingv1.PolicyTypeIngress] || !gotTypes[networkingv1.PolicyTypeEgress] {
			t.Errorf("PolicyTypes must include BOTH Ingress and Egress (fail-closed), got %+v", np.Spec.PolicyTypes)
		}
		// The podSelector must NOT be empty (an empty selector captures every
		// pod in the namespace — a wildcard, rule 05.5). It must scope to the
		// workspace.
		if len(np.Spec.PodSelector.MatchLabels) == 0 && len(np.Spec.PodSelector.MatchExpressions) == 0 {
			t.Error("podSelector must not be empty (rule 05.5: no namespace-wide wildcard)")
		}
		if np.Spec.PodSelector.MatchLabels["keese.ai/workspace"] != "ws-demo" {
			t.Errorf("podSelector must scope to the workspace, got %+v", np.Spec.PodSelector.MatchLabels)
		}
	})

	t.Run("exact egress: gateway:443", func(t *testing.T) {
		if !hasEgressRule(np, gatewayPort, gatewayNamespace, gatewayPodLabelKey, gatewayPodLabelValue) {
			t.Errorf("missing exact egress to gateway %s:%d (%s=%s)", gatewayNamespace, gatewayPort, gatewayPodLabelKey, gatewayPodLabelValue)
		}
	})

	t.Run("exact egress: NATS:4222", func(t *testing.T) {
		if !hasEgressRule(np, natsPort, natsNamespace, natsPodLabelKey, natsPodLabelValue) {
			t.Errorf("missing exact egress to NATS %s:%d (%s=%s)", natsNamespace, natsPort, natsPodLabelKey, natsPodLabelValue)
		}
	})

	t.Run("exact egress: peer workspace pods :8081", func(t *testing.T) {
		if !hasPeerEgressRule(np, A2ABridgePort) {
			t.Errorf("missing exact egress to peer workspace pods on :%d", A2ABridgePort)
		}
	})

	t.Run("exact ingress: from peer workspace pods :8081", func(t *testing.T) {
		if !hasPeerIngressRule(np, A2ABridgePort) {
			t.Errorf("missing exact ingress from peer workspace pods on :%d", A2ABridgePort)
		}
	})

	t.Run("no wildcards anywhere (rule 05.5)", func(t *testing.T) {
		assertNoWildcardEgress(t, np)
		assertNoWildcardIngress(t, np)
	})

	t.Run("every rule carries a port (no port-open allow)", func(t *testing.T) {
		for i, e := range np.Spec.Egress {
			if len(e.Ports) == 0 {
				t.Errorf("egress rule %d has no Ports — opens all ports (rule 05.5)", i)
			}
		}
		for i, in := range np.Spec.Ingress {
			if len(in.Ports) == 0 {
				t.Errorf("ingress rule %d has no Ports — opens all ports (rule 05.5)", i)
			}
		}
	})
}

// TestADKPythonProvider_ThreeReconcileIdempotency asserts the NetworkPolicy
// render is a pure function of its input: three successive BuildNetworkPolicy
// calls with identical input produce deeply-equal objects. SSA convergence in
// ≤3 reconciles with no churn (rule 06.6) depends on this byte-stability — a
// non-deterministic render would re-patch the NetworkPolicy on every reconcile.
func TestADKPythonProvider_ThreeReconcileIdempotency(t *testing.T) {
	in := testNetworkPolicyInput()
	first := BuildNetworkPolicy(in)
	for i := 0; i < 3; i++ {
		next := BuildNetworkPolicy(in)
		if !reflect.DeepEqual(first, next) {
			t.Fatalf("NetworkPolicy render %d differs from first render — not idempotent", i+1)
		}
	}

	// The pod render must be idempotent over the SAME input too, so that an SSA
	// reconcile that re-applies BOTH objects converges with no churn.
	firstPod := BuildPodSpec(testInput())
	for i := 0; i < 3; i++ {
		nextPod := BuildPodSpec(testInput())
		if !reflect.DeepEqual(firstPod, nextPod) {
			t.Fatalf("pod render %d differs from first render — not idempotent", i+1)
		}
	}
}

// hasEgressRule reports whether np has an egress rule allowing tcp/port to a
// peer selected by (namespace metadata.name == ns) AND (pod label key==val).
func hasEgressRule(np *networkingv1.NetworkPolicy, port int, ns, podKey, podVal string) bool {
	for _, e := range np.Spec.Egress {
		if !rulePortsTCP(e.Ports, port) {
			continue
		}
		for _, peer := range e.To {
			if peerMatchesNamespacedPod(peer, ns, podKey, podVal) {
				return true
			}
		}
	}
	return false
}

// hasPeerEgressRule reports whether np has an egress rule on tcp/port to a peer
// selected by the workspace managed-by pod label and NO namespace selector
// (same-namespace peers only).
func hasPeerEgressRule(np *networkingv1.NetworkPolicy, port int) bool {
	for _, e := range np.Spec.Egress {
		if !rulePortsTCP(e.Ports, port) {
			continue
		}
		for _, peer := range e.To {
			if peerMatchesWorkspacePod(peer) {
				return true
			}
		}
	}
	return false
}

// hasPeerIngressRule reports whether np has an ingress rule on tcp/port from a
// peer selected by the workspace managed-by pod label.
func hasPeerIngressRule(np *networkingv1.NetworkPolicy, port int) bool {
	for _, in := range np.Spec.Ingress {
		if !rulePortsTCP(in.Ports, port) {
			continue
		}
		for _, peer := range in.From {
			if peerMatchesWorkspacePod(peer) {
				return true
			}
		}
	}
	return false
}

func rulePortsTCP(ports []networkingv1.NetworkPolicyPort, want int) bool {
	for _, p := range ports {
		if p.Port == nil || p.Port.IntValue() != want {
			continue
		}
		if p.Protocol == nil || *p.Protocol == corev1.ProtocolTCP {
			return true
		}
	}
	return false
}

func peerMatchesNamespacedPod(peer networkingv1.NetworkPolicyPeer, ns, podKey, podVal string) bool {
	if peer.NamespaceSelector == nil || peer.PodSelector == nil {
		return false
	}
	if peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != ns {
		return false
	}
	return peer.PodSelector.MatchLabels[podKey] == podVal
}

func peerMatchesWorkspacePod(peer networkingv1.NetworkPolicyPeer) bool {
	if peer.PodSelector == nil {
		return false
	}
	return peer.PodSelector.MatchLabels[workspaceManagedLabelKey] == workspaceManagedLabelValue
}

// assertNoWildcardEgress fails if any egress peer is an open wildcard: a nil/
// empty peer, an open ipBlock (0.0.0.0/0 with no except), or an empty rule with
// no peers (which allows egress to all destinations).
func assertNoWildcardEgress(t *testing.T, np *networkingv1.NetworkPolicy) {
	t.Helper()
	for i, e := range np.Spec.Egress {
		if len(e.To) == 0 {
			t.Errorf("egress rule %d has empty To — allows all destinations (rule 05.5)", i)
			continue
		}
		for j, peer := range e.To {
			assertPeerNotWildcard(t, peer, "egress", i, j)
		}
	}
}

func assertNoWildcardIngress(t *testing.T, np *networkingv1.NetworkPolicy) {
	t.Helper()
	for i, in := range np.Spec.Ingress {
		if len(in.From) == 0 {
			t.Errorf("ingress rule %d has empty From — allows all sources (rule 05.5)", i)
			continue
		}
		for j, peer := range in.From {
			assertPeerNotWildcard(t, peer, "ingress", i, j)
		}
	}
}

func assertPeerNotWildcard(t *testing.T, peer networkingv1.NetworkPolicyPeer, dir string, i, j int) {
	t.Helper()
	// An open ipBlock is a wildcard.
	if peer.IPBlock != nil {
		if peer.IPBlock.CIDR == "0.0.0.0/0" || peer.IPBlock.CIDR == "::/0" {
			t.Errorf("%s rule %d peer %d is an open ipBlock %q (rule 05.5)", dir, i, j, peer.IPBlock.CIDR)
		}
		return
	}
	// A peer with neither a namespace selector nor a pod selector matches every
	// pod in the namespace — an unbounded wildcard.
	if peer.NamespaceSelector == nil && peer.PodSelector == nil {
		t.Errorf("%s rule %d peer %d has no selector and no ipBlock — unbounded wildcard (rule 05.5)", dir, i, j)
		return
	}
	// An empty namespace selector (matches all namespaces) paired with an empty
	// pod selector is also a wildcard.
	if peer.NamespaceSelector != nil && isEmptySelectorLabels(peer.NamespaceSelector.MatchLabels, peer.NamespaceSelector.MatchExpressions) &&
		(peer.PodSelector == nil || isEmptySelectorLabels(peer.PodSelector.MatchLabels, peer.PodSelector.MatchExpressions)) {
		t.Errorf("%s rule %d peer %d has empty namespace+pod selectors — wildcard (rule 05.5)", dir, i, j)
	}
}

func isEmptySelectorLabels(labels map[string]string, exprs []metav1.LabelSelectorRequirement) bool {
	return len(labels) == 0 && len(exprs) == 0
}
