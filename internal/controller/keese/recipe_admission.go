// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"
	"time"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// AdmissionError is returned when one of the three gates denies admission.
type AdmissionError struct {
	// Gate is one of "tool", "model", "extension".
	Gate string
	// Reason is the event reason constant (e.g. ReasonRecipeToolNotAllowed).
	Reason string
	// Message is a human-readable denial message (no credential material).
	Message string
}

func (e *AdmissionError) Error() string {
	return fmt.Sprintf("admission denied [gate=%s reason=%s]: %s", e.Gate, e.Reason, e.Message)
}

// ExtAuthzChecker abstracts the OpenFGA extension check.
// Real implementation calls OpenFGA with the workspace SA token;
// FakeExtAuthzChecker is used in tests.
type ExtAuthzChecker interface {
	// CheckExtensionEnabled returns nil if the extension is enabled for the workspace.
	// Returns context.DeadlineExceeded on timeout (>500ms), which triggers fail-closed 503.
	CheckExtensionEnabled(ctx context.Context, extensionName, extensionNS, workspaceName string) error
}

// FakeExtAuthzChecker is a test double for ExtAuthzChecker.
// AllowedExtensions is the set of extension names that will be allowed.
// If TimeoutOnCheck is true, CheckExtensionEnabled returns DeadlineExceeded.
type FakeExtAuthzChecker struct {
	AllowedExtensions map[string]bool
	TimeoutOnCheck    bool
	DenyExtensions    map[string]bool
}

func (f *FakeExtAuthzChecker) CheckExtensionEnabled(_ context.Context, extensionName, _, _ string) error {
	if f.TimeoutOnCheck {
		return context.DeadlineExceeded
	}
	if f.DenyExtensions != nil && f.DenyExtensions[extensionName] {
		return fmt.Errorf("extension %q not enabled for workspace", extensionName)
	}
	if f.AllowedExtensions != nil && !f.AllowedExtensions[extensionName] {
		return fmt.Errorf("extension %q not enabled for workspace", extensionName)
	}
	return nil
}

var _ ExtAuthzChecker = &FakeExtAuthzChecker{}

// EffectivePolicy is the subset of GuardrailBinding.status.effectivePolicy
// fields used by the admission gates.
type EffectivePolicy struct {
	// AllowedTools is the set of tool names permitted by the GuardrailBinding.
	AllowedTools map[string]bool
	// AllowedModels is the set of "provider/modelID" strings permitted.
	AllowedModels map[string]bool
	// ObservedGeneration is the generation of the GuardrailBinding that produced
	// this policy. Used for TOCTOU freshness checks.
	ObservedGeneration int64
}

// AdmissionRequest carries the inputs for the three-gate check.
type AdmissionRequest struct {
	Recipe *keesev1alpha1.Recipe
	// WorkspaceName is the workspace being admitted.
	WorkspaceName string
	// WorkspaceGuardrailGeneration is Workspace.status.guardrailGeneration.
	// When non-zero, EffectivePolicy.ObservedGeneration must match.
	WorkspaceGuardrailGeneration int64
	// EffectivePolicy is read from GuardrailBinding.status.effectivePolicy.
	EffectivePolicy *EffectivePolicy
}

// extAuthzTimeout is the maximum wait for an OpenFGA extension check (spec §three-gate).
const extAuthzTimeout = 500 * time.Millisecond

// CheckThreeGates runs all three admission gates in order.
// Gates are fail-closed; partial admission is forbidden.
// Returns an *AdmissionError on any gate failure.
func CheckThreeGates(ctx context.Context, req AdmissionRequest, extChecker ExtAuthzChecker) error {
	if err := checkStalePolicy(req); err != nil {
		return err
	}
	if err := checkToolGate(req); err != nil {
		return err
	}
	if err := checkModelGate(req); err != nil {
		return err
	}
	return checkExtensionGate(ctx, req, extChecker)
}

// checkStalePolicy returns StaleParentStatus if the GuardrailBinding's effective
// policy generation does not match the workspace's recorded guardrail generation
// (TOCTOU guard — spec §three-gate, design 06).
func checkStalePolicy(req AdmissionRequest) error {
	if req.WorkspaceGuardrailGeneration == 0 {
		// Workspace has not recorded a guardrail generation yet; skip freshness check.
		return nil
	}
	if req.EffectivePolicy == nil {
		return &AdmissionError{
			Gate:    "stale",
			Reason:  ReasonStaleParentStatus,
			Message: "effectivePolicy is nil; GuardrailBinding not yet reconciled",
		}
	}
	if req.EffectivePolicy.ObservedGeneration != req.WorkspaceGuardrailGeneration {
		return &AdmissionError{
			Gate:   "stale",
			Reason: ReasonStaleParentStatus,
			Message: fmt.Sprintf(
				"GuardrailBinding effectivePolicy generation %d does not match workspace guardrailGeneration %d",
				req.EffectivePolicy.ObservedGeneration,
				req.WorkspaceGuardrailGeneration,
			),
		}
	}
	return nil
}

// checkToolGate asserts all recipe tools are in the GuardrailBinding allow list.
func checkToolGate(req AdmissionRequest) error {
	if req.EffectivePolicy == nil {
		return &AdmissionError{
			Gate:    "tool",
			Reason:  ReasonRecipeToolNotAllowed,
			Message: "effectivePolicy is nil; cannot verify tool allowlist",
		}
	}
	for _, tool := range req.Recipe.Spec.Tools {
		if !req.EffectivePolicy.AllowedTools[tool.Name] {
			return &AdmissionError{
				Gate:   "tool",
				Reason: ReasonRecipeToolNotAllowed,
				Message: fmt.Sprintf(
					"tool %q is not in GuardrailBinding effectivePolicy.tools.allow",
					tool.Name,
				),
			}
		}
	}
	return nil
}

// checkModelGate asserts the recipe model is in the GuardrailBinding allow list.
func checkModelGate(req AdmissionRequest) error {
	if req.EffectivePolicy == nil {
		return &AdmissionError{
			Gate:    "model",
			Reason:  ReasonRecipeModelNotAllowed,
			Message: "effectivePolicy is nil; cannot verify model allowlist",
		}
	}
	modelKey := req.Recipe.Spec.Model.Provider + "/" + req.Recipe.Spec.Model.ModelID
	if !req.EffectivePolicy.AllowedModels[modelKey] {
		return &AdmissionError{
			Gate:   "model",
			Reason: ReasonRecipeModelNotAllowed,
			Message: fmt.Sprintf(
				"model %q is not in GuardrailBinding effectivePolicy allowed-model list",
				modelKey,
			),
		}
	}
	return nil
}

// checkExtensionGate calls OpenFGA for each recipe extension.
// Times out after 500ms per spec; returns fail-closed on DeadlineExceeded.
func checkExtensionGate(ctx context.Context, req AdmissionRequest, extChecker ExtAuthzChecker) error {
	for _, ext := range req.Recipe.Spec.Extensions {
		checkCtx, cancel := context.WithTimeout(ctx, extAuthzTimeout)
		err := extChecker.CheckExtensionEnabled(checkCtx, ext.Name, ext.Namespace, req.WorkspaceName)
		cancel()

		if err != nil {
			if err == context.DeadlineExceeded {
				return &AdmissionError{
					Gate:    "extension",
					Reason:  ReasonRecipeAdmitExtAuthzTimeout,
					Message: fmt.Sprintf("OpenFGA check for extension %q/%s timed out (>500ms); fail-closed", ext.Namespace, ext.Name),
				}
			}
			return &AdmissionError{
				Gate:    "extension",
				Reason:  ReasonRecipeExtensionNotEnabled,
				Message: fmt.Sprintf("extension %q/%s not enabled for workspace %q: %v", ext.Namespace, ext.Name, req.WorkspaceName, err),
			}
		}
	}
	return nil
}
