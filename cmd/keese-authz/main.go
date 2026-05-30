// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// keese-authz is the Envoy ext_authz gRPC server. It compiles
// authz.keese.ai ToolBinding + WorkspaceTool CRs into an in-memory
// trie, then per request:
//   1. Matches the request against the trie → tool name.
//   2. Extracts the subject from the projected SA token.
//   3. Calls OpenFGA Check(user, can_call, tool:<name>).
//   4. Returns ALLOW + injected headers, or DENY with a structured
//      audit log line (no tokens, no bodies — rule 02 + spec §10).
//
// gRPC :9001 implements envoy.service.auth.v3.Authorization.
// HTTP :8081 serves /healthz for kubelet liveness/readiness probes.
//
// Runtime config (env):
//   OPENFGA_API_URL                — http(s) endpoint
//   OPENFGA_STORE_ID               — store UUID
//   OPENFGA_AUTHORIZATION_MODEL_ID — model UUID
package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/go-logr/logr"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
	"github.com/keese-ai/keese/internal/authz/extauth"
	"github.com/keese-ai/keese/internal/rebac"
)

const (
	grpcAddr   = ":9001"
	healthAddr = ":8081"
)

func main() {
	log := zap.New(zap.UseDevMode(true))
	ctrl.SetLogger(log)
	setup := log.WithName("setup")

	runScheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(runScheme))
	utilruntime.Must(authzv1alpha1.AddToScheme(runScheme))

	// OpenFGA client.
	rebacCfg := rebac.Config{
		APIURL:               os.Getenv("OPENFGA_API_URL"),
		StoreID:              os.Getenv("OPENFGA_STORE_ID"),
		AuthorizationModelID: os.Getenv("OPENFGA_AUTHORIZATION_MODEL_ID"),
	}
	if err := rebacCfg.Validate(); err != nil {
		setup.Error(err, "OpenFGA config invalid")
		os.Exit(1)
	}
	rebacClient, err := rebac.New(rebacCfg)
	if err != nil {
		setup.Error(err, "OpenFGA client construction failed")
		os.Exit(1)
	}
	setup.Info("OpenFGA client ready",
		"api_url", rebacCfg.APIURL, "store_id", rebacCfg.StoreID)

	resolver := extauth.NewResolver()

	// Controller-runtime manager — read-only on ToolBinding +
	// WorkspaceTool. We don't run reconcilers here; we use a
	// background goroutine that periodically lists both kinds and
	// recompiles the trie. (A future revision can swap this for
	// proper informer-driven event handlers.)
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 runScheme,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		LeaderElection:         false,
	})
	if err != nil {
		setup.Error(err, "controller-runtime manager")
		os.Exit(1)
	}
	if err := mgr.Add(&trieRefresher{
		client:   mgr.GetClient(),
		resolver: resolver,
		log:      log.WithName("trie-refresher"),
		interval: 10 * time.Second,
	}); err != nil {
		setup.Error(err, "register trie refresher")
		os.Exit(1)
	}

	// gRPC server.
	authServer := &authzServer{
		resolver: resolver,
		fga:      rebacClient,
		log:      log.WithName("authz"),
	}
	gs := grpc.NewServer()
	authv3.RegisterAuthorizationServer(gs, authServer)

	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		setup.Error(err, "listen", "addr", grpcAddr)
		os.Exit(1)
	}

	// Health server (separate goroutine).
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	healthSrv := &http.Server{Addr: healthAddr, Handler: healthMux, ReadHeaderTimeout: 5 * time.Second}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	go func() {
		setup.Info("gRPC ext_authz listening", "addr", grpcAddr)
		if err := gs.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			setup.Error(err, "grpc server")
		}
	}()
	go func() {
		setup.Info("health server listening", "addr", healthAddr)
		if err := healthSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			setup.Error(err, "health server")
		}
	}()
	go func() {
		setup.Info("controller-runtime manager starting")
		if err := mgr.Start(ctx); err != nil {
			setup.Error(err, "manager")
		}
	}()

	<-ctx.Done()
	setup.Info("shutdown initiated")
	gs.GracefulStop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = healthSrv.Shutdown(shutdownCtx)
	setup.Info("shutdown complete")
}

// authzServer implements envoy.service.auth.v3.AuthorizationServer.
type authzServer struct {
	authv3.UnimplementedAuthorizationServer
	resolver *extauth.Resolver
	fga      extauth.FGAChecker
	log      logr.Logger
}

// Check is the per-request hot path.
func (s *authzServer) Check(ctx context.Context, req *authv3.CheckRequest) (*authv3.CheckResponse, error) {
	start := time.Now()
	httpReq := envoyRequestToHTTPRequest(req)
	requestID := httpReq.Headers["x-request-id"]

	d := extauth.Authorize(ctx, httpReq, s.resolver, s.fga)
	dur := time.Since(start)
	extauth.LogAudit(s.log, extauth.AuditFromDecision(d, httpReq, requestID, dur))

	if d.Allowed {
		return &authv3.CheckResponse{
			Status: &status.Status{Code: int32(codes.OK)},
			HttpResponse: &authv3.CheckResponse_OkResponse{
				OkResponse: &authv3.OkHttpResponse{
					Headers: []*corev3.HeaderValueOption{
						{Header: &corev3.HeaderValue{Key: "x-keese-tool", Value: d.FinalToolName}},
						{Header: &corev3.HeaderValue{
							Key:   "x-keese-workspace",
							Value: d.Workspace.Namespace + "/" + d.Workspace.Name,
						}},
					},
				},
			},
		}, nil
	}
	return &authv3.CheckResponse{
		Status: &status.Status{Code: int32(codes.PermissionDenied)},
		HttpResponse: &authv3.CheckResponse_DeniedResponse{
			DeniedResponse: &authv3.DeniedHttpResponse{
				Status: &typev3.HttpStatus{Code: typev3.StatusCode_Forbidden},
				Body:   "permission_denied",
			},
		},
	}, nil
}

// envoyRequestToHTTPRequest unpacks the Envoy CheckRequest into the
// extauth-internal HTTPRequest. Headers are lowercased for
// case-insensitive lookup.
func envoyRequestToHTTPRequest(req *authv3.CheckRequest) *extauth.HTTPRequest {
	out := &extauth.HTTPRequest{
		Headers:     map[string]string{},
		QueryParams: map[string]string{},
	}
	if req == nil || req.Attributes == nil || req.Attributes.Request == nil ||
		req.Attributes.Request.Http == nil {
		return out
	}
	h := req.Attributes.Request.Http
	out.Path = stripQuery(h.Path)
	out.Method = h.Method
	for k, v := range h.Headers {
		out.Headers[strings.ToLower(k)] = v
	}
	out.Body = []byte(h.Body)
	if i := strings.Index(h.Path, "?"); i >= 0 {
		query := h.Path[i+1:]
		for _, kv := range strings.Split(query, "&") {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) == 2 {
				out.QueryParams[parts[0]] = parts[1]
			}
		}
	}
	return out
}

func stripQuery(p string) string {
	if i := strings.Index(p, "?"); i >= 0 {
		return p[:i]
	}
	return p
}

// trieRefresher periodically lists ToolBindings + WorkspaceTools and
// recompiles the resolver's trie. controller-runtime informers
// would be more efficient; for the demo this is good enough.
type trieRefresher struct {
	client   ctrlclient.Client
	resolver *extauth.Resolver
	log      logr.Logger
	interval time.Duration
}

// Start satisfies manager.Runnable.
func (t *trieRefresher) Start(ctx context.Context) error {
	t.log.Info("trie refresher running", "interval", t.interval)
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	t.refresh(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			t.refresh(ctx)
		}
	}
}

func (t *trieRefresher) refresh(ctx context.Context) {
	tbs := &authzv1alpha1.ToolBindingList{}
	wts := &authzv1alpha1.WorkspaceToolList{}
	if err := t.client.List(ctx, tbs); err != nil {
		t.log.Error(err, "list ToolBindings")
		return
	}
	if err := t.client.List(ctx, wts); err != nil {
		t.log.Error(err, "list WorkspaceTools")
		return
	}
	rejected := t.resolver.ApplySnapshot(tbs.Items, wts.Items)
	for _, r := range rejected {
		t.log.Info("trie binding rejected", "reason", r)
	}
	t.log.V(1).Info("trie refreshed",
		"toolBindings", len(tbs.Items),
		"workspaceTools", len(wts.Items),
		"rejected", len(rejected))
}
