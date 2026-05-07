//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	eventingv1 "knative.dev/eventing/pkg/apis/eventing/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
	// +kubebuilder:scaffold:imports
)

// workflowCRDs is the narrow CRD load list for this package. Loading the full
// config/crd/bases/ directory hits the 10 s envtest install timeout when 17
// CRDs install concurrently on macOS arm64 (root cause documented in 6ddacf6).
var workflowCRDs = []string{
	"keese.ai_workflows.yaml",
	"keese.ai_workflowruns.yaml",
}

// workflowExtraCRDDirs lists directories containing CRDs for types the
// workflow controller SSA-projects (Knative Trigger, Gateway API HTTPRoute).
// batch/v1 CronJob is built-in to envtest so no extra path is needed for it.
var workflowExtraCRDDirs = []string{
	filepath.Join("..", "..", "..", "hack", "testdata", "gateway-api"),
	filepath.Join("..", "..", "..", "hack", "testdata", "knative-eventing"),
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
		CRDDirectoryPaths:     append(crdPaths, workflowExtraCRDDirs...),
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

	err = keesev1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// batch/v1 is built-in to envtest; register the Go types so the controller
	// can use typed SSA Patch calls on CronJob.
	Expect(batchv1.AddToScheme(scheme.Scheme)).To(Succeed())

	// Register Gateway API and Knative eventing schemes so SSA Patch calls
	// for HTTPRoute and Trigger succeed against the envtest API server.
	Expect(gatewayv1.Install(scheme.Scheme)).To(Succeed())
	Expect(eventingv1.AddToScheme(scheme.Scheme)).To(Succeed())

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
func newFakes() (*FakeArgoProjector, *FakeNatsStreamProvisioner, *FakeNatsStreamDeleter, *WorkflowFakeRebacWriter, *FakeWorkflowCTAResolver) {
	return &FakeArgoProjector{
			StatusByName: map[string]*ArgoWorkflowStatus{},
		},
		&FakeNatsStreamProvisioner{},
		&FakeNatsStreamDeleter{},
		&WorkflowFakeRebacWriter{TupleCount: 2},
		&FakeWorkflowCTAResolver{}
}
