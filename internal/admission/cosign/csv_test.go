// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package cosign

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func mkCSV(images []string, related []string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	containers := []any{}
	for _, img := range images {
		containers = append(containers, map[string]any{
			"name":  "manager",
			"image": img,
		})
	}
	relatedImages := []any{}
	for _, r := range related {
		relatedImages = append(relatedImages, map[string]any{
			"name":  "related",
			"image": r,
		})
	}
	u.Object = map[string]any{
		"spec": map[string]any{
			"relatedImages": relatedImages,
			"install": map[string]any{
				"spec": map[string]any{
					"deployments": []any{
						map[string]any{
							"spec": map[string]any{
								"template": map[string]any{
									"spec": map[string]any{
										"containers":     containers,
										"initContainers": []any{},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	u.SetGroupVersionKind(CSVGVK)
	return u
}

func TestImagesFromCSV_Dedup(t *testing.T) {
	csv := mkCSV(
		[]string{"ghcr.io/keese-ai/keese@sha256:abc", "ghcr.io/keese-ai/keese@sha256:abc"},
		[]string{"ghcr.io/keese-ai/keese@sha256:abc", "ghcr.io/keese-ai/keese-bundle@sha256:def"},
	)
	got, err := imagesFromCSV(csv)
	if err != nil {
		t.Fatalf("imagesFromCSV: %v", err)
	}
	want := []string{
		"ghcr.io/keese-ai/keese-bundle@sha256:def",
		"ghcr.io/keese-ai/keese@sha256:abc",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestImagesFromCSV_Empty(t *testing.T) {
	csv := mkCSV(nil, nil)
	got, err := imagesFromCSV(csv)
	if err != nil {
		t.Fatalf("imagesFromCSV: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestImagesFromCSV_InitContainersIncluded(t *testing.T) {
	csv := &unstructured.Unstructured{}
	csv.SetGroupVersionKind(CSVGVK)
	csv.Object = map[string]any{
		"spec": map[string]any{
			"install": map[string]any{
				"spec": map[string]any{
					"deployments": []any{
						map[string]any{
							"spec": map[string]any{
								"template": map[string]any{
									"spec": map[string]any{
										"initContainers": []any{
											map[string]any{
												"image": "ghcr.io/keese-ai/migrator@sha256:zzz",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	got, err := imagesFromCSV(csv)
	if err != nil {
		t.Fatalf("imagesFromCSV: %v", err)
	}
	if len(got) != 1 || got[0] != "ghcr.io/keese-ai/migrator@sha256:zzz" {
		t.Errorf("got %v", got)
	}
}

func TestParseInstallPlan(t *testing.T) {
	ip := &unstructured.Unstructured{}
	ip.SetGroupVersionKind(InstallPlanGVK)
	ip.SetNamespace("operators")
	ip.Object["metadata"] = map[string]any{"namespace": "operators"}
	ip.Object["spec"] = map[string]any{
		"clusterServiceVersionNames": []any{"keese.v0.0.2", "keese.v0.0.1"},
	}
	ref, err := parseInstallPlan(ip)
	if err != nil {
		t.Fatalf("parseInstallPlan: %v", err)
	}
	if ref.Namespace != "operators" {
		t.Errorf("namespace = %q", ref.Namespace)
	}
	want := []string{"keese.v0.0.1", "keese.v0.0.2"}
	if !reflect.DeepEqual(ref.CSVNames, want) {
		t.Errorf("CSVNames = %v, want %v (sorted)", ref.CSVNames, want)
	}
}

func TestParseInstallPlan_MissingField(t *testing.T) {
	ip := &unstructured.Unstructured{}
	ip.SetGroupVersionKind(InstallPlanGVK)
	ip.Object["spec"] = map[string]any{}
	if _, err := parseInstallPlan(ip); err == nil {
		t.Fatalf("expected error on missing spec.clusterServiceVersionNames")
	}
}
