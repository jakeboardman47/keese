// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// keese-cosign-webhook is the pre-install ValidatingWebhook server
// that fails-closed on OLM InstallPlans whose target CSVs reference
// unsigned keese-published images.
//
// Anchored from rule 05.12 ("Bundle image + operator image carry
// Sigstore cosign keyless OIDC signatures") and design 14a §4
// ("A pre-install ValidatingWebhook rejects InstallPlan approval
// if the bundle image digest is unsigned").
//
// Endpoints:
//
//	POST /validate-installplan  — admission webhook
//	GET  /healthz, /readyz       — kubelet probes
//	GET  /metrics                — controller-runtime metrics
//
// Runtime config (env + flags — env wins where overlapping):
//
//	WEBHOOK_PORT          (default 9443)
//	HEALTH_PORT           (default 8081)
//	METRICS_PORT          (default 8082)
//	WEBHOOK_CERT_DIR      (default /etc/webhook/certs)
//	COSIGN_BINARY         (default cosign — must be on PATH or absolute)
//	COSIGN_IDENTITY_REGEX (override identity regexp)
//	COSIGN_OIDC_ISSUER    (override OIDC issuer)
//	COSIGN_REGISTRY_ALLOW (CSV list of registry prefixes; default ghcr.io/keese-ai/)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	cosignadm "github.com/keese-ai/keese/internal/admission/cosign"
	"github.com/keese-ai/keese/internal/featuregate"
)

const (
	defaultWebhookPort = 9443
	defaultHealthPort  = 8081
	defaultMetricsPort = 8082
	defaultCertDir     = "/etc/webhook/certs"

	shutdownGrace = 30 * time.Second // matches the Deployment terminationGracePeriodSeconds
)

func main() {
	logger := zap.New(zap.UseDevMode(false))
	ctrl.SetLogger(logger)
	setup := logger.WithName("setup")

	var (
		webhookPort = envIntFlag("WEBHOOK_PORT", "webhook-port",
			defaultWebhookPort, "TLS port for the admission webhook server")
		healthPort = envIntFlag("HEALTH_PORT", "health-port",
			defaultHealthPort, "HTTP port for /healthz + /readyz")
		metricsPort = envIntFlag("METRICS_PORT", "metrics-port",
			defaultMetricsPort, "HTTP port for /metrics")
		certDir = envStringFlag("WEBHOOK_CERT_DIR", "webhook-cert-dir",
			defaultCertDir, "Directory containing tls.crt + tls.key")
		cosignBinary = envStringFlag("COSIGN_BINARY", "cosign-binary",
			"cosign", "Path to the cosign executable")
		identityRegex = envStringFlag("COSIGN_IDENTITY_REGEX", "cosign-identity-regex",
			"", "Override --certificate-identity-regexp (default: keese-ai workflows)")
		oidcIssuer = envStringFlag("COSIGN_OIDC_ISSUER", "cosign-oidc-issuer",
			"", "Override --certificate-oidc-issuer (default: GitHub Actions OIDC)")
		registryAllow = envStringFlag("COSIGN_REGISTRY_ALLOW", "cosign-registry-allow",
			"", "Comma-separated registry prefixes to gate (default: ghcr.io/keese-ai/)")
		featureGatesPath = envStringFlag("KEESE_FEATURE_GATES_PATH", "feature-gates-path",
			"/etc/keese/features/gates.json",
			"Path to the keese-features ConfigMap projection (D27)")
	)
	flag.Parse()

	cfg := cosignadm.VerifierConfig{
		CosignBinary:              *cosignBinary,
		CertificateIdentityRegexp: *identityRegex,
		CertificateOIDCIssuer:     *oidcIssuer,
	}
	if *registryAllow != "" {
		for _, p := range strings.Split(*registryAllow, ",") {
			if s := strings.TrimSpace(p); s != "" {
				cfg.AllowedRegistryPrefixes = append(cfg.AllowedRegistryPrefixes, s)
			}
		}
	}
	verifier, err := cosignadm.NewVerifier(cfg)
	if err != nil {
		setup.Error(err, "build verifier")
		os.Exit(1)
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	// Register OLM unstructured kinds so the controller-runtime client
	// can Get them via the dynamic typing path.
	scheme.AddKnownTypeWithName(cosignadm.InstallPlanGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(cosignadm.CSVGVK, &unstructured.Unstructured{})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: fmt.Sprintf(":%d", *metricsPort),
		},
		HealthProbeBindAddress: fmt.Sprintf(":%d", *healthPort),
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    *webhookPort,
			CertDir: *certDir,
		}),
		LeaderElection: false,
	})
	if err != nil {
		setup.Error(err, "build manager")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setup.Error(err, "add healthz")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setup.Error(err, "add readyz")
		os.Exit(1)
	}

	gates, err := featuregate.New(context.Background(), featuregate.Options{
		Path:   *featureGatesPath,
		Binary: "keese-cosign-webhook",
		Log:    logger.WithName("featuregate"),
		Defaults: map[featuregate.Gate]bool{
			// Stage alpha → off. Production OLM bundle ships a seed CR
			// with `override: true` for cosign-installplan-verify.
			featuregate.CosignInstallPlanVerify:     false,
			featuregate.CosignInstallPlanFailClosed: false,
		},
	})
	if err != nil {
		setup.Error(err, "build featuregate eval")
		os.Exit(1)
	}

	handler := &cosignadm.Handler{
		Client:   mgr.GetClient(),
		Verifier: verifier,
		Gates:    gates,
		Decoder:  admission.NewDecoder(scheme),
		Log:      logger.WithName("cosign-webhook"),
	}
	mgr.GetWebhookServer().Register("/validate-installplan", &webhook.Admission{Handler: handler})

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	setup.Info("keese-cosign-webhook starting",
		"webhook_port", *webhookPort,
		"health_port", *healthPort,
		"metrics_port", *metricsPort,
		"cert_dir", *certDir,
		"cosign_binary", *cosignBinary,
	)

	go func() {
		if err := mgr.Start(ctx); err != nil {
			setup.Error(err, "manager exited")
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	setup.Info("shutdown signal received — draining",
		"reason", "SIGTERM",
		"grace_seconds", shutdownGrace.Seconds())

	// controller-runtime drains via the parent context; give it a hard
	// upper bound so we don't outlive terminationGracePeriodSeconds.
	drainCtx, drainCancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer drainCancel()
	<-drainCtx.Done()
	setup.Info("shutdown complete",
		"event", "shutdown",
		"reason", "drain_complete")
}

// envStringFlag binds a flag and overrides its default with the env
// var when set. Returns the pointer the flag wrote into.
func envStringFlag(env, name, def, usage string) *string {
	if v, ok := os.LookupEnv(env); ok {
		def = v
	}
	return flag.String(name, def, usage)
}

func envIntFlag(env, name string, def int, usage string) *int {
	if v, ok := os.LookupEnv(env); ok {
		if n, err := strconv.Atoi(v); err == nil {
			def = n
		}
	}
	return flag.Int(name, def, usage)
}
