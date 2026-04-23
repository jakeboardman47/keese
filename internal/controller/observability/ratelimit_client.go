// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package observability

import (
	"context"
	"fmt"

	envoygatewayv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	// defaultFieldOwner is the SSA field owner for BackendTrafficPolicy projections.
	// Matches rule 04.7: keese-<kind>-controller.
	defaultFieldOwner = tokenBudgetFieldOwner

	// btpGroupVersion is the APIVersion written into SSA objects for BackendTrafficPolicy.
	btpAPIVersion = "gateway.envoyproxy.io/v1alpha1"
	// btpKind is the kind for BackendTrafficPolicy.
	btpKind = "BackendTrafficPolicy"

	// annotationScopeID is written onto projected BackendTrafficPolicies so the
	// controller can identify which TokenBudget scope owns them.
	annotationScopeID = "keese.ai/tokenbudget-scope-id"
	// annotationModel identifies the model dimension of the projected policy.
	annotationModel = "keese.ai/tokenbudget-model"
)

// ClientRateLimitProjector is the production SSA implementation of RateLimitProjector.
// It projects keese TokenBudget CRs into Envoy Gateway BackendTrafficPolicy objects
// via Server-Side Apply (rule 04.7).
//
// The projected BackendTrafficPolicy carries a Local rate-limit rule whose
// request-per-second ceiling is derived from RateLimitPolicy.RemainingTokens.
// A RemainingTokens value of 0 projects a ceiling of 0 rps (fully block the model).
type ClientRateLimitProjector struct {
	c          client.Client
	fieldOwner string
}

// NewClientRateLimitProjector constructs a ClientRateLimitProjector with the default
// field owner ("keese-tokenbudget-controller").
func NewClientRateLimitProjector(c client.Client) *ClientRateLimitProjector {
	return &ClientRateLimitProjector{
		c:          c,
		fieldOwner: defaultFieldOwner,
	}
}

// Apply upserts a BackendTrafficPolicy in the given namespace via SSA.
// RemainingTokens is clamped to 0 (never negative) and becomes the requests/second
// ceiling on the projected LocalRateLimit rule.
//
// The policy is tagged with keese.ai/tokenbudget-scope-id and keese.ai/tokenbudget-model
// annotations for debuggability and selective cleanup.
func (p *ClientRateLimitProjector) Apply(ctx context.Context, policy RateLimitPolicy) error {
	remaining := policy.RemainingTokens
	if remaining < 0 {
		remaining = 0
	}

	btp := p.buildBackendTrafficPolicy(policy, remaining)

	if err := p.c.Patch(ctx, btp, client.Apply,
		client.FieldOwner(p.fieldOwner),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("applying BackendTrafficPolicy %s/%s: %w", policy.Namespace, policy.Name, err)
	}
	return nil
}

// Delete removes the BackendTrafficPolicy projection for the given namespace/name.
// Idempotent: returns nil when the policy does not exist.
func (p *ClientRateLimitProjector) Delete(ctx context.Context, namespace, name string) error {
	btp := &envoygatewayv1alpha1.BackendTrafficPolicy{}
	if err := p.c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, btp); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("getting BackendTrafficPolicy %s/%s for deletion: %w", namespace, name, err)
	}
	if err := p.c.Delete(ctx, btp); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("deleting BackendTrafficPolicy %s/%s: %w", namespace, name, err)
	}
	return nil
}

// buildBackendTrafficPolicy constructs the SSA object for a given RateLimitPolicy.
// The target selector uses label keese.ai/managed=true on HTTPRoute resources in the
// policy namespace so the rate-limit rule applies to all managed routes for the scope.
func (p *ClientRateLimitProjector) buildBackendTrafficPolicy(policy RateLimitPolicy, remainingRPS int64) *envoygatewayv1alpha1.BackendTrafficPolicy {
	gwGroup := gwapiv1.Group("gateway.networking.k8s.io")
	gwKind := gwapiv1.Kind("HTTPRoute")

	// Build a LocalRateLimit rule. When remainingRPS == 0 the ceiling is 0 rps,
	// which causes Envoy to reject all requests for this scope+model pair.
	rpsRequests := uint(0)
	if remainingRPS > 0 {
		rpsRequests = uint(remainingRPS)
	}

	btp := &envoygatewayv1alpha1.BackendTrafficPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: btpAPIVersion,
			Kind:       btpKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      policy.Name,
			Namespace: policy.Namespace,
			Annotations: map[string]string{
				annotationScopeID: policy.ScopeID,
				annotationModel:   policy.Model,
			},
			Labels: map[string]string{
				"keese.ai/managed":  "true",
				"keese.ai/scope-id": policy.ScopeID,
			},
		},
		Spec: envoygatewayv1alpha1.BackendTrafficPolicySpec{
			PolicyTargetReferences: envoygatewayv1alpha1.PolicyTargetReferences{
				TargetSelectors: []envoygatewayv1alpha1.TargetSelector{
					{
						Group: &gwGroup,
						Kind:  gwKind,
						MatchLabels: map[string]string{
							"keese.ai/managed": "true",
						},
					},
				},
			},
			RateLimit: &envoygatewayv1alpha1.RateLimitSpec{
				Type: envoygatewayv1alpha1.LocalRateLimitType,
				Local: &envoygatewayv1alpha1.LocalRateLimit{
					Rules: []envoygatewayv1alpha1.RateLimitRule{
						{
							ClientSelectors: []envoygatewayv1alpha1.RateLimitSelectCondition{
								{
									Headers: []envoygatewayv1alpha1.HeaderMatch{
										{
											Type:  envoyGWHeaderMatchTypePtr(envoygatewayv1alpha1.HeaderMatchExact),
											Name:  "x-keese-scope",
											Value: strPtr(policy.ScopeID),
										},
									},
								},
							},
							Limit: envoygatewayv1alpha1.RateLimitValue{
								Requests: rpsRequests,
								Unit:     envoygatewayv1alpha1.RateLimitUnitSecond,
							},
						},
					},
				},
			},
		},
	}
	return btp
}

// envoyGWHeaderMatchTypePtr returns a pointer to an envoy-gateway HeaderMatchType.
func envoyGWHeaderMatchTypePtr(t envoygatewayv1alpha1.HeaderMatchType) *envoygatewayv1alpha1.HeaderMatchType {
	return &t
}

// strPtr returns a pointer to the given string.
func strPtr(s string) *string { return &s }

var _ RateLimitProjector = &ClientRateLimitProjector{}
