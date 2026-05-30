// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Package wflauncher contains the WorkspaceSession polling helper used by
// the keese-wf-launcher binary. Lives under internal/ so it can be unit-
// tested without exposing it as a stable public API.
package wflauncher

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/rest"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// SessionGetter is the narrow REST interface PollSessionCompleted needs.
// rest.Interface satisfies it; tests can pass a fake.
type SessionGetter interface {
	Get() *rest.Request
}

// PollSessionCompleted polls the WorkspaceSession's status.phase on a ticker
// until it reaches a terminal state (Completed, Evicted, Terminating) or the
// context is cancelled.
//
// Returns the terminal phase observed, or an error if the context is cancelled
// before a terminal phase is reached. The caller decides what to do with the
// phase — typically Completed → exit 0, anything else → exit 1.
func PollSessionCompleted(ctx context.Context, client SessionGetter, namespace, name string, interval time.Duration) (keesev1alpha1.WorkspaceSessionPhase, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		phase, err := getPhase(ctx, client, namespace, name)
		if err == nil {
			switch phase {
			case keesev1alpha1.WorkspaceSessionPhaseCompleted,
				keesev1alpha1.WorkspaceSessionPhaseEvicted,
				keesev1alpha1.WorkspaceSessionPhaseTerminating:
				return phase, nil
			}
		}
		// Continue polling on transient errors (e.g. status not yet populated).
		select {
		case <-ctx.Done():
			if err != nil {
				return "", fmt.Errorf("polling cancelled (last error: %w)", err)
			}
			return "", fmt.Errorf("polling cancelled before terminal phase: %w", ctx.Err())
		case <-tick.C:
		}
	}
}

// getPhase fetches the WorkspaceSession and returns its status.phase.
func getPhase(ctx context.Context, client SessionGetter, namespace, name string) (keesev1alpha1.WorkspaceSessionPhase, error) {
	var sess keesev1alpha1.WorkspaceSession
	res := client.Get().
		Namespace(namespace).
		Resource("workspacesessions").
		Name(name).
		Do(ctx)
	if err := res.Error(); err != nil {
		return "", err
	}
	if err := res.Into(&sess); err != nil {
		return "", err
	}
	return sess.Status.Phase, nil
}
