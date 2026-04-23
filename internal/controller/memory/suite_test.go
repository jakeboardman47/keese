// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

package memory

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
	"sigs.k8s.io/controller-runtime/pkg/manager"

	memoryv1alpha1 "github.com/keese-ai/keese/api/memory/v1alpha1"
)

const suiteTimeout = 120 * time.Second

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
	mgr       manager.Manager

	fakeBackend *FakeBackendProvisioner
	fakeRebac   *FakeRebacWriter
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Memory Controller Suite", Label("integration"))
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithTimeout(context.Background(), suiteTimeout)

	var err error
	err = memoryv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	if d := getFirstFoundEnvTestBinaryDir(); d != "" {
		testEnv.BinaryAssetsDirectory = d
	}

	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	// Build a manager that hosts both reconcilers.
	mgr, err = ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	fakeBackend = NewFakeBackendProvisioner()
	fakeRebac = &FakeRebacWriter{}

	memRec := &MemoryReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("memory-controller"),
		Backend:  fakeBackend,
		Rebac:    fakeRebac,
	}
	Expect(memRec.SetupWithManager(mgr)).To(Succeed())

	smRec := &SharedMemoryReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("sharedmemory-controller"),
		Backend:  fakeBackend,
		Rebac:    fakeRebac,
	}
	Expect(smRec.SetupWithManager(mgr)).To(Succeed())

	// Run the manager in the background; it is stopped by cancel() in AfterSuite.
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterSuite(func() {
	By("tearing down test environment")
	cancel()
	Expect(testEnv.Stop()).To(Succeed())
})

// Reset shared fakes before every spec to prevent cross-test state pollution.
// The manager goroutine holds interface references to these same structs, so
// we mutate in-place rather than replacing the pointers.
var _ = BeforeEach(func() {
	fakeBackend.Reset()
	fakeRebac.Reset()
})

func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
