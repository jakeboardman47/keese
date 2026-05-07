// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// recipe builds a minimal valid Recipe for use in table tests.
func minimalRecipe() *keesev1alpha1.Recipe {
	return &keesev1alpha1.Recipe{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-recipe",
			Namespace: "default",
		},
		Spec: keesev1alpha1.RecipeSpec{
			Instructions: "oci://ghcr.io/keese-ai/recipes/test:latest",
			Tools: []keesev1alpha1.RecipeTool{
				{Name: "bash"},
			},
			Model: keesev1alpha1.RecipeModel{
				Provider: "anthropic",
				ModelID:  "claude-sonnet-4-6",
			},
			SourceRef: keesev1alpha1.RecipeSourceRef{
				Name: "my-source",
			},
		},
	}
}

func TestRecipeWebhook_ValidateCreate(t *testing.T) {
	wh := &RecipeWebhook{
		ExtAuthz: &FakeExtAuthzChecker{AllowedExtensions: map[string]bool{}},
	}

	cases := []struct {
		name    string
		mutate  func(*keesev1alpha1.Recipe)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid minimal recipe",
			mutate:  func(_ *keesev1alpha1.Recipe) {},
			wantErr: false,
		},
		{
			name: "empty tools list is rejected",
			mutate: func(r *keesev1alpha1.Recipe) {
				r.Spec.Tools = nil
			},
			wantErr: true,
			errMsg:  "spec.tools must contain at least one entry",
		},
		{
			name: "empty tools slice is rejected",
			mutate: func(r *keesev1alpha1.Recipe) {
				r.Spec.Tools = []keesev1alpha1.RecipeTool{}
			},
			wantErr: true,
			errMsg:  "spec.tools must contain at least one entry",
		},
		{
			name: "empty modelID is rejected",
			mutate: func(r *keesev1alpha1.Recipe) {
				r.Spec.Model.ModelID = ""
			},
			wantErr: true,
			errMsg:  "spec.model.modelID is required",
		},
		{
			name: "empty sourceRef.name is rejected",
			mutate: func(r *keesev1alpha1.Recipe) {
				r.Spec.SourceRef.Name = ""
			},
			wantErr: true,
			errMsg:  "spec.sourceRef.name is required",
		},
		{
			name: "multiple tools accepted",
			mutate: func(r *keesev1alpha1.Recipe) {
				r.Spec.Tools = []keesev1alpha1.RecipeTool{
					{Name: "bash"},
					{Name: "python"},
					{Name: "fetch"},
				}
			},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := minimalRecipe()
			tc.mutate(r)

			_, err := wh.ValidateCreate(context.Background(), r)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.errMsg)
				}
				if tc.errMsg != "" && !containsString(err.Error(), tc.errMsg) {
					t.Fatalf("expected error to contain %q, got: %v", tc.errMsg, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			}
		})
	}
}

func TestRecipeWebhook_ValidateUpdate(t *testing.T) {
	wh := &RecipeWebhook{
		ExtAuthz: &FakeExtAuthzChecker{AllowedExtensions: map[string]bool{}},
	}

	old := minimalRecipe()
	newR := minimalRecipe()
	newR.Spec.Model.ModelID = "claude-opus-4-7"

	_, err := wh.ValidateUpdate(context.Background(), old, newR)
	if err != nil {
		t.Fatalf("valid update rejected: %v", err)
	}

	// Update that empties tools should be rejected.
	bad := minimalRecipe()
	bad.Spec.Tools = nil
	_, err = wh.ValidateUpdate(context.Background(), old, bad)
	if err == nil {
		t.Fatal("expected error for empty tools on update, got nil")
	}
}

func TestRecipeWebhook_ValidateDelete(t *testing.T) {
	wh := &RecipeWebhook{}
	_, err := wh.ValidateDelete(context.Background(), minimalRecipe())
	if err != nil {
		t.Fatalf("delete should never be rejected, got: %v", err)
	}
}

func TestRecipeWebhook_Default(t *testing.T) {
	wh := &RecipeWebhook{}

	t.Run("defaults empty provider to anthropic", func(t *testing.T) {
		r := minimalRecipe()
		r.Spec.Model.Provider = ""

		if err := wh.Default(context.Background(), r); err != nil {
			t.Fatalf("Default returned error: %v", err)
		}
		if r.Spec.Model.Provider != "anthropic" {
			t.Fatalf("expected provider=anthropic, got %q", r.Spec.Model.Provider)
		}
	})

	t.Run("does not overwrite existing provider", func(t *testing.T) {
		r := minimalRecipe()
		r.Spec.Model.Provider = "openai"

		if err := wh.Default(context.Background(), r); err != nil {
			t.Fatalf("Default returned error: %v", err)
		}
		if r.Spec.Model.Provider != "openai" {
			t.Fatalf("expected provider=openai, got %q", r.Spec.Model.Provider)
		}
	})
}

// containsString is a simple substring helper used in tests.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
