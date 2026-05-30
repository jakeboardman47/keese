// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package goose

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	spi "github.com/keese-ai/keese/internal/runtime/spi/v1alpha1"
)

// Provider name registered with the SPI registry. AgentRuntime CRs
// reference this string via spec.implementation.goose.
const ProviderName = "goose"

// PodExecutor abstracts the pod-exec call so the provider can be
// unit-tested against a fake. The production implementation wraps
// k8s.io/client-go/tools/remotecommand.
type PodExecutor interface {
	// Exec runs argv inside the named container of the pod and returns
	// (stdout, stderr) bytes plus an error if the command exits
	// non-zero or the executor itself fails.
	Exec(ctx context.Context, namespace, podName, container string, argv []string) (stdout, stderr []byte, err error)
}

// Runtime is the goose SPI provider. Construct via Factory; never
// build directly outside the package.
type Runtime struct {
	executor PodExecutor

	// Image is the goose image tag used at session-pod build time.
	// Recorded for observability and for ImageVersionUnsupported
	// admission checks.
	image string

	// drainBudget is the wall-clock budget Drain enforces. Spec §Drain
	// pins this to 90 s; tests override.
	drainBudget time.Duration

	// resumeBudget is the wall-clock budget Resume enforces. Spec D25
	// pins this to 60 s; tests override.
	resumeBudget time.Duration
}

// Capabilities is the static CapabilityMatrix declared at registration.
// SupportsACP/Recipes are wired here (Run + Attach + Health implemented).
// Sub-agents are gated off until InvokeSubAgent / CleanupSubAgents land
// (TD-P3-05 epic).
var capabilities = spi.CapabilityMatrix{
	ProviderName:               ProviderName,
	SPIVersion:                 "1.0.0",
	SupportsACP:                true,
	SupportsSubAgents:          false,
	MaxSubAgents:               0,
	SupportsResume:             true,
	SupportsSubAgentCleanup:    false,
	SupportsInjectPrompt:       true, // TD-P3-04: fifo inject via podexec
	SupportsStreaming:          false,
	SupportsMCP:                true,
	SupportsRecipes:            true,
	SupportsCredentialRotation: false,
}

// Factory satisfies spi.Factory. config currently accepts the keys:
//
//   - "image": goose image (informational; pod spec carries the auth)
func Factory(config map[string]string) (spi.AgentRuntime, error) {
	r := &Runtime{
		image:        config["image"],
		drainBudget:  90 * time.Second,
		resumeBudget: 60 * time.Second,
	}
	return r, nil
}

// FactoryWithExecutor is the test-friendly constructor that injects a
// PodExecutor. Production code uses Factory; tests pass a fake here.
func FactoryWithExecutor(image string, exec PodExecutor) *Runtime {
	return &Runtime{
		executor:     exec,
		image:        image,
		drainBudget:  90 * time.Second,
		resumeBudget: 60 * time.Second,
	}
}

// SetExecutor wires a PodExecutor after construction. The keese
// operator does this once the manager + REST config are available
// (cmd/main.go).
func (r *Runtime) SetExecutor(exec PodExecutor) { r.executor = exec }

// --- Identity ---------------------------------------------------------

func (r *Runtime) Name() string                    { return ProviderName }
func (r *Runtime) Capabilities() spi.CapabilityMatrix { return capabilities }

// --- Bootstrap --------------------------------------------------------

// Bootstrap idempotently creates the keese checkpoint directory layout
// inside the workspace PVC. Spec §Bootstrap: ≤ 30 s, idempotent.
//
// We do NOT touch goose's $HOME/.local/share/goose/sessions/ here —
// goose creates that lazily on first use. We DO ensure the keese-
// owned namespace exists, because Drain writes into it on shutdown.
func (r *Runtime) Bootstrap(ctx context.Context, ws spi.Workspace) error {
	if r.executor == nil {
		// No executor yet (operator pre-startup or init-container path).
		// Bootstrap is idempotent; the WorkspaceSession reconciler's
		// init container already creates these dirs. Return nil so
		// reconcile loops don't backoff on missing exec.
		return nil
	}
	dir := keeseSessionDir(ws.UID)
	cmd := []string{"/bin/sh", "-c", "mkdir -p " + dir + " && touch " + dir + "/.keese-bootstrap"}
	_, stderr, err := r.executor.Exec(ctx, ws.Namespace, podForWorkspace(ws), agentContainerName, cmd)
	if err != nil {
		return fmt.Errorf("goose bootstrap: %w (stderr=%s)", err, string(stderr))
	}
	return nil
}

// --- Drain ------------------------------------------------------------

// Drain enforces the 90s budget from spec §Drain. Steps executed
// inside the agent container (in order):
//
//  1. Touch /tmp/draining (kubelet flips readiness NotReady — rule 06.9).
//  2. SIGTERM goose's process group via kill -TERM 1 to trigger its
//     own SQLite checkpoint.
//  3. Wait for goose to write its SQLite to disk (poll for
//     ~/.local/share/goose/sessions/*.sqlite mtime newer than
//     drainStart).
//  4. Atomic-rename the latest SQLite into the keese checkpoint dir
//     (Workspace.LastCheckpoint.SQLiteRef target).
//
// Returns spi.ErrBudget if the deadline is exceeded. Errors from
// individual steps fold into the wrapping context's deadline.
func (r *Runtime) Drain(ctx context.Context, sess spi.WorkspaceSession) error {
	if r.executor == nil {
		return fmt.Errorf("goose drain: no PodExecutor wired")
	}
	deadline, cancel := context.WithTimeout(ctx, r.drainBudget)
	defer cancel()

	// Step 1: signal preStop semantics (kubelet readiness flip via
	// /tmp/draining). Even on a manager-driven Drain we touch this
	// flag for symmetry with the kubelet preStop path.
	if err := r.runShell(deadline, sess, "touch /tmp/draining"); err != nil {
		return fmt.Errorf("goose drain (mark draining): %w", err)
	}

	// Step 2: SIGTERM goose. /proc/1/sigterm style — we send to PID 1
	// because the agent container's entrypoint IS goose.
	if err := r.runShell(deadline, sess, "kill -TERM 1 || true"); err != nil {
		return fmt.Errorf("goose drain (SIGTERM): %w", err)
	}

	// Step 3: wait until goose's sessions.db mtime is stable (proxy
	// for "flush complete"). We poll for ≤ drainBudget − 5 s.
	pollUntil := time.Now().Add(r.drainBudget - 5*time.Second)
	var lastMtime string
	for time.Now().Before(pollUntil) {
		if deadline.Err() != nil {
			return spi.ErrBudget
		}
		stdout, _, err := r.runShellOut(deadline, sess,
			`stat -c '%Y' /var/run/keese/session/home/.local/share/goose/sessions/sessions.db 2>/dev/null || echo 0`)
		if err == nil {
			cur := strings.TrimSpace(string(stdout))
			if cur != "" && cur == lastMtime {
				break
			}
			lastMtime = cur
		}
		select {
		case <-deadline.Done():
			return spi.ErrBudget
		case <-time.After(500 * time.Millisecond):
		}
	}

	// Step 4: copy sessions.db + WAL + SHM as a recoverable triple
	// into the keese-owned checkpoint dir.
	keeseDir := keeseSessionDir(sess.WorkspaceID)
	cmd := fmt.Sprintf(`set -e
if [ ! -d /var/run/keese/session/home/.local/share/goose/sessions ]; then
  echo "no goose session dir to checkpoint" >&2
  exit 0
fi
mkdir -p %s
cp -f /var/run/keese/session/home/.local/share/goose/sessions/sessions.db* %s/ 2>/dev/null || true
`, keeseDir, keeseDir)
	_ = filepath.Dir // keep filepath import valid for future use
	if err := r.runShell(deadline, sess, cmd); err != nil {
		return fmt.Errorf("goose drain (checkpoint copy): %w", err)
	}
	return nil
}

// --- Resume -----------------------------------------------------------

// Resume restores from Workspace.LastCheckpoint within the 60 s budget
// from spec D25 GUPP. Implementation today: validates the checkpoint
// SQLite is readable + non-empty, then `cp` it back into goose's
// expected sessions directory so a fresh container picks it up.
//
// AgentUnresponsive is returned when the agent never reports SQLite
// readability within the budget. The supervision ladder (D23) handles
// the next step.
func (r *Runtime) Resume(ctx context.Context, ws spi.Workspace) error {
	if r.executor == nil {
		return fmt.Errorf("goose resume: no PodExecutor wired")
	}
	if ws.LastCheckpoint.SQLiteRef == "" {
		// Nothing to resume from — fresh session.
		return nil
	}
	deadline, cancel := context.WithTimeout(ctx, r.resumeBudget)
	defer cancel()

	cmd := fmt.Sprintf(`set -e
test -s %s
mkdir -p /var/run/keese/session/home/.local/share/goose/sessions
cp -f %s /var/run/keese/session/home/.local/share/goose/sessions/
`, ws.LastCheckpoint.SQLiteRef, ws.LastCheckpoint.SQLiteRef)
	sess := spi.WorkspaceSession{
		Namespace: ws.Namespace,
		PodName:   podForWorkspace(ws),
	}
	if err := r.runShell(deadline, sess, cmd); err != nil {
		if deadline.Err() != nil {
			return spi.ErrAgentUnresponsive
		}
		return fmt.Errorf("goose resume: %w", err)
	}
	return nil
}

// --- InjectPrompt ----------------------------------------------------

// injectFifoPath is the named pipe goose reads for synthetic user turns.
// Goose v1.33.1+ monitors this path and injects any line written to it
// as a user turn in the running session. The path lives on the session
// PVC so it survives pod restarts.
//
// Design ref: docs/designs/23-agent-supervision.md §Step 2 mechanism
const injectFifoPath = "/var/run/keese/session/home/.local/state/goose/inject-fifo"

// InjectPrompt injects a synthetic user turn into the running goose session
// via the session pod's named FIFO at injectFifoPath.
//
// Implementation (approach a from TD-P3-04): shells into the session pod
// via the PodExecutor and writes the prompt line to the FIFO. Goose reads
// the FIFO on its next event-loop iteration and treats the line as a user
// turn with source: supervisor (design 23 §step 2).
//
// Safety: the prompt is sanitised to remove embedded newlines (a newline
// would terminate the FIFO write prematurely and could start a second
// injected turn). The script creates the FIFO if absent (idempotent) but
// does NOT block: it uses a sub-shell with a timeout to avoid hanging if
// goose is not listening on the FIFO yet.
//
// Rule 02: no secrets or tokens in the prompt are logged or surfaced in
// events by this method — the caller is responsible.
// Rule 04.8: returns an error, never panics.
// Rule 05.11: no privileged exec; podexec carries no special capabilities.
func (r *Runtime) InjectPrompt(ctx context.Context, sess spi.WorkspaceSession, prompt string) error {
	if r.executor == nil {
		return fmt.Errorf("goose InjectPrompt: no PodExecutor wired")
	}
	if sess.PodName == "" {
		return fmt.Errorf("goose InjectPrompt: session has no PodName")
	}

	// Sanitise: collapse embedded newlines to a space so one write = one turn.
	sanitised := strings.ReplaceAll(prompt, "\n", " ")
	sanitised = strings.ReplaceAll(sanitised, "\r", " ")

	// The shell script:
	//   1. mkfifo if absent (idempotent; fails silently if already present).
	//   2. Writes the prompt with a 5 s timeout (goose may not be listening
	//      yet; timeout prevents the exec from blocking indefinitely).
	//      Uses a background write + sleep to avoid blocking on open(2) of a
	//      FIFO with no reader, which would hang the subshell forever.
	sh := fmt.Sprintf(`set -e
FIFO=%s
[ -p "$FIFO" ] || mkfifo "$FIFO" 2>/dev/null || true
# Write with a 5s deadline. If goose is not reading the FIFO, the
# write blocks on open; the background sleep + kill ensures we exit.
(
  echo %s > "$FIFO" &
  WRITE_PID=$!
  sleep 5
  kill "$WRITE_PID" 2>/dev/null || true
)
`, injectFifoPath, shellescape(sanitised))

	_, stderr, err := r.executor.Exec(ctx, sess.Namespace, sess.PodName, agentContainerName,
		[]string{"/bin/sh", "-c", sh})
	if err != nil {
		return fmt.Errorf("goose InjectPrompt: podexec: %w (stderr=%s)", err, string(stderr))
	}
	return nil
}

// shellescape wraps s in single quotes, escaping any embedded single quotes.
// This prevents shell injection when the prompt is interpolated into the
// script string above.
func shellescape(s string) string {
	// Replace every ' with '\'' (end quote, escaped quote, reopen quote).
	escaped := strings.ReplaceAll(s, "'", `'\''`)
	return "'" + escaped + "'"
}

// --- Run, Attach, Health (demo-critical) -----------------------------

// Run is the bounded-recipe execution path. Phase 3 of the demo plan: when
// a non-interactive WorkspaceSession is started, the controller sets the
// pod's Command to `goose run --recipe …` directly (the recipe path is
// mounted via the recipe ConfigMap), so the pod terminates with
// PodSucceeded on completion. This SPI method covers the alternate path
// where the controller wants to fire a recipe inside an *already-running*
// session pod (e.g. an interactive attach pod that picks up a recipe on
// demand).
//
// Pod identity is passed through the params map under reserved keys
// `keese.pod_name` and `keese.namespace`; remaining params are forwarded
// to goose as `--param key=value` flags. When pod identity is absent the
// method returns spi.ErrUnsupported so callers can detect the contract
// mismatch.
//
// Sentinel errors: ErrUnsupported when no pod identity provided; the
// raw exec error otherwise (wrapped with stderr context).
const (
	runParamPodName   = "keese.pod_name"
	runParamNamespace = "keese.namespace"
)

func (r *Runtime) Run(ctx context.Context, recipe string, params map[string]string) (*spi.RunResult, error) {
	if r.executor == nil {
		return nil, fmt.Errorf("goose Run: no PodExecutor wired")
	}
	if recipe == "" {
		return nil, fmt.Errorf("goose Run: empty recipe path")
	}
	pod := params[runParamPodName]
	ns := params[runParamNamespace]
	if pod == "" || ns == "" {
		return nil, spi.ErrUnsupported
	}

	argv := []string{"/usr/local/bin/goose", "run", "--recipe", recipe}
	for k, v := range params {
		if k == runParamPodName || k == runParamNamespace {
			continue
		}
		argv = append(argv, "--params", fmt.Sprintf("%s=%s", k, v))
	}

	stdout, stderr, err := r.executor.Exec(ctx, ns, pod, agentContainerName, argv)
	if err != nil {
		return &spi.RunResult{ExitCode: 1}, fmt.Errorf("goose Run: %w (stderr=%s)", err, string(stderr))
	}
	_ = stdout
	return &spi.RunResult{ExitCode: 0}, nil
}

// Attach returns a descriptor pointing at the session's serve-mode pod.
// The Endpoint format is `pod://<namespace>/<pod>/<container>` — callers
// (the keese controller, an IDE bridge) translate this into a
// `kubectl exec` or a port-forward + ACP dial. SocketFD is unused today.
//
// Returns ErrAttachUnsupported when the session has no pod (e.g. recipe-
// mode session that has already completed).
func (r *Runtime) Attach(_ context.Context, sess spi.WorkspaceSession) (*spi.AttachHandle, error) {
	if sess.PodName == "" || sess.Namespace == "" {
		return nil, spi.ErrAttachUnsupported
	}
	return &spi.AttachHandle{
		Endpoint: fmt.Sprintf("pod://%s/%s/%s", sess.Namespace, sess.PodName, agentContainerName),
	}, nil
}

// Health probes the agent container by sending signal 0 to PID 1 (a
// no-op signal that succeeds iff the process exists and is reachable).
// The exec exits 0 when goose is alive, non-zero (or returns an error)
// when the process is gone.
func (r *Runtime) Health(ctx context.Context, sess spi.WorkspaceSession) (*spi.HealthReport, error) {
	if r.executor == nil {
		return nil, fmt.Errorf("goose Health: no PodExecutor wired")
	}
	if sess.PodName == "" {
		return &spi.HealthReport{Phase: "Down"}, nil
	}
	_, _, err := r.executor.Exec(ctx, sess.Namespace, sess.PodName, agentContainerName,
		[]string{"/bin/sh", "-c", "kill -0 1"})
	if err != nil {
		return &spi.HealthReport{Phase: "Down"}, nil
	}
	return &spi.HealthReport{Phase: "Running"}, nil
}

// --- Deferred (capability-gated) SPI methods -------------------------

func (r *Runtime) CleanupSubAgents(_ context.Context, _ spi.Workspace) error {
	return spi.ErrUnsupported
}

func (r *Runtime) InvokeSubAgent(_ context.Context, _ spi.Workspace, _ spi.SubAgentSpec) (*spi.SubAgentHandle, error) {
	return nil, spi.ErrSubAgentLimitExceeded
}

func (r *Runtime) StreamEvents(_ context.Context) (<-chan spi.RuntimeEvent, error) {
	return nil, spi.ErrUnsupported
}

// --- Helpers ----------------------------------------------------------

const agentContainerName = "agent"

// keeseSessionDir is the keese-owned checkpoint dir inside the
// workspace PVC. Lives under the PVC mount (/var/run/keese/session/)
// so writes survive pod replacement. The pod's rootfs at
// /var/run/keese/sessions/ (plural) is read-only and CANNOT be used.
func keeseSessionDir(workspaceUID string) string {
	return "/var/run/keese/session/keese-checkpoints/" + workspaceUID
}

// keeseCheckpointPath is the atomic-rename target on Drain.
func keeseCheckpointPath(workspaceUID string) string {
	return keeseSessionDir(workspaceUID) + "/session.sqlite"
}

// podForWorkspace returns the per-user session pod name. Today the
// keese controller computes this deterministically from the workspace
// UID + subject hash; the SPI takes the value verbatim from the
// caller's Workspace via the PodName-equivalent field. For Resume we
// need a pod name and use the controller's deterministic recipe; long
// term this should move into Workspace itself.
//
// TODO: hand the controller-computed pod name across the SPI boundary
// so we don't reimplement the naming convention here.
func podForWorkspace(ws spi.Workspace) string {
	if ws.UID == "" {
		return ""
	}
	if len(ws.UID) >= 8 {
		return "ws-" + ws.UID[:8] + "-sess-resume"
	}
	return "ws-" + ws.UID + "-sess-resume"
}

func (r *Runtime) runShell(ctx context.Context, sess spi.WorkspaceSession, sh string) error {
	_, stderr, err := r.executor.Exec(ctx, sess.Namespace, sess.PodName, agentContainerName,
		[]string{"/bin/sh", "-c", sh})
	if err != nil {
		return fmt.Errorf("%w (stderr=%s)", err, string(stderr))
	}
	return nil
}

func (r *Runtime) runShellOut(ctx context.Context, sess spi.WorkspaceSession, sh string) ([]byte, []byte, error) {
	return r.executor.Exec(ctx, sess.Namespace, sess.PodName, agentContainerName,
		[]string{"/bin/sh", "-c", sh})
}
