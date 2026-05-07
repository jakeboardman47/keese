// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

// Package authz contains envtest-backed integration tests for the OIDCProvider
// controller.
//
// Build tag: integration — excluded from the default `go test` (unit) tier.
// Run via: make test-integration  (rule 06-testing.md §Build tags / test tiers)
//
// CRD allow-list: only the authz-group OIDCProvider CRD is loaded here to
// minimize apiserver startup latency and improve isolation across packages.
// Loading all 17 CRDs from config/crd/bases/ caused envtest CRD-install
// timeouts (default 10 s) when all packages ran concurrently.
package authz

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
	// +kubebuilder:scaffold:imports
)

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
	mgrCancel context.CancelFunc
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Authz Controller Suite")
}

// authzCRDs lists only the CRD files this package's controllers require.
// Do NOT add CRDs from other API groups here — keep envtest startup fast and
// per-package isolated. See rule 04-kubernetes.md §16 and the CRD-install
// timeout root-cause documented in this file's package comment.
var authzCRDs = []string{
	"authz.keese.ai_oidcproviders.yaml",
	// Required by GuardrailBindingReconciler tests in guardrailbinding_controller_test.go.
	"authz.keese.ai_guardrailbindings.yaml",
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	var err error
	err = authzv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	crdBasePath := filepath.Join("..", "..", "..", "config", "crd", "bases")
	crdPaths := make([]string, 0, len(authzCRDs))
	for _, f := range authzCRDs {
		crdPaths = append(crdPaths, filepath.Join(crdBasePath, f))
	}

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		// Load only this package's CRDs (not the entire config/crd/bases/ directory)
		// to prevent the 10 s CRD-install timeout when 17 CRDs install concurrently.
		CRDDirectoryPaths:     crdPaths,
		ErrorIfCRDPathMissing: true,
		CRDInstallOptions: envtest.CRDInstallOptions{
			// Belt-and-suspenders: raise the per-install timeout even with the
			// narrow CRD list above, so a slow CI runner does not time out.
			MaxTime: 120 * time.Second,
		},
	}
	if dir := getFirstFoundEnvTestBinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}

	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Start a controller manager so reconcilers run against envtest.
	// Metrics disabled to avoid port conflicts with other test packages.
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	err = (&OIDCProviderReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Recorder:     mgr.GetEventRecorderFor("oidcprovider-controller"),
		JwksFetcher:  &FakeJwksFetcher{},
		CacheFlusher: &FakeCacheFlusher{},
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	// Wire the GuardrailBindingReconciler with fakes so guardrailbinding_controller_test.go
	// can exercise it in the same envtest environment.
	fakeRebac = &FakeRebacWriter{}
	fakeKyverno = &FakeKyvernoProjector{}
	fakeEnvoy = &FakeEnvoyProjector{}
	err = (&GuardrailBindingReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("guardrailbinding-controller"),
		Rebac:    fakeRebac,
		Kyverno:  fakeKyverno,
		Envoy:    fakeEnvoy,
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	var mgrCtx context.Context
	mgrCtx, mgrCancel = context.WithCancel(ctx)
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(mgrCtx)).To(Succeed())
	}()
})

var _ = AfterSuite(func() {
	By("stopping the controller manager")
	if mgrCancel != nil {
		mgrCancel()
	}
	By("tearing down the test environment")
	if cancel != nil {
		cancel()
	}
	if testEnv != nil {
		Expect(testEnv.Stop()).To(Succeed())
	}
})

// getFirstFoundEnvTestBinaryDir locates the first binary directory under bin/k8s.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read envtest binary dir", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
