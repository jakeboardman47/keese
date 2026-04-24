// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

// Package authz — bootstrap_apply_test.go
//
// Asserts that every OIDCProvider bootstrap CR under config/default/bootstrap/
// is APPLY-CLEAN against the CRD schema and VAP rules loaded by the suite.
//
// Test strategy (rule 06-testing.md):
//   - Tier: integration (envtest).
//   - Method: client.Create(..., client.DryRunAll) — runs full API-server
//     admission (OpenAPI schema + CEL XValidation) without persisting the
//     object. This catches schema-validation bugs before they reach a cluster.
//   - No reconciler exercise: the controller manager is already running from
//     suite_test.go but dry-run objects are never persisted, so no reconcile
//     loop fires.
//   - One It block per file so failures point at the exact bad manifest.
//   - No sleep or Eventually needed: dry-run Apply is synchronous.
//
// Run:
//
//	KUBEBUILDER_ASSETS=$(pwd)/bin/k8s/1.30.3-darwin-arm64 \
//	  go test -tags=integration -count=1 -timeout=180s ./internal/controller/authz/...
package authz

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
)

// bootstrapFixturesDir is the path to bootstrap manifests relative to this
// test file's package root (three levels up from internal/controller/authz/).
const bootstrapFixturesDir = "../../../config/default/bootstrap"

var _ = Describe("Bootstrap OIDCProvider CRs: dry-run schema validation", func() {
	// bootstrapFiles lists every OIDCProvider bootstrap manifest.
	// Enumerate explicitly rather than glob so a missing file is a test failure,
	// not a silent no-op.
	bootstrapFiles := []string{
		"oidcprovider-kubernetes-default.yaml",
		"oidcprovider-google.yaml",
		"oidcprovider-github-actions.yaml",
		"oidcprovider-azure-entra.yaml",
		"oidcprovider-okta.yaml",
		"oidcprovider-keycloak.yaml",
		"oidcprovider-gitlab.yaml",
	}

	// decoder uses the suite's scheme (authzv1alpha1 already registered in
	// BeforeSuite) to deserialise raw YAML into typed OIDCProvider objects.
	var decoder runtime.Decoder

	BeforeEach(func() {
		// Build a new codec factory each time from the global scheme so we pick
		// up any registrations made by suite_test.go's BeforeSuite.
		decoder = serializer.NewCodecFactory(scheme.Scheme).UniversalDeserializer()
	})

	for _, filename := range bootstrapFiles {
		// Capture loop variable for the closure.
		filename := filename

		It("passes dry-run admission for "+filename, func() {
			path := filepath.Join(bootstrapFixturesDir, filename)

			By("reading " + path)
			raw, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred(), "bootstrap fixture %s must exist on disk", path)

			By("decoding " + filename + " into OIDCProvider")
			obj, gvk, err := decoder.Decode(raw, nil, nil)
			Expect(err).NotTo(HaveOccurred(), "YAML decode must succeed for %s", filename)
			Expect(gvk.Kind).To(Equal("OIDCProvider"),
				"%s must decode to Kind=OIDCProvider, got %s", filename, gvk.Kind)

			provider, ok := obj.(*authzv1alpha1.OIDCProvider)
			Expect(ok).To(BeTrue(), "decoded object must be *authzv1alpha1.OIDCProvider")
			Expect(provider.Name).NotTo(BeEmpty(), "decoded OIDCProvider must have a non-empty name")

			// Stamp a unique suffix on the name so concurrent runs in the same
			// envtest instance do not collide on the cluster-scoped resource name.
			// DryRunAll means it is never persisted, but the API server still
			// checks uniqueness in its current store during Create; the suffix
			// keeps the test hermetic if a prior run leaked an object.
			provider.Name = provider.Name + "-dryrun"

			// Ensure TypeMeta is set — required by the API server for dry-run Create.
			provider.TypeMeta = metav1.TypeMeta{
				APIVersion: authzv1alpha1.GroupVersion.String(),
				Kind:       "OIDCProvider",
			}

			By("dry-run creating " + provider.Name)
			err = k8sClient.Create(ctx, provider, client.DryRunAll)
			Expect(err).NotTo(HaveOccurred(),
				"bootstrap CR %s must pass CRD schema + CEL VAP validation (dry-run)", filename)
		})
	}
})
