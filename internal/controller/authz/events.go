// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

// Event reason constants for the OIDCProvider controller.
// All recorder.Eventf calls MUST use one of these constants — no free-text reasons.
// See .claude/rules/04-kubernetes.md §11.
const (
	// Template lifecycle events.
	ReasonTemplateInvalid             = "TemplateInvalid"
	ReasonAudienceTemplateEvalError   = "AudienceTemplateEvalError"
	ReasonMissingWorkflowAudience     = "MissingWorkflowAudience"
	ReasonTemplateValidationSucceeded = "TemplateValidationSucceeded"

	// OIDC provider reference events (used by Tenant controller cross-references).
	ReasonOIDCProviderMissing  = "OIDCProviderMissing"
	ReasonOIDCProviderDegraded = "OIDCProviderDegraded"

	// JWKS reachability events.
	ReasonJWKSUnreachable = "JWKSUnreachable"
	ReasonJWKSReachable   = "JWKSReachable"

	// Cache flush events.
	ReasonCacheFlushComplete = "CacheFlushComplete"
	ReasonCacheFlushTimeout  = "CacheFlushTimeout"

	// Bootstrap events.
	ReasonBootstrapCRPreserved = "BootstrapCRPreserved"
)
