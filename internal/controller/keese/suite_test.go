// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

//go:build integration

// Package keese hosts the envtest-backed integration suite for every keese-group
// reconciler (Workspace, WorkspaceShare, WorkspaceSession, Memory, SharedMemory,
// Recipe, RecipeSource, AgentRuntime, RuntimeExtension, Tenant,
// CrossTenantAgreement, Transport, Workflow, WorkflowRun).
//
// History: commit ce2436e merged seven formerly-separate controller packages
// into a single `package keese`. Each merged package shipped its own
// `*_suite_test.go` that redeclared `TestControllers`, the shared
// `ctx`/`cancel`/`testEnv`/`cfg`/`k8sClient` envtest plumbing, and
// `getFirstFoundEnvTestBinaryDir`. Seven copies of the same package-level
// symbols is a duplicate-symbol compile error, so the `keese` test binary did
// not build and the integration CI job was red on a build failure (CH9).
//
// This file is the ONE shared envtest harness for the package: the lone
// `TestControllers`/`RunSpecs`, the shared envtest vars, the env-bin helper, and
// a single `BeforeSuite` that installs every keese CRD and starts one controller
// manager.
//
// Reconciler registration. The manager-driven controllers (Workspace,
// WorkspaceShare, WorkspaceSession, Memory, SharedMemory, Recipe, RecipeSource,
// Tenant, CrossTenantAgreement) are wired into the manager here, exactly as their
// own `SetupWithManager` does, so their `Eventually`-style specs observe live
// reconciliation. The remaining controllers (AgentRuntime, RuntimeExtension,
// Transport, Workflow, WorkflowRun) are exercised by specs that construct a
// reconciler with per-spec fakes and call `Reconcile` synchronously, asserting
// on those fakes' call counts (e.g. the CH6 `RunCount derivation` spec drives the
// WorkflowRun owner-watch logic by hand via an Eventually+Reconcile loop). Wiring
// a background manager copy of those reconcilers — each with a different fake set
// — would race the manual calls on finalizer/status writes and corrupt the
// per-spec fake assertions, so those kinds are deliberately not manager-registered
// here. The CRDs and schemes they need are still installed below.
//
// Build tag: integration — excluded from the default `go test` (unit) tier.
// Run via: make test-integration  (rule 06-testing.md §Build tags / test tiers).
package keese

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	eventingv1 "knative.dev/eventing/pkg/apis/eventing/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
	policyv1alpha1 "github.com/keese-ai/keese/api/policy/v1alpha1"
	// +kubebuilder:scaffold:imports
)

// suiteTimeout bounds the whole suite. Mirrors the per-package budgets the merged
// suites used (120s).
const suiteTimeout = 120 * time.Second

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
	mgr       manager.Manager
	mgrCancel context.CancelFunc

	// Shared memory fakes. The memory + sharedmemory specs assert on these and a
	// BeforeEach resets them between specs (see below).
	fakeBackend *FakeBackendProvisioner
	fakeRebac   *MemoryFakeRebacWriter
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "keese Controller Suite", Label("integration"))
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithTimeout(context.Background(), suiteTimeout)

	// Register every scheme any reconciler or spec touches.
	Expect(keesev1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(authzv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	// policy.keese.ai — TokenBudget / FeatureGate. The WorkspaceSession
	// reconciler lists TokenBudgets in its budget gate, so the type must be in
	// the scheme even when no TokenBudget object exists.
	Expect(policyv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	// Capsule Tenant — TenantReconciler Mode B resolves a Capsule Tenant.
	Expect(capsulev1beta2.AddToScheme(scheme.Scheme)).To(Succeed())
	// batch/v1 CronJob, Gateway API, Knative eventing — projected via SSA by the
	// Workspace / Workflow controllers.
	Expect(batchv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(gatewayv1.Install(scheme.Scheme)).To(Succeed())
	Expect(gatewayv1beta1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(eventingv1.AddToScheme(scheme.Scheme)).To(Succeed())
	// +kubebuilder:scaffold:scheme

	crdBasePath := filepath.Join("..", "..", "..", "config", "crd", "bases")

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		// Load the full CRD base directory plus the vendored extra CRDs (Capsule
		// Tenant for tenancy Mode B; Gateway API + Knative for workflow trigger
		// projection). The per-package narrow CRD lists in the old suites existed
		// only to dodge a same-GroupVersion install timeout that triggered when
		// many test packages installed CRDs concurrently. With a single suite
		// there is no concurrency, so the full-directory load (already used by the
		// former memory suite) is safe.
		CRDDirectoryPaths: []string{
			crdBasePath,
			filepath.Join("..", "..", "..", "hack", "testdata", "capsule"),
			filepath.Join("..", "..", "..", "hack", "testdata", "gateway-api"),
			filepath.Join("..", "..", "..", "hack", "testdata", "knative-eventing"),
		},
		ErrorIfCRDPathMissing: true,
		CRDInstallOptions: envtest.CRDInstallOptions{
			MaxTime: suiteTimeout,
		},
	}
	if dir := getFirstFoundEnvTestBinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

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

	By("starting the controller manager")
	mgr, err = ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	registerManagerReconcilers(mgr)

	var mgrCtx context.Context
	mgrCtx, mgrCancel = context.WithCancel(ctx)
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(mgrCtx)).To(Succeed())
	}()
})

// registerManagerReconcilers wires the manager-driven reconcilers into mgr,
// each exactly as its own SetupWithManager does. See the package doc for why the
// manual-reconcile controllers (AgentRuntime, RuntimeExtension, Transport,
// Workflow, WorkflowRun) are not registered here.
func registerManagerReconcilers(mgr manager.Manager) {
	// Only the Memory and SharedMemory specs are *manager-driven*: they create a
	// Memory / SharedMemory object and then `Eventually` await the background
	// manager to drive Status.Phase to Ready (they never construct and call their
	// own reconciler in the assertion loop). Their reconcilers must therefore run
	// in this manager, wired to the package-level fakes the specs assert on.
	//
	// Every other keese controller's specs are *manual-reconcile*: they build a
	// reconciler with per-spec fakes and call Reconcile synchronously (workspace,
	// workspacesession, recipe, recipesource, tenant, transport, agentruntime,
	// runtimeextension, workflow, workflowrun — and the cleanup/idempotency specs
	// of workspaceshare). Registering a background copy of those reconcilers would
	// race the manual calls on finalizer/status writes and, because the background
	// copy holds a *different* fake, would silently steal the reconcile the spec
	// expects its own fake to observe — corrupting the per-spec assertions. Those
	// kinds are deliberately not registered here; their CRDs + schemes are still
	// installed in BeforeSuite.
	fakeBackend = NewFakeBackendProvisioner()
	fakeRebac = &MemoryFakeRebacWriter{}
	Expect((&MemoryReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("memory-controller"),
		Backend:  fakeBackend,
		Rebac:    fakeRebac,
	}).SetupWithManager(mgr)).To(Succeed())

	Expect((&SharedMemoryReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("sharedmemory-controller"),
		Backend:  fakeBackend,
		Rebac:    fakeRebac,
	}).SetupWithManager(mgr)).To(Succeed())
}

// NOTE on memory fakes: we deliberately do NOT reset fakeBackend / fakeRebac in
// a BeforeEach. The memory + sharedmemory specs assert against them using
// reset-independent baselines — a delta (deprovCountBefore -> len(...) >
// deprovCountBefore) and NotTo(BeEmpty()) — so accumulation across specs is
// harmless. A global BeforeEach reset, on the other hand, would race the
// background memory manager goroutine that reads these same fakes while a prior
// memory object is still reconciling (the structs are deliberately not
// concurrency-safe — see FakeBackendProvisioner in memory_backend.go). Dropping
// the reset removes that race without touching production code.

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

// getFirstFoundEnvTestBinaryDir locates the first binary directory under bin/k8s
// so envtest works from an IDE without KUBEBUILDER_ASSETS set. Run
// `make setup-envtest` first.
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

// ensureDevNamespace creates (or fetches) a namespace with the keese.ai/env=dev
// label. Used by recipe / recipesource specs.
func ensureDevNamespace(nsName string) {
	ensureNamespace(nsName, map[string]string{
		"keese.ai/env":     "dev",
		"keese.ai/managed": "true",
	})
}

// ensureProdNamespace creates (or fetches) a namespace WITHOUT the dev label.
func ensureProdNamespace(nsName string) {
	ensureNamespace(nsName, map[string]string{
		"keese.ai/managed": "true",
	})
}

// ensureNamespace creates the named namespace with the given labels if it does
// not already exist.
func ensureNamespace(nsName string, labels map[string]string) {
	ns := &corev1.Namespace{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: nsName}, ns); err == nil {
		return
	}
	Expect(k8sClient.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: nsName, Labels: labels},
	})).To(Succeed())
}
