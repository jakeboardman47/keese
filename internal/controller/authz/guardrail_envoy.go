// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
)

const (
	// ConditionCELCompilationFailed is the condition type set when CEL compilation fails.
	// The controller sets this Degraded condition and emits ReasonCELCompileError.
	ConditionCELCompilationFailed = "CELCompilationFailed"

	// securityPolicyAPIVersion is the APIVersion for Envoy Gateway SecurityPolicy objects.
	securityPolicyAPIVersion = "gateway.envoyproxy.io/v1alpha1"
	// securityPolicyKind is the Kind for Envoy Gateway SecurityPolicy.
	securityPolicyKind = "SecurityPolicy"

	// toolNameHeader is the HTTP request header the ext_authz gateway sets to the
	// resolved MCP tool name. SecurityPolicy authorization rules match on this header.
	// This is the same header keese-authz reads in TD-P1-03.
	toolNameHeader = "x-mcp-tool-name"

	// annotationBindingRef is written onto projected SecurityPolicies to identify
	// which GuardrailBinding owns them (for debuggability and selective cleanup).
	annotationBindingRef = "keese.ai/guardrailbinding-ref"
)

// EnvoySecurityPolicyProjector abstracts SSA projection of Envoy SecurityPolicy
// objects derived from a GuardrailBinding's effective tool policy. Compile-time
// CEL validation is the responsibility of the production implementation; test
// fakes may skip compilation and return configurable errors instead.
type EnvoySecurityPolicyProjector interface {
	// Apply compiles the effective policy's tool allow/deny lists into a CEL
	// expression, validates it, then SSA-applies a SecurityPolicy encoding the
	// guardrail authorisation rules.
	//
	// Returns a non-nil error containing "[CEL]" prefix if compilation fails so
	// the controller can distinguish compile failures from API errors.
	Apply(ctx context.Context, binding *authzv1alpha1.GuardrailBinding, ep *authzv1alpha1.EffectivePolicy) error

	// Delete removes the SSA-projected SecurityPolicy for the given binding.
	// Missing objects are silently ignored (idempotent).
	Delete(ctx context.Context, bindingNamespace, bindingName string) error
}

// ClientSecurityPolicyProjector is the production implementation of
// EnvoySecurityPolicyProjector. It uses github.com/google/cel-go for CEL
// compilation and sigs.k8s.io/controller-runtime SSA for object projection.
//
// Field owner is always "keese-guardrailbinding-controller" (rule 04.7).
type ClientSecurityPolicyProjector struct {
	c          client.Client
	fieldOwner string
}

// NewClientSecurityPolicyProjector constructs a ClientSecurityPolicyProjector.
func NewClientSecurityPolicyProjector(c client.Client) *ClientSecurityPolicyProjector {
	return &ClientSecurityPolicyProjector{
		c:          c,
		fieldOwner: fieldOwner, // "keese-guardrailbinding-controller" from constants.go
	}
}

// Apply compiles the effective tool policy into a CEL expression for validation,
// then SSA-applies a SecurityPolicy whose Authorization.Rules encode the allow/deny
// lists as header-match rules on the x-mcp-tool-name request header.
//
// CEL expression compiled (for validation only — not projected as CEL filter):
//
//	tool_name in [<allow list>] && !(tool_name in [<deny list>])
//
// If either list is empty the corresponding sub-expression is omitted.
// Compile errors are returned with a "[CEL]" prefix so the controller can set
// the CELCompilationFailed condition distinct from API errors.
func (p *ClientSecurityPolicyProjector) Apply(
	ctx context.Context,
	binding *authzv1alpha1.GuardrailBinding,
	ep *authzv1alpha1.EffectivePolicy,
) error {
	// --- Step 1: compile CEL expression for structural validation ---
	expr, err := buildToolPolicyCELExpression(ep)
	if err != nil {
		// buildToolPolicyCELExpression only fails when the expression is syntactically
		// invalid — this should not happen with well-formed allow/deny string slices,
		// but surface it as a CEL error for safety.
		return fmt.Errorf("[CEL] build expression: %w", err)
	}

	if err := compileCELExpression(expr); err != nil {
		return fmt.Errorf("[CEL] compile: %w", err)
	}

	// --- Step 2: SSA-apply SecurityPolicy ---
	// Determine the namespace and name for the projected SecurityPolicy.
	// When spec.envoy.securityPolicyRef is set, use that namespace; otherwise
	// fall back to the binding's own namespace.
	targetNS := binding.Namespace
	spName := securityPolicyNameFor(binding.Namespace, binding.Name)
	if binding.Spec.Envoy != nil && binding.Spec.Envoy.SecurityPolicyRef != nil {
		// Fetch the referenced SecurityPolicy to verify it is accessible.
		var ref authzv1alpha1.NamespacedRef
		ref = *binding.Spec.Envoy.SecurityPolicyRef
		var existing egv1alpha1.SecurityPolicy
		if err := p.c.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &existing); err != nil {
			return fmt.Errorf("fetching referenced SecurityPolicy %s/%s: %w", ref.Namespace, ref.Name, err)
		}
		// Project under the referenced object's coordinates.
		targetNS = ref.Namespace
		spName = ref.Name
	}

	sp := p.buildSecurityPolicy(spName, targetNS, binding, ep)
	if err := p.c.Patch(ctx, sp, client.Apply,
		client.FieldOwner(p.fieldOwner),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("applying SecurityPolicy %s/%s: %w", targetNS, spName, err)
	}
	return nil
}

// Delete removes the SSA-projected SecurityPolicy for the given binding.
// Missing objects are silently ignored.
func (p *ClientSecurityPolicyProjector) Delete(ctx context.Context, bindingNamespace, bindingName string) error {
	spName := securityPolicyNameFor(bindingNamespace, bindingName)
	sp := &egv1alpha1.SecurityPolicy{}
	if err := p.c.Get(ctx, types.NamespacedName{Name: spName, Namespace: bindingNamespace}, sp); err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("getting SecurityPolicy %s/%s for deletion: %w", bindingNamespace, spName, err)
	}
	if err := p.c.Delete(ctx, sp); err != nil && !isNotFound(err) {
		return fmt.Errorf("deleting SecurityPolicy %s/%s: %w", bindingNamespace, spName, err)
	}
	return nil
}

// securityPolicyNameFor derives the SecurityPolicy name from the binding ref.
// Format: keese-guardrail-<bindingNamespace>-<bindingName> (truncated if needed).
func securityPolicyNameFor(bindingNamespace, bindingName string) string {
	base := fmt.Sprintf("keese-guardrail-%s-%s", bindingNamespace, bindingName)
	// Kubernetes names are limited to 253 characters.
	if len(base) > 253 {
		base = base[:253]
	}
	return base
}

// buildToolPolicyCELExpression constructs a CEL expression string from the
// effective tool allow/deny lists. The expression is used only for structural
// validation via cel-go; it is NOT projected as a filter into the SecurityPolicy.
//
// Expression grammar:
//
//	<allow-expr> && <deny-expr>
//
// Where:
//
//	allow-expr = tool_name in ["a", "b", ...]   (omitted if allow list is empty)
//	deny-expr  = !(tool_name in ["x", "y", ...]) (omitted if deny list is empty)
//
// Returns an empty string (always-pass) when both lists are empty.
func buildToolPolicyCELExpression(ep *authzv1alpha1.EffectivePolicy) (string, error) {
	if ep == nil {
		return "true", nil
	}

	var parts []string

	if len(ep.Tools.Allow) > 0 {
		parts = append(parts, fmt.Sprintf("tool_name in [%s]", quotedList(ep.Tools.Allow)))
	}

	if len(ep.Tools.Deny) > 0 {
		parts = append(parts, fmt.Sprintf("!(tool_name in [%s])", quotedList(ep.Tools.Deny)))
	}

	if len(parts) == 0 {
		return "true", nil
	}
	return strings.Join(parts, " && "), nil
}

// quotedList converts a string slice into a comma-separated CEL string-literal list.
// e.g. ["a", "b"] → `"a","b"`.
func quotedList(ss []string) string {
	quoted := make([]string, len(ss))
	for i, s := range ss {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(quoted, ",")
}

// compileCELExpression compiles expr using cel-go with a single string variable
// "tool_name". Returns a non-nil error if the expression is syntactically or
// type-check invalid, or if it does not evaluate to bool.
//
// Note: per rule 02 (security), this function MUST NOT log the expression value —
// it may contain tool names that are considered internal policy data.
func compileCELExpression(expr string) error {
	env, err := cel.NewEnv(
		cel.Variable("tool_name", cel.StringType),
	)
	if err != nil {
		return fmt.Errorf("creating CEL environment: %w", err)
	}

	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return fmt.Errorf("expression parse failed: %w", issues.Err())
	}

	// Type-check: the expression must evaluate to bool.
	checked, issues := env.Check(ast)
	if issues != nil && issues.Err() != nil {
		return fmt.Errorf("expression type-check failed: %w", issues.Err())
	}

	// Verify the output type is bool (not dyn or another type).
	if !checked.OutputType().IsExactType(cel.BoolType) {
		return fmt.Errorf("expression output type must be bool, got %s", checked.OutputType())
	}

	return nil
}

// buildSecurityPolicy constructs the SSA SecurityPolicy object encoding the
// guardrail's effective tool policy as Envoy Gateway Authorization rules.
//
// Rules structure:
//   - One ALLOW rule per tool in ep.Tools.Allow (header exact-match on x-mcp-tool-name)
//   - One DENY rule per tool in ep.Tools.Deny (header exact-match on x-mcp-tool-name)
//   - DefaultAction: Deny (fail-closed when no allow-list match)
//     (overridden to Allow when allow list is empty, since empty = allow all)
//
// The SecurityPolicy targets all HTTPRoutes in the binding namespace that carry
// the keese.ai/managed=true label (same targeting strategy as BackendTrafficPolicy).
func (p *ClientSecurityPolicyProjector) buildSecurityPolicy(
	name, namespace string,
	binding *authzv1alpha1.GuardrailBinding,
	ep *authzv1alpha1.EffectivePolicy,
) *egv1alpha1.SecurityPolicy {
	gwGroup := gwapiv1.Group("gateway.networking.k8s.io")
	gwKind := gwapiv1.Kind("HTTPRoute")

	defaultAction := egv1alpha1.AuthorizationActionDeny
	if len(ep.Tools.Allow) == 0 {
		// Empty allow list means "allow all" per merge lattice semantics.
		defaultAction = egv1alpha1.AuthorizationActionAllow
	}

	var rules []egv1alpha1.AuthorizationRule

	// Deny rules first (deny-wins — union of all deny lists).
	for _, tool := range ep.Tools.Deny {
		rules = append(rules, egv1alpha1.AuthorizationRule{
			Action: egv1alpha1.AuthorizationActionDeny,
			Principal: egv1alpha1.Principal{
				Headers: []egv1alpha1.AuthorizationHeaderMatch{
					{
						Name:   toolNameHeader,
						Values: []string{tool},
					},
				},
			},
		})
	}

	// Allow rules (intersection of all allow lists).
	for _, tool := range ep.Tools.Allow {
		rules = append(rules, egv1alpha1.AuthorizationRule{
			Action: egv1alpha1.AuthorizationActionAllow,
			Principal: egv1alpha1.Principal{
				Headers: []egv1alpha1.AuthorizationHeaderMatch{
					{
						Name:   toolNameHeader,
						Values: []string{tool},
					},
				},
			},
		})
	}

	return &egv1alpha1.SecurityPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: securityPolicyAPIVersion,
			Kind:       securityPolicyKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				annotationBindingRef: fmt.Sprintf("%s/%s", binding.Namespace, binding.Name),
			},
			Labels: map[string]string{
				"keese.ai/managed":              "true",
				"keese.ai/guardrailbinding-ref": fmt.Sprintf("%s.%s", binding.Namespace, binding.Name),
			},
		},
		Spec: egv1alpha1.SecurityPolicySpec{
			PolicyTargetReferences: egv1alpha1.PolicyTargetReferences{
				TargetSelectors: []egv1alpha1.TargetSelector{
					{
						Group: &gwGroup,
						Kind:  gwKind,
						MatchLabels: map[string]string{
							"keese.ai/managed": "true",
						},
					},
				},
			},
			Authorization: &egv1alpha1.Authorization{
				Rules:         rules,
				DefaultAction: &defaultAction,
			},
		},
	}
}

// isNotFound returns true when err is a Kubernetes "not found" error.
// Uses client.IgnoreNotFound: if the error is not-found, IgnoreNotFound returns nil.
func isNotFound(err error) bool {
	return err != nil && client.IgnoreNotFound(err) == nil
}

var _ EnvoySecurityPolicyProjector = &ClientSecurityPolicyProjector{}
