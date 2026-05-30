// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Package featuregate is the in-process eval API for keese feature
// gates. Anchored from design 27.
//
// Public surface:
//
//	gates, err := featuregate.New(ctx, featuregate.Options{Path: "/etc/keese/features/gates.json"})
//	defer gates.Close()
//	if !gates.Enabled(ctx, featuregate.CosignInstallPlanVerify) {
//	    return passThroughResponse()
//	}
//
// Internals: an OpenFeature client wraps a custom in-process
// provider over `atomic.Value[map[string]bool]`. The provider's
// values come from a JSON file (the projection of the FeatureGate
// CRs the operator writes into ConfigMap
// `keese-system/keese-features` — see internal/controller/policy/
// featuregate_controller.go). The file is watched via fsnotify;
// reloads are atomic + lock-free on the read path.
//
// Reads emit `keese_featuregate_eval_total{gate, value, binary}` so
// dead gates surface as zero-rate and overrides surface as a
// per-gate counter delta.
package featuregate
