// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package podexec_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	remotecommandconsts "k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/rest"
	utilexec "k8s.io/client-go/util/exec"

	"github.com/keese-ai/keese/internal/runtime/podexec"
)

// --- Fake exec server -------------------------------------------------------
//
// The Kubernetes exec subresource speaks the SPDY streaming protocol v4.
// remotecommand.NewSPDYExecutor (used inside podexec.Exec) connects to it,
// opens stdin/stdout/stderr/error streams, copies bytes, and reads a final
// metav1.Status off the error stream to surface the command's exit code.
//
// To unit-test podexec.Exec without a real apiserver we stand up an httptest
// server that performs the server half of that protocol. The handler logic is
// modeled on client-go's own remotecommand spdy_test.go fake. We own this fake
// (rule 06 — no mocking a type we don't own without an adapter).

// execScript is what the fake server "runs": it writes fixed bytes to stdout /
// stderr and then reports a status (success or a non-zero exit code).
type execScript struct {
	stdout   string
	stderr   string
	exitCode int // 0 => StatusSuccess; non-zero => NonZeroExitCode status
}

type serverStreams struct {
	conn        io.Closer
	stdoutW     io.WriteCloser
	stderrW     io.WriteCloser
	writeStatus func(*apierrors.StatusError) error
}

// newFakeExecServer returns an httptest.Server that upgrades the request to
// SPDY v4 and runs the given script. If script is nil the handler refuses the
// upgrade (returns 400), which drives the SPDY-setup failure path in Exec.
func newFakeExecServer(t *testing.T, script *execScript) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if script == nil {
			http.Error(w, "exec refused", http.StatusBadRequest)
			return
		}
		streams, err := upgradeToV4(w, req)
		if err != nil {
			// Handshake/upgrade failed; nothing more to do.
			return
		}
		defer streams.conn.Close()

		if script.stdout != "" && streams.stdoutW != nil {
			_, _ = io.WriteString(streams.stdoutW, script.stdout)
		}
		if script.stderr != "" && streams.stderrW != nil {
			_, _ = io.WriteString(streams.stderrW, script.stderr)
		}

		if script.exitCode == 0 {
			_ = streams.writeStatus(&apierrors.StatusError{ErrStatus: metav1.Status{
				Status: metav1.StatusSuccess,
			}})
			return
		}
		_ = streams.writeStatus(&apierrors.StatusError{ErrStatus: metav1.Status{
			Status: metav1.StatusFailure,
			Reason: remotecommandconsts.NonZeroExitCodeReason,
			Details: &metav1.StatusDetails{
				Causes: []metav1.StatusCause{{
					Type:    remotecommandconsts.ExitCodeCauseType,
					Message: itoa(script.exitCode),
				}},
			},
		}})
	}))
}

// upgradeToV4 performs the server-side SPDY v4 handshake + stream collection.
func upgradeToV4(w http.ResponseWriter, req *http.Request) (*serverStreams, error) {
	if _, err := httpstream.Handshake(req, w, []string{remotecommandconsts.StreamProtocolV4Name}); err != nil {
		return nil, err
	}

	type streamAndReply struct {
		httpstream.Stream
		replySent <-chan struct{}
	}
	streamCh := make(chan streamAndReply)
	upgrader := spdy.NewResponseUpgrader()
	conn := upgrader.UpgradeResponse(w, req, func(stream httpstream.Stream, replySent <-chan struct{}) error {
		streamCh <- streamAndReply{Stream: stream, replySent: replySent}
		return nil
	})
	if conn == nil {
		return nil, errors.New("upgrade returned nil connection")
	}

	// podexec sets Stdout=true, Stderr=true, no Stdin => error + stdout + stderr.
	const expected = 3
	out := &serverStreams{conn: conn}
	replyCh := make(chan struct{}, expected)
	received := 0
	for received < expected {
		select {
		case s := <-streamCh:
			switch s.Headers().Get(corev1.StreamType) {
			case corev1.StreamTypeError:
				out.writeStatus = v4WriteStatus(s)
				replyCh <- struct{}{}
			case corev1.StreamTypeStdout:
				out.stdoutW = s
				replyCh <- struct{}{}
			case corev1.StreamTypeStderr:
				out.stderrW = s
				replyCh <- struct{}{}
			default:
				return nil, errors.New("unexpected stream type")
			}
		case <-replyCh:
			received++
		}
	}
	return out, nil
}

func v4WriteStatus(stream io.Writer) func(*apierrors.StatusError) error {
	return func(status *apierrors.StatusError) error {
		bs, err := json.Marshal(status.Status())
		if err != nil {
			return err
		}
		_, err = stream.Write(bs)
		return err
	}
}

func itoa(i int) string {
	// small positive ints only; avoids strconv import churn in the fake.
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

// executorForServer builds a podexec.Executor whose REST config points at the
// fake server's host.
func executorForServer(t *testing.T, srv *httptest.Server) *podexec.Executor {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	e, err := podexec.New(&rest.Config{Host: u.Host})
	if err != nil {
		t.Fatalf("podexec.New: %v", err)
	}
	return e
}

// --- Tests ------------------------------------------------------------------

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *rest.Config
		wantErr bool
	}{
		{name: "valid host", cfg: &rest.Config{Host: "https://example.test:6443"}, wantErr: false},
		{
			name: "invalid rate-limiter config rejected",
			cfg: &rest.Config{
				Host: "https://example.test:6443",
				// QPS>0 with Burst<=0 and no RateLimiter makes
				// kubernetes.NewForConfig fail validation.
				QPS:   10,
				Burst: 0,
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, err := podexec.New(tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatal("New: expected error, got nil")
				}
				if e != nil {
					t.Errorf("New: expected nil Executor on error, got %v", e)
				}
				return
			}
			if err != nil {
				t.Fatalf("New: unexpected error: %v", err)
			}
			if e == nil {
				t.Fatal("New: got nil Executor without error")
			}
		})
	}
}

func TestExecSuccess(t *testing.T) {
	srv := newFakeExecServer(t, &execScript{stdout: "hello-out", stderr: "warn-err", exitCode: 0})
	defer srv.Close()
	e := executorForServer(t, srv)

	stdout, stderr, err := e.Exec(context.Background(), "ns", "pod", "agent", []string{"echo", "hi"})
	if err != nil {
		t.Fatalf("Exec: unexpected error: %v", err)
	}
	if string(stdout) != "hello-out" {
		t.Errorf("stdout: got %q, want %q", stdout, "hello-out")
	}
	if string(stderr) != "warn-err" {
		t.Errorf("stderr: got %q, want %q", stderr, "warn-err")
	}
}

func TestExecNonZeroExit(t *testing.T) {
	srv := newFakeExecServer(t, &execScript{stdout: "partial", stderr: "boom", exitCode: 7})
	defer srv.Close()
	e := executorForServer(t, srv)

	stdout, stderr, err := e.Exec(context.Background(), "ns", "pod", "agent", []string{"false"})
	if err == nil {
		t.Fatal("Exec: expected non-zero-exit error, got nil")
	}
	if !strings.Contains(err.Error(), "podexec: stream") {
		t.Errorf("error not wrapped by podexec: %v", err)
	}
	// The underlying error carries the exit code.
	var codeErr utilexec.CodeExitError
	if !errors.As(err, &codeErr) {
		t.Fatalf("error is not a CodeExitError: %v", err)
	}
	if codeErr.Code != 7 {
		t.Errorf("exit code: got %d, want 7", codeErr.Code)
	}
	// Bytes streamed before the failure are still returned to the caller.
	if string(stdout) != "partial" {
		t.Errorf("stdout: got %q, want %q", stdout, "partial")
	}
	if string(stderr) != "boom" {
		t.Errorf("stderr: got %q, want %q", stderr, "boom")
	}
}

func TestExecStreamSetupFailure(t *testing.T) {
	// nil script => server refuses the upgrade with HTTP 400, so the SPDY
	// stream cannot be established and StreamWithContext fails.
	srv := newFakeExecServer(t, nil)
	defer srv.Close()
	e := executorForServer(t, srv)

	stdout, stderr, err := e.Exec(context.Background(), "ns", "pod", "agent", []string{"true"})
	if err == nil {
		t.Fatal("Exec: expected stream-setup error, got nil")
	}
	if !strings.Contains(err.Error(), "podexec: stream") {
		t.Errorf("error not wrapped by podexec stream path: %v", err)
	}
	if len(stdout) != 0 || len(stderr) != 0 {
		t.Errorf("expected empty stdout/stderr on setup failure, got %q / %q", stdout, stderr)
	}
}

func TestExecTimeout(t *testing.T) {
	// A normal, responsive fake server: the deadline, not the server, ends the
	// call. We pass an already-expired context so remotecommand's SPDY executor
	// aborts during connection setup and returns the context error *before* it
	// spawns the stdout/stderr copy goroutines. (Letting the deadline fire
	// mid-stream instead would race those leaked goroutines against Exec's
	// buffer read at podexec.go:65 — a real production property documented in
	// the SUMMARY, not something we can assert race-clean here.)
	srv := newFakeExecServer(t, &execScript{stdout: "ignored", exitCode: 0})
	defer srv.Close()
	e := executorForServer(t, srv)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, _, err := e.Exec(ctx, "ns", "pod", "agent", []string{"sleep", "infinity"})
	if err == nil {
		t.Fatal("Exec: expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "podexec: stream") {
		t.Errorf("error not wrapped by podexec stream path: %v", err)
	}
	// The expired deadline surfaces as a timeout. Depending on where the SPDY
	// round-tripper observes the deadline it may be the context sentinel, an
	// os.ErrDeadlineExceeded, a net.Error with Timeout()==true, or (during
	// dial) a wrapped "i/o timeout". Accept any of those timeout signals.
	var netErr net.Error
	timedOut := errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, os.ErrDeadlineExceeded) ||
		(errors.As(err, &netErr) && netErr.Timeout()) ||
		strings.Contains(err.Error(), "timeout")
	if !timedOut {
		t.Errorf("expected a timeout error, got %v", err)
	}
}

// TestExecRequestShape asserts Exec targets the pod exec subresource with the
// argv and container the caller passed (defense against a silent regression in
// the request builder).
func TestExecRequestShape(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		gotQuery = req.URL.RawQuery
		// Refuse the upgrade; we only care about the request line here.
		http.Error(w, "inspected", http.StatusBadRequest)
	}))
	defer srv.Close()
	e := executorForServer(t, srv)

	_, _, _ = e.Exec(context.Background(), "tenant-a", "agent-pod", "agent", []string{"/bin/sh", "-c", "id"})

	if !strings.HasSuffix(gotPath, "/namespaces/tenant-a/pods/agent-pod/exec") {
		t.Errorf("request path: got %q, want .../namespaces/tenant-a/pods/agent-pod/exec", gotPath)
	}
	q, err := url.ParseQuery(gotQuery)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	if q.Get("container") != "agent" {
		t.Errorf("container param: got %q, want agent", q.Get("container"))
	}
	cmds := q["command"]
	wantCmds := []string{"/bin/sh", "-c", "id"}
	if len(cmds) != len(wantCmds) {
		t.Fatalf("command params: got %v, want %v", cmds, wantCmds)
	}
	for i := range wantCmds {
		if cmds[i] != wantCmds[i] {
			t.Errorf("command[%d]: got %q, want %q", i, cmds[i], wantCmds[i])
		}
	}
	if q.Get("stdout") != "true" || q.Get("stderr") != "true" {
		t.Errorf("stdout/stderr params: got stdout=%q stderr=%q, want true/true", q.Get("stdout"), q.Get("stderr"))
	}
	if q.Get("stdin") == "true" {
		t.Errorf("stdin should not be requested, got stdin=%q", q.Get("stdin"))
	}
}
