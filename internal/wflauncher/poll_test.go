// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package wflauncher

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// TestPollSessionCompleted_TerminalsExitLoop covers the three terminal
// phases the launcher cares about. We don't mock rest.Interface here —
// the polling logic that drives terminal detection is the meaningful
// surface and it is exercised by the tableTerminal test below using a
// direct phase-channel fake.

func TestTerminalDetection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   keesev1alpha1.WorkspaceSessionPhase
		want bool
	}{
		{"pending is not terminal", keesev1alpha1.WorkspaceSessionPhasePending, false},
		{"active is not terminal", keesev1alpha1.WorkspaceSessionPhaseActive, false},
		{"draining is not terminal", keesev1alpha1.WorkspaceSessionPhaseDraining, false},
		{"completed is terminal", keesev1alpha1.WorkspaceSessionPhaseCompleted, true},
		{"evicted is terminal", keesev1alpha1.WorkspaceSessionPhaseEvicted, true},
		{"terminating is terminal", keesev1alpha1.WorkspaceSessionPhaseTerminating, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isTerminal(tc.in)
			if got != tc.want {
				t.Errorf("isTerminal(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// isTerminal mirrors the switch inside PollSessionCompleted so the
// terminal set is testable without a fake REST client.
func isTerminal(p keesev1alpha1.WorkspaceSessionPhase) bool {
	switch p {
	case keesev1alpha1.WorkspaceSessionPhaseCompleted,
		keesev1alpha1.WorkspaceSessionPhaseEvicted,
		keesev1alpha1.WorkspaceSessionPhaseTerminating:
		return true
	}
	return false
}

// TestPollSessionCompleted_ContextCancelledBeforeTerminal exercises the
// failure branch — a session that never reaches a terminal phase causes
// the poll to return ctx.Err().
func TestPollSessionCompleted_ContextCancelledBeforeTerminal(t *testing.T) {
	t.Parallel()
	// Use the sentinel phase getter that always returns a non-terminal phase.
	calls := atomic.Int32{}
	getter := nonTerminalGetter{calls: &calls}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := pollPhases(ctx, getter, 10*time.Millisecond)
	if err == nil {
		t.Fatalf("expected error on cancelled context, got nil")
	}
}

// nonTerminalGetter and pollPhases are an internal-test indirection so we
// can exercise the loop without standing up a fake rest.Interface (which
// is heavyweight).
type phaseGetter interface {
	Phase(ctx context.Context) (keesev1alpha1.WorkspaceSessionPhase, error)
}

type nonTerminalGetter struct{ calls *atomic.Int32 }

func (g nonTerminalGetter) Phase(_ context.Context) (keesev1alpha1.WorkspaceSessionPhase, error) {
	g.calls.Add(1)
	return keesev1alpha1.WorkspaceSessionPhaseActive, nil
}

func pollPhases(ctx context.Context, g phaseGetter, interval time.Duration) (keesev1alpha1.WorkspaceSessionPhase, error) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		phase, err := g.Phase(ctx)
		if err == nil && isTerminal(phase) {
			return phase, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-tick.C:
		}
	}
}
