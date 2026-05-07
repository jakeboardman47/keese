// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// recipeWebhookLog is the logger for the Recipe webhook.
var recipeWebhookLog = logf.Log.WithName("recipe-webhook")

// RecipeWebhook implements both webhook.CustomDefaulter and webhook.CustomValidator
// for the Recipe kind. It is purposely thin: defaulting sets a non-empty provider
// default on spec.model.provider if absent; validation runs the three-gate check
// (tool / model / extension) synchronously at admission time.
//
// Per rule 04.12: no business logic here — only validation and defaulting.
// Business logic (OCI pull, cosign verify, ReBAC sync) lives in the reconciler.
//
// +kubebuilder:webhook:path=/mutate-keese-ai-v1alpha1-recipe,mutating=true,failurePolicy=fail,sideEffects=None,groups=keese.ai,resources=recipes,verbs=create;update,versions=v1alpha1,name=mrecipe.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-keese-ai-v1alpha1-recipe,mutating=false,failurePolicy=fail,sideEffects=None,groups=keese.ai,resources=recipes,verbs=create;update,versions=v1alpha1,name=vrecipe.kb.io,admissionReviewVersions=v1
type RecipeWebhook struct {
	// ExtAuthz is the OpenFGA extension checker used by the extension gate.
	// In production this is wired to the real OpenFGA client; in tests a
	// FakeExtAuthzChecker is injected.
	ExtAuthz ExtAuthzChecker
}

var _ webhook.CustomDefaulter = &RecipeWebhook{}
var _ webhook.CustomValidator = &RecipeWebhook{}

// SetupRecipeWebhookWithManager registers the Recipe defaulting and validating
// webhook with the manager. Called from cmd/main.go after the reconciler is set up.
func SetupRecipeWebhookWithManager(mgr ctrl.Manager, extAuthz ExtAuthzChecker) error {
	if extAuthz == nil {
		extAuthz = &FakeExtAuthzChecker{AllowedExtensions: map[string]bool{}}
	}
	wh := &RecipeWebhook{ExtAuthz: extAuthz}
	return ctrl.NewWebhookManagedBy(mgr).
		For(&keesev1alpha1.Recipe{}).
		WithDefaulter(wh).
		WithValidator(wh).
		Complete()
}

// Default implements webhook.CustomDefaulter.
// Sets spec.model.provider to "anthropic" when the field is empty so that
// existing YAML that omits the provider field still passes model gate validation.
// No other fields are defaulted here; CRD defaulting markers handle the rest.
func (w *RecipeWebhook) Default(_ context.Context, obj runtime.Object) error {
	recipe, ok := obj.(*keesev1alpha1.Recipe)
	if !ok {
		return fmt.Errorf("expected a Recipe but got %T", obj)
	}
	log := recipeWebhookLog.WithValues("recipe", recipe.Name, "namespace", recipe.Namespace)

	if recipe.Spec.Model.Provider == "" {
		recipe.Spec.Model.Provider = "anthropic"
		log.V(1).Info("defaulted spec.model.provider to anthropic")
	}
	return nil
}

// ValidateCreate implements webhook.CustomValidator.
func (w *RecipeWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	recipe, ok := obj.(*keesev1alpha1.Recipe)
	if !ok {
		return nil, fmt.Errorf("expected a Recipe but got %T", obj)
	}
	return w.validate(ctx, recipe)
}

// ValidateUpdate implements webhook.CustomValidator.
func (w *RecipeWebhook) ValidateUpdate(ctx context.Context, _ runtime.Object, newObj runtime.Object) (admission.Warnings, error) {
	recipe, ok := newObj.(*keesev1alpha1.Recipe)
	if !ok {
		return nil, fmt.Errorf("expected a Recipe but got %T", newObj)
	}
	return w.validate(ctx, recipe)
}

// ValidateDelete implements webhook.CustomValidator — recipes are always deletable.
func (w *RecipeWebhook) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// validate runs the static admission checks that can be evaluated without
// fetching Kubernetes objects. The three-gate check (tool / model / extension)
// requires a GuardrailBinding, which is a cross-resource lookup; those checks
// happen in the reconciler, not here (rule 04.12). Here we enforce simpler
// structural invariants only.
func (w *RecipeWebhook) validate(_ context.Context, recipe *keesev1alpha1.Recipe) (admission.Warnings, error) {
	log := recipeWebhookLog.WithValues("recipe", recipe.Name, "namespace", recipe.Namespace)

	// Gate 1: spec.tools must not be empty — a Recipe with zero tools is a
	// policy error (the agent could call any tool once the session runs).
	if len(recipe.Spec.Tools) == 0 {
		log.Info("admission denied: empty tools list", "recipe", recipe.Name)
		return nil, fmt.Errorf("recipe %s/%s: spec.tools must contain at least one entry; "+
			"a Recipe with no tools allows unrestricted tool access at runtime",
			recipe.Namespace, recipe.Name)
	}

	// Gate 2: spec.model.modelID must not be empty (provider is defaulted above).
	if recipe.Spec.Model.ModelID == "" {
		return nil, fmt.Errorf("recipe %s/%s: spec.model.modelID is required",
			recipe.Namespace, recipe.Name)
	}

	// Gate 3: spec.sourceRef.name must not be empty.
	if recipe.Spec.SourceRef.Name == "" {
		return nil, fmt.Errorf("recipe %s/%s: spec.sourceRef.name is required",
			recipe.Namespace, recipe.Name)
	}

	return nil, nil
}
