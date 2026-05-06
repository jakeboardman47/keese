// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

// Event reason constants for the GuardrailBinding controller.
// All recorder.Eventf calls MUST use one of these constants — no free-text reasons.
// See .claude/rules/04-kubernetes.md §11.
const (
	// BindingMerged is emitted when a full merge across the scope chain completes.
	ReasonBindingMerged = "BindingMerged"

	// EffectivePolicyComputed is emitted when status.effectivePolicy is written.
	ReasonEffectivePolicyComputed = "EffectivePolicyComputed"

	// DefaultBindingMissing is emitted when no cluster-default binding exists for a Tenant.
	ReasonDefaultBindingMissing = "DefaultBindingMissing"

	// MergeConflict is emitted when the strictest-wins lattice detects a conflict
	// that prevents computation (e.g. allow list expansion attempt).
	ReasonMergeConflict = "MergeConflict"

	// CELCompileError is emitted when an Envoy SecurityPolicy CEL expression fails to parse.
	ReasonCELCompileError = "CELCompileError"

	// KyvernoProjectFailed is emitted when a Kyverno ClusterPolicy SSA patch fails.
	ReasonKyvernoProjectFailed = "KyvernoProjectFailed"

	// TupleWriteFailed is emitted when syncing OpenFGA tuples fails.
	ReasonTupleWriteFailed = "TupleWriteFailed"

	// DefaultBindingReadForbidden is emitted when the controller cannot read the default binding.
	ReasonDefaultBindingReadForbidden = "DefaultBindingReadForbidden"
)
