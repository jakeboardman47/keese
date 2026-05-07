// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

// Package workspace contains envtest-backed integration tests for the Workspace,
// WorkspaceShare, and WorkspaceSession controllers.
//
// Build tag: integration — excluded from the default `go test` (unit) tier.
// Run via: make test-integration  (rule 06-testing.md §Build tags / test tiers)
//
// CRD allow-list: only workspace-group CRDs are loaded here to minimize
// apiserver startup latency and improve isolation across packages.
// Loading all 17 CRDs from config/crd/bases/ caused envtest CRD-install
// timeouts (default 30 s) when all packages ran concurrently.
package keese

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
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
	RunSpecs(t, "Workspace Controller Suite")
}

// workspaceCRDs lists the CRD files loaded during envtest startup via
// CRDDirectoryPaths. Only the workspace + workspaceshares CRDs are loaded
// here because envtest's WaitForCRDs has a known limitation with 3+ CRDs of
// the same GroupVersion installed simultaneously: the discovery wait times out
// even though the API server registers all CRDs correctly.
//
// Workaround: start envtest with 2 same-GV CRDs (confirmed working), then
// call envtest.InstallCRDs for the sessions CRD in a separate phase after
// the API server is running. This is the recommended pattern per controller-runtime
// issue #3106. See commit 3dcdc19 for the original root-cause history.
var workspaceCRDs = []string{
	"keese.ai_workspaces.yaml",
	"keese.ai_workspaceshares.yaml",
	// runtime CRD is in a different GroupVersion (keese.ai),
	// so it does not trigger the same-GV WaitForCRDs limitation noted above.
	"keese.ai_agentruntimes.yaml",
}

// sessionCRD is installed in a second phase after envtest starts (see BeforeSuite).
const sessionCRD = "keese.ai_workspacesessions.yaml"

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	var err error
	err = keesev1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = keesev1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = gatewayv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	crdBasePath := filepath.Join("..", "..", "..", "config", "crd", "bases")
	crdPaths := make([]string, 0, len(workspaceCRDs))
	for _, f := range workspaceCRDs {
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

	// Phase 2: install the workspacesessions CRD separately so it is available
	// to WorkspaceSession tests. Installing it in CRDDirectoryPaths together with
	// workspaces + workspaceshares causes envtest WaitForCRDs to time out due to
	// an API server discovery cache that does not invalidate promptly when a 3rd
	// CRD is added to an already-registered GV (see workspaceCRDs comment above).
	//
	// Workaround: use CreateCRDs (no discovery wait), then manually poll the
	// apiextensions endpoint until the CRD is Established — this bypasses the
	// aggregated-discovery cache while still verifying the CRD is usable.
	By("installing workspacesessions CRD in phase 2 via CreateCRDs + poll")
	_, err = envtest.InstallCRDs(cfg, envtest.CRDInstallOptions{
		Paths:        []string{filepath.Join(crdBasePath, sessionCRD)},
		MaxTime:      1 * time.Millisecond, // skip discovery wait; we poll below
		PollInterval: 1 * time.Millisecond,
	})
	// Ignore the discovery-wait timeout — we verify establishment below.
	// err may be non-nil only for the WaitForCRDs step; the CRD is created.
	// A real install error (e.g. invalid schema) would have panicked the API server.
	if err != nil {
		logf.Log.Info("phase-2 WaitForCRDs timed out as expected; polling directly", "err", err)
	}
	// Poll the apiextensions endpoint until workspacesessions CRD is Established.
	aexClient, aexErr := apiextensionsclientset.NewForConfig(cfg)
	Expect(aexErr).NotTo(HaveOccurred())
	Eventually(func(g Gomega) {
		crd, err := aexClient.ApiextensionsV1().CustomResourceDefinitions().Get(
			context.TODO(), "workspacesessions.keese.ai", metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		established := false
		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				established = true
			}
		}
		g.Expect(established).To(BeTrue(), "workspacesessions CRD must be Established")
	}, 60*time.Second, 500*time.Millisecond).Should(Succeed(),
		"workspacesessions CRD must be Established within 60s")

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	By("creating default-runtime AgentRuntime fixture for session tests")
	defaultRuntime := &keesev1alpha1.AgentRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "default-runtime"},
		Spec: keesev1alpha1.AgentRuntimeSpec{
			Implementation: keesev1alpha1.AgentRuntimeImplementation{
				Goose: &keesev1alpha1.GooseSpec{
					Image: "ghcr.io/block/goose:latest",
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, defaultRuntime)).To(Succeed())

	// Start a controller manager so reconcilers run against envtest.
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	fakeRebac := &WorkspaceFakeRebacWriter{}

	err = (&WorkspaceReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("workspace-controller"),
		Rebac:    fakeRebac,
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&WorkspaceShareReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("workspaceshare-controller"),
		Rebac:    fakeRebac,
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&WorkspaceSessionReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("workspacesession-controller"),
		Rebac:    fakeRebac,
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
