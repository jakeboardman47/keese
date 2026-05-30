// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// keese-wf-launcher is the pod-based session launcher for non-interactive
// recipe runs. It is invoked as the container entrypoint inside Workflow
// trigger projections (CronJob, Knative Trigger backend, HTTPRoute backend).
//
// On invocation it:
//  1. Creates a non-interactive WorkspaceSession CR pointing at the
//     workspace (which already carries the recipe via spec.recipeRef).
//  2. Polls status.phase on a 5-second ticker until it reaches a terminal
//     state — Completed (success), Evicted, or Terminating.
//  3. Emits a structured shutdown event and exits 0 on Completed,
//     non-zero otherwise.
//
// Rule 06-signal-handling §1: a SIGTERM handler is installed before any I/O
// so in-flight Get/Create/Watch calls can be cancelled cleanly.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
	"github.com/keese-ai/keese/internal/wflauncher"
)

func main() {
	var workspace string
	var namespace string
	var attachSubject string
	var sessionName string
	var timeout string
	var cleanup bool

	flag.StringVar(&workspace, "workspace", "",
		"Workspace CR name (required)")
	flag.StringVar(&namespace, "namespace", "",
		"Namespace of the Workspace + WorkspaceSession (required)")
	flag.StringVar(&attachSubject, "attach-subject", "service_account:keese-wf-launcher",
		"OpenFGA subject to record on the session (controller writes attached_by tuple)")
	flag.StringVar(&sessionName, "session-name", "wf-launcher",
		"WorkspaceSession.spec.sessionName")
	flag.StringVar(&timeout, "timeout", "10m",
		"Maximum wall-clock time to wait for terminal phase before giving up")
	flag.BoolVar(&cleanup, "cleanup", false,
		"Delete the WorkspaceSession CR on exit (default: keep for debugging)")
	flag.Parse()

	if workspace == "" || namespace == "" {
		fmt.Fprintln(os.Stderr, "keese-wf-launcher: --workspace and --namespace are required")
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	dur, err := time.ParseDuration(timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "keese-wf-launcher: invalid --timeout %q: %v\n", timeout, err)
		os.Exit(2)
	}
	opCtx, opCancel := context.WithTimeout(ctx, dur)
	defer opCancel()

	cfg, err := rest.InClusterConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "keese-wf-launcher: in-cluster config: %v\n", err)
		emitShutdownEvent("config-failed", 0)
		os.Exit(1)
	}
	client, err := buildSessionsClient(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "keese-wf-launcher: build REST client: %v\n", err)
		emitShutdownEvent("client-failed", 0)
		os.Exit(1)
	}

	start := time.Now()

	sess, err := createSession(opCtx, client, namespace, workspace, attachSubject, sessionName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "keese-wf-launcher: create session: %v\n", err)
		emitShutdownEvent("create-failed", time.Since(start))
		os.Exit(1)
	}
	fmt.Printf("keese-wf-launcher: created session %s/%s for workspace %s\n",
		sess.Namespace, sess.Name, workspace)

	terminalPhase, err := wflauncher.PollSessionCompleted(opCtx, client, sess.Namespace, sess.Name, 5*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "keese-wf-launcher: poll: %v\n", err)
		if cleanup {
			_ = deleteSession(context.Background(), client, sess.Namespace, sess.Name)
		}
		emitShutdownEvent("poll-failed", time.Since(start))
		os.Exit(1)
	}

	if cleanup {
		_ = deleteSession(context.Background(), client, sess.Namespace, sess.Name)
	}
	if terminalPhase == keesev1alpha1.WorkspaceSessionPhaseCompleted {
		emitShutdownEvent("launch-succeeded", time.Since(start))
		os.Exit(0)
	}
	fmt.Fprintf(os.Stderr, "keese-wf-launcher: session reached terminal phase %s (not Completed)\n", terminalPhase)
	emitShutdownEvent("launch-non-success", time.Since(start))
	os.Exit(1)
}

// buildSessionsClient returns a REST client typed for keese.ai/v1alpha1.
func buildSessionsClient(cfg *rest.Config) (rest.Interface, error) {
	scheme := runtime.NewScheme()
	if err := keesev1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("add scheme: %w", err)
	}
	codecFactory := serializer.NewCodecFactory(scheme)
	cfg.NegotiatedSerializer = codecFactory.WithoutConversion()
	cfg.GroupVersion = &keesev1alpha1.GroupVersion
	return rest.RESTClientFor(cfg)
}

func createSession(ctx context.Context, client rest.Interface, namespace, workspace, attachSubject, sessionName string) (*keesev1alpha1.WorkspaceSession, error) {
	sess := &keesev1alpha1.WorkspaceSession{
		TypeMeta: metav1.TypeMeta{
			APIVersion: keesev1alpha1.GroupVersion.String(),
			Kind:       "WorkspaceSession",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-%s-", workspace, sessionName),
			Namespace:    namespace,
			Labels: map[string]string{
				"keese.ai/workspace":   workspace,
				"keese.ai/trigger-src": "wf-launcher",
			},
		},
		Spec: keesev1alpha1.WorkspaceSessionSpec{
			WorkspaceRef:  workspace,
			AttachSubject: attachSubject,
			SessionName:   sessionName,
			Mode:          keesev1alpha1.SessionModePerAttach,
		},
	}
	body, err := json.Marshal(sess)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	var out keesev1alpha1.WorkspaceSession
	res := client.Post().
		Namespace(namespace).
		Resource("workspacesessions").
		Body(body).
		Do(ctx)
	if err := res.Error(); err != nil {
		return nil, fmt.Errorf("create: %w", err)
	}
	if err := res.Into(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &out, nil
}

func deleteSession(ctx context.Context, client rest.Interface, namespace, name string) error {
	res := client.Delete().
		Namespace(namespace).
		Resource("workspacesessions").
		Name(name).
		Do(ctx)
	return res.Error()
}

// emitShutdownEvent writes a structured shutdown log line per rule 06-signal-handling §4.
func emitShutdownEvent(reason string, elapsed time.Duration) {
	fmt.Printf(`{"event":"shutdown","reason":%q,"drain_duration_ms":%d,"checkpoint_location":"n/a"}`+"\n",
		reason, elapsed.Milliseconds())
}
