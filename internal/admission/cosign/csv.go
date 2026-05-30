// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package cosign

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// GVKs we touch dynamically. OLM is not in our scheme; the handler
// uses the unstructured client to avoid a hard import dependency.
var (
	// CSVGVK is operators.coreos.com/v1alpha1 ClusterServiceVersion.
	CSVGVK = schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "ClusterServiceVersion",
	}

	// InstallPlanGVK is operators.coreos.com/v1alpha1 InstallPlan.
	InstallPlanGVK = schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "InstallPlan",
	}
)

// installPlanRef carries the names + namespace needed to look up the
// CSVs an InstallPlan would install.
type installPlanRef struct {
	Namespace string
	CSVNames  []string
}

func parseInstallPlan(obj *unstructured.Unstructured) (installPlanRef, error) {
	ref := installPlanRef{Namespace: obj.GetNamespace()}
	csvs, found, err := unstructured.NestedStringSlice(
		obj.Object, "spec", "clusterServiceVersionNames")
	if err != nil {
		return ref, fmt.Errorf("read spec.clusterServiceVersionNames: %w", err)
	}
	if !found {
		return ref, errors.New("spec.clusterServiceVersionNames missing")
	}
	ref.CSVNames = append(ref.CSVNames, csvs...)
	sort.Strings(ref.CSVNames)
	return ref, nil
}

// imagesFromCSV pulls every container image reference out of an OLM
// ClusterServiceVersion. Sources, in priority order:
//
//  1. spec.relatedImages[].image — the operator's own published index.
//  2. spec.install.spec.deployments[].spec.template.spec.containers[].image
//  3. spec.install.spec.deployments[].spec.template.spec.initContainers[].image
//
// Duplicates are deduped; output is sorted for stable error messages.
func imagesFromCSV(csv *unstructured.Unstructured) ([]string, error) {
	seen := map[string]struct{}{}
	add := func(s string) {
		if s == "" {
			return
		}
		seen[s] = struct{}{}
	}

	related, _, err := unstructured.NestedSlice(csv.Object, "spec", "relatedImages")
	if err != nil {
		return nil, fmt.Errorf("read spec.relatedImages: %w", err)
	}
	for _, r := range related {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if img, ok := m["image"].(string); ok {
			add(img)
		}
	}

	deployments, _, err := unstructured.NestedSlice(
		csv.Object, "spec", "install", "spec", "deployments")
	if err != nil {
		return nil, fmt.Errorf("read spec.install.spec.deployments: %w", err)
	}
	for _, d := range deployments {
		dm, ok := d.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"containers", "initContainers"} {
			cs, _, _ := unstructured.NestedSlice(dm,
				"spec", "template", "spec", key)
			for _, c := range cs {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				if img, ok := cm["image"].(string); ok {
					add(img)
				}
			}
		}
	}

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// fetchCSVs retrieves the named CSVs from the InstallPlan's
// namespace. A missing CSV is treated as a fatal admission error —
// without the manifest we cannot reason about its supply chain.
func fetchCSVs(
	ctx context.Context, c ctrlclient.Client, ref installPlanRef,
) ([]*unstructured.Unstructured, error) {
	out := make([]*unstructured.Unstructured, 0, len(ref.CSVNames))
	for _, name := range ref.CSVNames {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(CSVGVK)
		if err := c.Get(ctx, types.NamespacedName{
			Namespace: ref.Namespace, Name: name,
		}, u); err != nil {
			return nil, fmt.Errorf("get CSV %s/%s: %w",
				ref.Namespace, name, err)
		}
		out = append(out, u)
	}
	return out, nil
}
