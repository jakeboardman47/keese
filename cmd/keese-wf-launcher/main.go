// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// keese-wf-launcher is a minimal sub-binary that creates a WorkflowRun CR in
// response to an external trigger (CronJob tick, Knative Trigger, HTTPRoute POST).
//
// It is invoked as the container entrypoint inside the CronJob projected by
// reconcileCronTrigger, and as the backing Service for the Knative Trigger and
// HTTPRoute projections.
//
// Signal handling: rule 06-signal-handling §1 — SIGTERM handler installed before
// any work begins so in-flight API calls can be cancelled cleanly.
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
)

func main() {
	var workflow string
	var namespace string
	var timeout string
	flag.StringVar(&workflow, "workflow", "", "Name of the Workflow CR to launch a run for (required)")
	flag.StringVar(&namespace, "namespace", "", "Namespace of the Workflow CR (required)")
	flag.StringVar(&timeout, "timeout", "30s", "Maximum time to wait for the WorkflowRun to be accepted")
	flag.Parse()

	if workflow == "" || namespace == "" {
		fmt.Fprintln(os.Stderr, "keese-wf-launcher: --workflow and --namespace are required")
		os.Exit(1)
	}

	// Rule 06-signal-handling §1: SIGTERM handler installed before any I/O.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	dur, err := time.ParseDuration(timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "keese-wf-launcher: invalid --timeout %q: %v\n", timeout, err)
		os.Exit(1)
	}
	opCtx, opCancel := context.WithTimeout(ctx, dur)
	defer opCancel()

	start := time.Now()
	if err := createWorkflowRun(opCtx, workflow, namespace); err != nil {
		fmt.Fprintf(os.Stderr, "keese-wf-launcher: failed to create WorkflowRun: %v\n", err)
		// Rule 06-signal-handling §4: structured shutdown event even on failure.
		emitShutdownEvent("launch-failed", time.Since(start))
		os.Exit(1)
	}

	// Rule 06-signal-handling §4: structured shutdown event on success.
	emitShutdownEvent("launch-succeeded", time.Since(start))
}

// createWorkflowRun creates a WorkflowRun CR for the named Workflow using the
// in-cluster REST config (projected ServiceAccount token, rule 05-security §3).
// It uses the raw REST client to avoid pulling in controller-runtime or a full
// kubeconfig — keeping the binary small and its security footprint minimal.
func createWorkflowRun(ctx context.Context, workflow, namespace string) error {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("in-cluster config: %w", err)
	}

	scheme := runtime.NewScheme()
	if err := keesev1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("add scheme: %w", err)
	}
	codecFactory := serializer.NewCodecFactory(scheme)
	cfg.NegotiatedSerializer = codecFactory.WithoutConversion()
	cfg.GroupVersion = &keesev1alpha1.GroupVersion

	client, err := rest.RESTClientFor(cfg)
	if err != nil {
		return fmt.Errorf("build REST client: %w", err)
	}

	run := &keesev1alpha1.WorkflowRun{
		TypeMeta: metav1.TypeMeta{
			APIVersion: keesev1alpha1.GroupVersion.String(),
			Kind:       "WorkflowRun",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", workflow),
			Namespace:    namespace,
			Labels: map[string]string{
				"keese.ai/workflow":     workflow,
				"keese.ai/trigger-src":  "wf-launcher",
			},
		},
		Spec: keesev1alpha1.WorkflowRunSpec{
			WorkflowRef:  keesev1alpha1.LocalObjectReference{Name: workflow},
			WorkspaceRef: keesev1alpha1.LocalObjectReference{Name: os.Getenv("KEESE_WORKSPACE")},
			RetryBudget:  3,
		},
	}

	body, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("marshal WorkflowRun: %w", err)
	}

	result := client.Post().
		Namespace(namespace).
		Resource("workflowruns").
		Body(body).
		Do(ctx)
	if result.Error() != nil {
		return fmt.Errorf("create WorkflowRun: %w", result.Error())
	}
	return nil
}

// emitShutdownEvent writes a structured shutdown log line per rule 06-signal-handling §4.
func emitShutdownEvent(reason string, elapsed time.Duration) {
	fmt.Printf(`{"event":"shutdown","reason":%q,"drain_duration_ms":%d,"checkpoint_location":"n/a"}`+"\n",
		reason, elapsed.Milliseconds())
}
