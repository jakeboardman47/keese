// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package policy

import "context"

// RateLimitPolicy is the controller-internal value object passed to the
// RateLimitProjector. ClientRateLimitProjector (ratelimit_client.go) is the
// production wiring; it projects this into a real
// gateway.envoyproxy.io/v1alpha1.BackendTrafficPolicy via SSA with field
// owner "keese-tokenbudget-controller" (rule 04.7). FakeRateLimitProjector
// is the test double.
type RateLimitPolicy struct {
	// Namespace is the target namespace for the projected policy.
	Namespace string
	// Name is the projected policy name, derived from the TokenBudget UID.
	Name string
	// ScopeID identifies the budget scope (tenant name or workspace UID).
	ScopeID string
	// Model is the model this rate limit applies to.
	Model string
	// RemainingTokens is the current budget ceiling (clamped to 0; never negative).
	RemainingTokens int64
}

// RateLimitProjector applies and removes the Envoy RateLimitPolicy projection
// for a TokenBudget. The real implementation performs SSA via client.Apply with
// fieldOwner "keese-tokenbudget-controller"; FakeRateLimitProjector is used in tests.
type RateLimitProjector interface {
	// Apply upserts the RateLimitPolicy projection via SSA.
	// RemainingTokens is clamped to 0 on scale-down (never negative).
	Apply(ctx context.Context, policy RateLimitPolicy) error

	// Delete removes the projected RateLimitPolicy for the given scope/name.
	// Idempotent: safe to call when the policy does not exist.
	Delete(ctx context.Context, namespace, name string) error
}

// FakeRateLimitProjector is a test double for RateLimitProjector.
// It records apply/delete calls for assertion.
//
// Kept for tests; see ratelimit_client.go for the production SSA impl.
type FakeRateLimitProjector struct {
	// Applied holds the most recent RateLimitPolicy per name.
	Applied map[string]RateLimitPolicy

	// Deleted records the names of deleted policies.
	Deleted []string

	// FailNextApply causes the next Apply call to return an error.
	FailNextApply bool

	// FailNextDelete causes the next Delete call to return an error.
	FailNextDelete bool
}

type rateLimitProjectorError struct{ op, name string }

func (e rateLimitProjectorError) Error() string {
	return "ratelimit: " + e.op + " failed for policy " + e.name + " (fake)"
}

// Apply implements RateLimitProjector.
func (f *FakeRateLimitProjector) Apply(_ context.Context, policy RateLimitPolicy) error {
	if f.FailNextApply {
		f.FailNextApply = false
		return rateLimitProjectorError{op: "apply", name: policy.Name}
	}
	if f.Applied == nil {
		f.Applied = make(map[string]RateLimitPolicy)
	}
	f.Applied[policy.Name] = policy
	return nil
}

// Delete implements RateLimitProjector.
func (f *FakeRateLimitProjector) Delete(_ context.Context, _, name string) error {
	if f.FailNextDelete {
		f.FailNextDelete = false
		return rateLimitProjectorError{op: "delete", name: name}
	}
	if f.Applied != nil {
		delete(f.Applied, name)
	}
	f.Deleted = append(f.Deleted, name)
	return nil
}

var _ RateLimitProjector = &FakeRateLimitProjector{}

// rateLimitPolicyName returns the canonical projected policy name for a TokenBudget.
// Pattern: keese-tb-<tokenbudget-uid>
func rateLimitPolicyName(tbUID string) string {
	return "keese-tb-" + tbUID
}
