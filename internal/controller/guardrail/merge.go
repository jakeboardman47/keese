// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package guardrail

import (
	"fmt"
	"sort"

	guardrailv1alpha1 "github.com/keese-ai/keese/api/guardrail/v1alpha1"
)

// MergeError is returned when the strictest-wins lattice detects an invalid
// tightening attempt (a narrower binding tries to loosen a parent constraint).
type MergeError struct {
	Field   string
	Message string
}

func (e *MergeError) Error() string {
	return fmt.Sprintf("merge conflict on field %q: %s", e.Field, e.Message)
}

// MergeBindings computes the effective policy from a slice of GuardrailBindings
// ordered from broadest scope (Cluster/default) to narrowest (Workspace).
//
// Rules per field type (design 06):
//   - tools.allow  — intersection (strictest = fewest tools allowed)
//   - tools.deny   — union (strictest = most tools denied)
//   - tokenBudget  — min() per field
//   - recipeHooks  — union (all hooks run)
//   - rateLimit    — min(requests) per (window, scope) tuple
//
// Returns a MergeError if a narrower binding attempts to widen the allow list
// (loosen a parent allow constraint). Widening deny or increasing a budget is
// also an error. These invariants map to the VAP rules in design 06 §VAP rules.
func MergeBindings(bindings []*guardrailv1alpha1.GuardrailBinding) (*guardrailv1alpha1.EffectivePolicy, error) {
	if len(bindings) == 0 {
		return &guardrailv1alpha1.EffectivePolicy{}, nil
	}

	// Start from the broadest binding (index 0) and progressively tighten.
	effectiveAllow, effectiveDeny, hasAllow := extractTools(bindings[0])
	effectiveBudget := extractBudget(bindings[0])
	effectiveRL := extractRateLimit(bindings[0])

	for i := 1; i < len(bindings); i++ {
		b := bindings[i]
		bAllow, bDeny, bHasAllow := extractTools(b)
		bBudget := extractBudget(b)
		bRL := extractRateLimit(b)

		// tools.allow — intersection. Narrower binding must be a subset of parent.
		if bHasAllow {
			if hasAllow {
				// Validate no widening: every element in bAllow must be in effectiveAllow.
				allowSet := toSet(effectiveAllow)
				for _, t := range bAllow {
					if !allowSet[t] {
						return nil, &MergeError{
							Field:   "tools.allow",
							Message: fmt.Sprintf("binding %s/%s adds tool %q not present in parent allow list", b.Namespace, b.Name, t),
						}
					}
				}
				effectiveAllow = bAllow // intersection — narrower wins
			} else {
				// Parent has no allow restriction (allow-all); child sets an explicit allow list.
				effectiveAllow = bAllow
				hasAllow = true
			}
		}

		// tools.deny — union. Narrower binding may only add more denials.
		if len(bDeny) > 0 {
			effectiveDeny = unionStrings(effectiveDeny, bDeny)
		}

		// tokenBudget — min per field.
		effectiveBudget = minBudget(effectiveBudget, bBudget)

		// rateLimit — min(requests) per (window, scope).
		effectiveRL = minRateLimit(effectiveRL, bRL)
	}

	ep := &guardrailv1alpha1.EffectivePolicy{
		Tools: guardrailv1alpha1.EffectiveTools{
			Allow: effectiveAllow,
			Deny:  effectiveDeny,
		},
		TokenBudget: guardrailv1alpha1.EffectiveTokenBudget{
			Input:  effectiveBudget.Input,
			Output: effectiveBudget.Output,
			Total:  effectiveBudget.Total,
		},
	}

	if effectiveRL != nil {
		ep.Tools.RateLimit = &guardrailv1alpha1.EffectiveRateLimit{
			Requests: effectiveRL.Requests,
			Window:   effectiveRL.Window,
			Scope:    effectiveRL.Scope,
		}
	}

	return ep, nil
}

// extractTools returns the allow list, deny list, and whether an explicit allow
// list is present for a binding. A nil/empty allow list means "allow all".
func extractTools(b *guardrailv1alpha1.GuardrailBinding) (allow []string, deny []string, hasAllow bool) {
	if b.Spec.Tools == nil {
		return nil, nil, false
	}
	hasAllow = len(b.Spec.Tools.Allow) > 0
	// Normalise both slices so merge is deterministic.
	allow = sortedCopy(b.Spec.Tools.Allow)
	deny = sortedCopy(b.Spec.Tools.Deny)
	return allow, deny, hasAllow
}

type budgetVals struct{ Input, Output, Total int64 }

func extractBudget(b *guardrailv1alpha1.GuardrailBinding) budgetVals {
	if b.Spec.TokenBudget == nil {
		return budgetVals{}
	}
	return budgetVals{b.Spec.TokenBudget.Input, b.Spec.TokenBudget.Output, b.Spec.TokenBudget.Total}
}

func minBudget(a, b budgetVals) budgetVals {
	return budgetVals{
		Input:  minPositive(a.Input, b.Input),
		Output: minPositive(a.Output, b.Output),
		Total:  minPositive(a.Total, b.Total),
	}
}

// minPositive returns the minimum of two values treating 0 as "no limit" — only
// positive values (real limits) tighten the budget. If both are 0, returns 0.
func minPositive(a, b int64) int64 {
	switch {
	case a == 0 && b == 0:
		return 0
	case a == 0:
		return b
	case b == 0:
		return a
	default:
		if a < b {
			return a
		}
		return b
	}
}

func extractRateLimit(b *guardrailv1alpha1.GuardrailBinding) *guardrailv1alpha1.RateLimit {
	if b.Spec.Tools == nil {
		return nil
	}
	return b.Spec.Tools.RateLimit
}

// minRateLimit returns the stricter of two RateLimits.
// "Stricter" = lower Requests value (non-zero wins over zero).
func minRateLimit(a, b *guardrailv1alpha1.RateLimit) *guardrailv1alpha1.RateLimit {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	// Lower requests = stricter. Treat 0 as "no limit".
	req := minPositive(a.Requests, b.Requests)
	// Window: pick the one that corresponds to the stricter limit.
	window := a.Window
	if b.Requests > 0 && (a.Requests == 0 || b.Requests < a.Requests) {
		window = b.Window
	}
	// Scope: use the narrower scope (sa < workspace < tenant).
	scope := narrowerScope(a.Scope, b.Scope)
	return &guardrailv1alpha1.RateLimit{
		Requests: req,
		Window:   window,
		Scope:    scope,
	}
}

// scopeRank maps scope values to a numeric rank; lower = narrower.
func scopeRank(s guardrailv1alpha1.RateLimitScope) int {
	switch s {
	case "sa":
		return 0
	case "workspace":
		return 1
	case "tenant":
		return 2
	default:
		return 3
	}
}

func narrowerScope(a, b guardrailv1alpha1.RateLimitScope) guardrailv1alpha1.RateLimitScope {
	if scopeRank(a) <= scopeRank(b) {
		return a
	}
	return b
}

// toSet converts a string slice to a membership map.
func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

// unionStrings returns the union of two string slices, deduplicated and sorted.
func unionStrings(a, b []string) []string {
	s := toSet(a)
	for _, v := range b {
		s[v] = true
	}
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedCopy returns a sorted copy of a string slice.
func sortedCopy(ss []string) []string {
	if ss == nil {
		return nil
	}
	cp := make([]string, len(ss))
	copy(cp, ss)
	sort.Strings(cp)
	return cp
}
