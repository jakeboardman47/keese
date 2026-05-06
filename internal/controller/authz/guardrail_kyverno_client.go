// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"context"
	"fmt"
	"strings"

	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// defaultFieldOwner is the SSA field-owner string for the GuardrailBinding controller.
	// See .claude/rules/04-kubernetes.md §7.
	defaultFieldOwner = "keese-guardrailbinding-controller"

	// clusterPolicyNameMaxLen is the maximum length of a Kubernetes object name (DNS subdomain).
	clusterPolicyNameMaxLen = 253
)

// ClientKyvernoPolicyProjector is a production KyvernoPolicyProjector that uses
// Server-Side Apply to project keese GuardrailBinding composed-Kyverno-policies
// into kyvernov1.ClusterPolicy objects.
//
// Note: ClusterPolicy is defined in github.com/kyverno/kyverno/api/kyverno/v1,
// not v2. Kyverno v2 contains PolicyException, CleanupPolicy, and UpdateRequest.
type ClientKyvernoPolicyProjector struct {
	client     client.Client
	fieldOwner string
}

// NewClientKyvernoPolicyProjector returns a production-ready KyvernoPolicyProjector.
// The client must have kyvernov1 registered in its scheme (via kyvernov1.AddToScheme).
func NewClientKyvernoPolicyProjector(c client.Client) *ClientKyvernoPolicyProjector {
	return &ClientKyvernoPolicyProjector{
		client:     c,
		fieldOwner: defaultFieldOwner,
	}
}

// clusterPolicyName produces the ClusterPolicy name for a given binding + policyRef triple.
// Format: keese-<bindingNamespace>-<bindingName>-<policyRef>, truncated to 253 chars.
// All components are lowercased; "/" and "_" are replaced with "-".
func clusterPolicyName(bindingNamespace, bindingName, policyRef string) string {
	sanitize := func(s string) string {
		s = strings.ToLower(s)
		s = strings.ReplaceAll(s, "/", "-")
		s = strings.ReplaceAll(s, "_", "-")
		return s
	}
	name := fmt.Sprintf("keese-%s-%s-%s",
		sanitize(bindingNamespace),
		sanitize(bindingName),
		sanitize(policyRef),
	)
	if len(name) > clusterPolicyNameMaxLen {
		name = name[:clusterPolicyNameMaxLen]
	}
	return name
}

// buildClusterPolicy constructs the desired kyvernov1.ClusterPolicy for SSA.
// Exported for shape-testing in unit tests without requiring an API server.
//
// The resulting ClusterPolicy has an empty Spec (no rules); it acts as a keese-owned
// placeholder that records the owning GuardrailBinding via annotations. When the
// GuardrailBinding design matures to include inline rule content
// (see docs/specs/guardrail.operator.keese.ai-v1alpha1.md), the Spec.Rules field will
// be populated by the merge layer.
func buildClusterPolicy(bindingNamespace, bindingName, policyRef string) *kyvernov1.ClusterPolicy {
	name := clusterPolicyName(bindingNamespace, bindingName, policyRef)
	return &kyvernov1.ClusterPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kyverno.io/v1",
			Kind:       "ClusterPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				"keese.ai/owner":      fmt.Sprintf("%s/%s", bindingNamespace, bindingName),
				"keese.ai/policy-ref": policyRef,
			},
			Labels: map[string]string{
				"keese.ai/managed":    "true",
				"keese.ai/group":      "guardrail.operator.keese.ai",
				"keese.ai/policy-ref": sanitizeLabel(policyRef),
			},
		},
		Spec: kyvernov1.Spec{},
	}
}

// Apply SSA-patches a kyvernov1.ClusterPolicy for the given binding+policyRef.
// The policy is annotated with keese.ai/owner to record the owning GuardrailBinding.
// Apply is idempotent: calling with the same arguments produces a no-op on the second call.
func (p *ClientKyvernoPolicyProjector) Apply(ctx context.Context, bindingNamespace, bindingName, policyRef string) error {
	desired := buildClusterPolicy(bindingNamespace, bindingName, policyRef)
	if err := p.client.Patch(ctx, desired, client.Apply,
		client.FieldOwner(p.fieldOwner),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("SSA ClusterPolicy %q: %w", desired.Name, err)
	}
	return nil
}

// Delete removes the SSA-owned ClusterPolicy copy for the given binding+policyRef.
// Missing objects are silently ignored (idempotent).
func (p *ClientKyvernoPolicyProjector) Delete(ctx context.Context, bindingNamespace, bindingName, policyRef string) error {
	name := clusterPolicyName(bindingNamespace, bindingName, policyRef)

	obj := &kyvernov1.ClusterPolicy{}
	if err := p.client.Get(ctx, types.NamespacedName{Name: name}, obj); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("fetching ClusterPolicy %q for deletion: %w", name, err)
	}

	propagation := metav1.DeletePropagationForeground
	if err := p.client.Delete(ctx, obj, &client.DeleteOptions{
		PropagationPolicy: &propagation,
	}); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("deleting ClusterPolicy %q: %w", name, err)
	}
	return nil
}

// sanitizeLabel returns a Kubernetes label-value-safe version of s (≤63 chars).
// Label values must match [a-z0-9][-a-z0-9.]*[a-z0-9] or be empty.
// Underscores are replaced with dashes; all other non-alphanumeric/dash/dot chars
// are replaced with dashes; the result is trimmed and lowercased.
func sanitizeLabel(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		default:
			// Replace underscore, slash, and any other chars with dash.
			b.WriteRune('-')
		}
	}
	result := b.String()
	// Trim leading/trailing non-alphanumeric (label values must start/end with alnum or be empty).
	result = strings.Trim(result, "-.")
	if len(result) > 63 {
		result = result[:63]
		result = strings.TrimRight(result, "-.")
	}
	return result
}

var _ KyvernoPolicyProjector = &ClientKyvernoPolicyProjector{}
