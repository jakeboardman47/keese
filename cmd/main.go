// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"flag"
	"os"
	"path/filepath"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	argov1alpha1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	envoyaigatewayv1alpha1 "github.com/envoyproxy/ai-gateway/api/v1alpha1"
	envoygatewayv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	kyvernov2 "github.com/kyverno/kyverno/api/kyverno/v2"
	eventingv1 "knative.dev/eventing/pkg/apis/eventing/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
	policyv1alpha1 "github.com/keese-ai/keese/api/policy/v1alpha1"
	authzcontroller "github.com/keese-ai/keese/internal/controller/authz"
	keesecontroller "github.com/keese-ai/keese/internal/controller/keese"
	policycontroller "github.com/keese-ai/keese/internal/controller/policy"
	"github.com/keese-ai/keese/internal/rebac"

	// AgentRuntime SPI providers — blank import drives Register() in
	// each provider's init(). Spec §Static registration.
	_ "github.com/keese-ai/keese/internal/runtime/providers/goose"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(keesev1alpha1.AddToScheme(scheme))
	utilruntime.Must(authzv1alpha1.AddToScheme(scheme))
	utilruntime.Must(policyv1alpha1.AddToScheme(scheme))

	// External operator API schemes.
	utilruntime.Must(argov1alpha1.AddToScheme(scheme))
	// capsulev1beta2: required for Mode B Tenant namespace aggregation (TD-P2-06).
	utilruntime.Must(capsulev1beta2.AddToScheme(scheme))
	utilruntime.Must(eventingv1.AddToScheme(scheme))
	utilruntime.Must(gatewayv1.AddToScheme(scheme))
	utilruntime.Must(gatewayv1beta1.AddToScheme(scheme))
	// kyvernov1 hosts ClusterPolicy; kyvernov2 hosts PolicyException + CleanupPolicy.
	utilruntime.Must(kyvernov1.AddToScheme(scheme))
	utilruntime.Must(kyvernov2.AddToScheme(scheme))
	utilruntime.Must(envoygatewayv1alpha1.AddToScheme(scheme))
	utilruntime.Must(envoyaigatewayv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for metrics and webhooks certificates
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		var err error
		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(webhookCertPath, webhookCertName),
			filepath.Join(webhookCertPath, webhookCertKey),
		)
		if err != nil {
			setupLog.Error(err, "Failed to initialize webhook certificate watcher")
			os.Exit(1)
		}

		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: webhookTLSOpts,
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(metricsCertPath, metricsCertName),
			filepath.Join(metricsCertPath, metricsCertKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
			os.Exit(1)
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "ae90101e.keese.ai",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Build the OpenFGA-backed ReBAC writers when OPENFGA_API_URL +
	// OPENFGA_STORE_ID + OPENFGA_AUTHORIZATION_MODEL_ID are all set;
	// otherwise leave each adapter nil so the nil-guard in every
	// reconciler's SetupWithManager falls back to the per-package
	// FakeRebacWriter (envtest + out-of-cluster local-run path).
	rebacCfg := rebac.Config{
		APIURL:               os.Getenv("OPENFGA_API_URL"),
		StoreID:              os.Getenv("OPENFGA_STORE_ID"),
		AuthorizationModelID: os.Getenv("OPENFGA_AUTHORIZATION_MODEL_ID"),
	}
	// Interface-typed nils (NOT typed-nil pointers — those would defeat
	// each reconciler's `if r.Rebac == nil` fallback guard).
	var (
		transportRebac keesecontroller.TransportRebacWriter
		tenantRebac    keesecontroller.TenantRebacWriter
		ctaRebac       authzcontroller.CTARebacWriter
		workspaceRebac keesecontroller.WorkspaceRebacWriter
		recipeRebac    keesecontroller.RecipeRebacWriter
		guardrailRebac authzcontroller.GuardrailRebacWriter
		memoryRebac    keesecontroller.MemoryRebacWriter
		runtimeRebac   keesecontroller.RuntimeRebacWriter
		workflowRebac  keesecontroller.WorkflowRebacWriter
	)
	if rebacCfg.APIURL != "" {
		client, rebacErr := rebac.New(rebacCfg)
		if rebacErr != nil {
			setupLog.Error(rebacErr, "unable to construct OpenFGA client; falling back to fake writers")
		} else {
			setupLog.Info("OpenFGA-backed ReBAC writers enabled",
				"api_url", rebacCfg.APIURL, "store_id", rebacCfg.StoreID)
			transportRebac = &keesecontroller.TransportOpenFGARebacWriter{Client: client}
			tenantRebac = &keesecontroller.TenantOpenFGARebacWriter{Client: client}
			ctaRebac = &authzcontroller.CTAOpenFGARebacWriter{Client: client}
			workspaceRebac = &keesecontroller.WorkspaceOpenFGARebacWriter{Client: client}
			recipeRebac = &keesecontroller.RecipeOpenFGARebacWriter{Client: client}
			guardrailRebac = &authzcontroller.GuardrailOpenFGARebacWriter{Client: client}
			memoryRebac = &keesecontroller.MemoryOpenFGARebacWriter{Client: client}
			runtimeRebac = &keesecontroller.RuntimeOpenFGARebacWriter{Client: client}
			workflowRebac = &keesecontroller.WorkflowOpenFGARebacWriter{Client: client}
		}
	} else {
		setupLog.Info("OPENFGA_API_URL unset — using FakeRebacWriter (no upstream authz)")
	}

	if err := (&keesecontroller.WorkspaceReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Rebac:  workspaceRebac,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Workspace")
		os.Exit(1)
	}
	if err := (&keesecontroller.WorkspaceShareReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Rebac:  workspaceRebac,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WorkspaceShare")
		os.Exit(1)
	}
	if err := (&keesecontroller.WorkspaceSessionReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Rebac:  workspaceRebac,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WorkspaceSession")
		os.Exit(1)
	}
	argoProjector := keesecontroller.NewClientArgoProjector(mgr.GetClient())
	// All Workflow/WorkflowRun reconciler dependencies are wired with
	// safe defaults (Fake* / NoOp*) until the production implementations
	// land. Each is tracked as a TD-P1 entry.
	if workflowRebac == nil {
		workflowRebac = keesecontroller.WorkflowNoopRebacWriter{}
	}
	wfNats := &keesecontroller.FakeNatsStreamProvisioner{}
	wfNatsDel := &keesecontroller.FakeNatsStreamDeleter{}
	if err := (&keesecontroller.WorkflowReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		Argo:          argoProjector,
		Rebac:         workflowRebac,
		EventRecorder: mgr.GetEventRecorderFor("keese-workflow-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Workflow")
		os.Exit(1)
	}
	if err := (&keesecontroller.WorkflowRunReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		Argo:        argoProjector,
		Nats:        wfNats,
		NatsDeleter: wfNatsDel,
		Rebac:       workflowRebac,
		// Single-tenant default until the CrossTenantAgreement (CTA)
		// reconciler is wired. NoOpCTAResolver returns no peers.
		CTA:           &keesecontroller.FakeWorkflowCTAResolver{},
		EventRecorder: mgr.GetEventRecorderFor("keese-workflowrun-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WorkflowRun")
		os.Exit(1)
	}
	if err := (&keesecontroller.AgentRuntimeReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AgentRuntime")
		os.Exit(1)
	}
	if err := (&keesecontroller.RuntimeExtensionReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Rebac:  runtimeRebac,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RuntimeExtension")
		os.Exit(1)
	}
	if err := (&keesecontroller.MemoryReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Rebac:  memoryRebac,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Memory")
		os.Exit(1)
	}
	if err := (&keesecontroller.SharedMemoryReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Rebac:  memoryRebac,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SharedMemory")
		os.Exit(1)
	}
	if err := (&keesecontroller.RecipeReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Rebac:  recipeRebac,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Recipe")
		os.Exit(1)
	}
	if err := keesecontroller.SetupRecipeWebhookWithManager(mgr, nil); err != nil {
		setupLog.Error(err, "unable to register webhook", "webhook", "Recipe")
		os.Exit(1)
	}
	if err := (&keesecontroller.RecipeSourceReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RecipeSource")
		os.Exit(1)
	}
	if err := (&authzcontroller.GuardrailBindingReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("guardrailbinding-controller"),
		Kyverno:  authzcontroller.NewClientKyvernoPolicyProjector(mgr.GetClient()),
		Rebac:    guardrailRebac,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GuardrailBinding")
		os.Exit(1)
	}
	if err := (&policycontroller.TokenBudgetReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		RateLimitProj: policycontroller.NewClientRateLimitProjector(mgr.GetClient()),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TokenBudget")
		os.Exit(1)
	}
	if err := (&policycontroller.FeatureGateReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "FeatureGate")
		os.Exit(1)
	}
	if err := (&keesecontroller.TransportReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Rebac:  transportRebac,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Transport")
		os.Exit(1)
	}
	if err := (&keesecontroller.TenantReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Rebac:  tenantRebac,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Tenant")
		os.Exit(1)
	}
	if err := (&authzcontroller.CrossTenantAgreementReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Rebac:  ctaRebac,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CrossTenantAgreement")
		os.Exit(1)
	}
	if err := (&authzcontroller.OIDCProviderReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "OIDCProvider")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if metricsCertWatcher != nil {
		setupLog.Info("Adding metrics certificate watcher to manager")
		if err := mgr.Add(metricsCertWatcher); err != nil {
			setupLog.Error(err, "unable to add metrics certificate watcher to manager")
			os.Exit(1)
		}
	}

	if webhookCertWatcher != nil {
		setupLog.Info("Adding webhook certificate watcher to manager")
		if err := mgr.Add(webhookCertWatcher); err != nil {
			setupLog.Error(err, "unable to add webhook certificate watcher to manager")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
