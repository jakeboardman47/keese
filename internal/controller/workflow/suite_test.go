//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package workflow

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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	workflowv1alpha1 "github.com/keese-ai/keese/api/workflow/v1alpha1"
	// +kubebuilder:scaffold:imports
)

// workflowCRDs is the narrow CRD load list for this package. Loading the full
// config/crd/bases/ directory hits the 10 s envtest install timeout when 17
// CRDs install concurrently on macOS arm64 (root cause documented in 6ddacf6).
var workflowCRDs = []string{
	"workflow.operator.keese.ai_workflows.yaml",
	"workflow.operator.keese.ai_workflowruns.yaml",
}

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Workflow Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.Background())

	crdBasePath := filepath.Join("..", "..", "..", "config", "crd", "bases")
	crdPaths := make([]string, 0, len(workflowCRDs))
	for _, f := range workflowCRDs {
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

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = workflowv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	Expect(testEnv.Stop()).To(Succeed())
})

// getFirstFoundEnvTestBinaryDir locates envtest binaries.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "failed to read envtest binary dir", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

// newFakes creates fresh fake dependencies for each test.
func newFakes() (*FakeArgoProjector, *FakeNatsStreamProvisioner, *FakeNatsStreamDeleter, *FakeRebacWriter, *FakeCTAResolver) {
	return &FakeArgoProjector{
			StatusByName: map[string]*ArgoWorkflowStatus{},
		},
		&FakeNatsStreamProvisioner{},
		&FakeNatsStreamDeleter{},
		&FakeRebacWriter{TupleCount: 2},
		&FakeCTAResolver{}
}
