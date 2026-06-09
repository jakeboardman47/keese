// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// keese-drain is the runtime-drain sidecar entrypoint bundled into the goose
// runtime image. It is invoked by the kubelet via the agent container's preStop
// lifecycle hook:
//
//	lifecycle:
//	  preStop:
//	    exec:
//	      command: [/usr/local/bin/keese-drain, --pvc-root=/var/run/keese/session, --timeout=25s]
//
// It calls the goose provider's Drain method and exits 0 on success or timeout.
// A non-zero exit is intentionally avoided — kubelet treats preStop exit codes
// as advisory and will still proceed with termination regardless.
//
// Signal handling: the binary installs a SIGTERM handler (rule 06-signal-handling §1)
// and cancels the drain context on receipt, allowing the underlying Drain call to
// return ErrBudget gracefully rather than being SIGKILLed mid-write.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	var pvcRoot string
	var timeoutStr string
	flag.StringVar(&pvcRoot, "pvc-root", "/var/run/keese/session",
		"Root directory of the session PVC mount")
	flag.StringVar(&timeoutStr, "timeout", "25s",
		"Maximum time to spend draining before exiting (e.g. 25s, 90s)")
	flag.Parse()

	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "keese-drain: invalid --timeout %q: %v\n", timeoutStr, err)
		os.Exit(1)
	}

	// Rule 06-signal-handling §1: install SIGTERM handler.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	wsUID := os.Getenv("KEESE_SESSION_ID")
	if wsUID == "" {
		// Fall back to an opaque marker when env is not set (e.g. during tests).
		wsUID = "unknown"
	}

	run(ctx, pvcRoot, wsUID, timeout, os.Stdout, os.Stderr)
}

// run executes one drain cycle against pvcRoot for session wsUID, bounded by the
// SIGTERM-cancellable ctx plus an absolute timeout. It writes the checkpoint
// marker, then emits the structured shutdown event (rule 06-signal-handling §4)
// to out. Drain errors are non-fatal (logged to errOut) because the kubelet
// treats preStop exit codes as advisory. Extracted from main() so tests can
// drive the real drain + shutdown-event path without spawning a process and so
// the SIGTERM-via-signal.NotifyContext path is observable end to end.
func run(ctx context.Context, pvcRoot, wsUID string, timeout time.Duration, out, errOut io.Writer) {
	// Add the absolute timeout on top of SIGTERM cancellation.
	drainCtx, drainCancel := context.WithTimeout(ctx, timeout)
	defer drainCancel()

	start := time.Now()
	if err := drain(drainCtx, pvcRoot, wsUID); err != nil {
		// Log but do not exit non-zero — kubelet ignores preStop exit codes.
		fmt.Fprintf(errOut, "keese-drain: drain error (non-fatal, pod terminating): %v\n", err)
	}

	// Rule 06-signal-handling §4: structured shutdown event.
	fmt.Fprintf(out, `{"event":"shutdown","reason":"preStop","drain_duration_ms":%d,"checkpoint_location":"%s"}`+"\n",
		time.Since(start).Milliseconds(),
		filepath.Join(pvcRoot, "sessions", wsUID, "draining"),
	)
}

// drain performs the minimal checkpoint steps that the goose provider's Drain
// method performs, without importing the full provider package. This keeps the
// binary small and avoids pulling in CGo dependencies.
//
// Steps:
//  1. Write draining-active sentinel (readiness NotReady per rule 06.9).
//  2. Write JSON checkpoint marker atomically to sessions/<wsUID>/draining.
//
// The actual SQLite WAL checkpoint is delegated to the goose process itself:
// sending SIGTERM to PID 1 (the goose process) causes it to checkpoint the WAL
// before exiting, since goose handles SIGTERM natively. The preStop hook fires
// before the main container receives SIGTERM, so this binary writes the marker
// and then allows kubelet to send SIGTERM to the container.
func drain(ctx context.Context, pvcRoot, wsUID string) error {
	sessionDir := filepath.Join(pvcRoot, "sessions", wsUID)
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		return fmt.Errorf("mkdir session dir: %w", err)
	}

	// Step 1: draining-active sentinel.
	drainingSentinel := filepath.Join(pvcRoot, "draining-active")
	if err := atomicWriteFile(drainingSentinel, []byte("draining\n")); err != nil {
		// Non-fatal: the checkpoint marker is more important.
		fmt.Fprintf(os.Stderr, "keese-drain: warning: write draining sentinel: %v\n", err)
	}

	// Check budget after sentinel write.
	select {
	case <-ctx.Done():
		return fmt.Errorf("drain budget exceeded after sentinel write")
	default:
	}

	// Step 2: JSON checkpoint marker.
	sqliteRef := filepath.Join(sessionDir, "session.sqlite")
	marker := map[string]interface{}{
		"version":       "v1",
		"workspace_uid": wsUID,
		"written_at":    time.Now().UTC().Format(time.RFC3339Nano),
		"sqlite_ref":    sqliteRef,
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	checkpointFile := filepath.Join(sessionDir, "draining")
	if err := atomicWriteFile(checkpointFile, data); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("drain budget exceeded after checkpoint write")
	default:
	}
	return nil
}

// atomicWriteFile writes data to path via a temp file + rename to prevent
// partial reads on crash.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
