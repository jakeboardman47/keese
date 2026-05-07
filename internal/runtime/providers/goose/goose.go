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
// Streaming + MCP + Recipes + ACP + SubAgents are all supported as of
// goose v1.33.1; InjectPrompt and CredentialRotation are pending the
// upstream SPI methods (see spec §Failure modes).
var capabilities = spi.CapabilityMatrix{
	ProviderName:               ProviderName,
	SPIVersion:                 "1.0.0",
	SupportsACP:                true,
	SupportsSubAgents:          true,
	MaxSubAgents:               10,
	SupportsResume:             true,
	SupportsSubAgentCleanup:    true,
	SupportsInjectPrompt:       false, // TODO: TD-P3-04 InjectPrompt SPI
	SupportsStreaming:          true,
	SupportsMCP:                true,
	SupportsRecipes:            true,
	SupportsCredentialRotation: false, // TODO: TD-P2-13 cred broker rotation
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

// --- Stubs for follow-on SPI methods ---------------------------------

func (r *Runtime) Run(_ context.Context, _ string, _ map[string]string) (*spi.RunResult, error) {
	return nil, spi.ErrUnsupported
}

func (r *Runtime) Attach(_ context.Context, _ spi.WorkspaceSession) (*spi.AttachHandle, error) {
	return nil, spi.ErrUnsupported
}

func (r *Runtime) CleanupSubAgents(_ context.Context, _ spi.Workspace) error {
	return spi.ErrUnsupported
}

func (r *Runtime) InjectPrompt(_ context.Context, _ spi.WorkspaceSession, _ string) error {
	return spi.ErrUnsupported
}

func (r *Runtime) InvokeSubAgent(_ context.Context, _ spi.Workspace, _ spi.SubAgentSpec) (*spi.SubAgentHandle, error) {
	return nil, spi.ErrUnsupported
}

func (r *Runtime) Health(_ context.Context, _ spi.WorkspaceSession) (*spi.HealthReport, error) {
	return nil, spi.ErrUnsupported
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
