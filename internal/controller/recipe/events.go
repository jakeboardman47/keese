// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package recipe

// Event reason constants for the Recipe and RecipeSource controllers.
// All recorder.Eventf calls MUST use one of these constants — no free-text reasons.
// See .claude/rules/04-kubernetes.md §11.
const (
	// RecipeSource pull and verify events.
	ReasonRecipePulled          = "RecipePulled"
	ReasonRecipeVerified        = "RecipeVerified"
	ReasonRecipeImageUnverified = "RecipeImageUnverified"
	ReasonRecipePullFailed      = "RecipePullFailed"
	ReasonOCIPullFailed         = "OCIPullFailed"
	ReasonCosignVerifyFailed    = "CosignVerifyFailed"

	// Admission gate events.
	ReasonRecipeToolNotAllowed       = "RecipeToolNotAllowed"
	ReasonRecipeModelNotAllowed      = "RecipeModelNotAllowed"
	ReasonRecipeExtensionNotEnabled  = "RecipeExtensionNotEnabled"
	ReasonRecipeSourceNotFound       = "RecipeSourceNotFound"
	ReasonRecipeAdmitExtAuthzTimeout = "RecipeAdmitExtAuthzTimeout"
	ReasonRecipeAdmissionDenied      = "RecipeAdmissionDenied"
	ReasonStaleParentStatus          = "StaleParentStatus"

	// Namespace policy events.
	ReasonDevSourceInProdNamespace = "DevSourceInProdNamespace"
	ReasonConfigMapSourceInNonDev  = "ConfigMapSourceInNonDev"

	// Lifecycle events.
	ReasonRecipeFinalizerAdded = "RecipeFinalizerAdded"
	ReasonRecipeCacheCleanup   = "RecipeCacheCleanup"
	ReasonRecipeReady          = "RecipeReady"
	ReasonRecipeFailed         = "RecipeFailed"

	// ReBAC events.
	ReasonRebacTupleWritten      = "RebacTupleWritten"
	ReasonRebacTupleDeleteFailed = "RebacTupleDeleteFailed"
)
