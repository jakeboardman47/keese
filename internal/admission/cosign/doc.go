// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Package cosign implements the pre-install ValidatingWebhook that
// rejects OLM InstallPlan resources whose target ClusterServiceVersion
// references unsigned keese images.
//
// Per design 14a §4 "Bundle Signing and Upgrade Verification" and
// rule 05.12 "Images pinned by digest in CSVs and production
// overlays."
//
// The webhook runs as cmd/keese-cosign-webhook. For each
// InstallPlan create/update:
//
//  1. Walks spec.clusterServiceVersionNames, fetches each CSV from
//     the apiserver.
//  2. Pulls the deployment + relatedImages references out of the CSV.
//  3. For every image whose registry path matches the keese
//     allowlist (default: ghcr.io/keese-ai/), invokes cosign verify
//     keyless — pinning the GitHub OIDC issuer + a workflow-identity
//     regexp.
//  4. Denies with reason=BundleUnsigned if any keese image is
//     unsigned, untagged, or signed by an unexpected identity.
//
// Non-keese images pass through unconditionally; this webhook only
// gates keese-controlled supply chain.
//
// Break-glass: the webhook honors the rule 05.13 namespace label
// `keese.ai/break-glass=true` together with annotation
// `keese.ai/unsafe-allow-unsigned=true` on the InstallPlan. Both
// must be set; either alone is rejected.
package cosign
