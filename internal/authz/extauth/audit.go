// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package extauth

import (
	"time"

	"github.com/go-logr/logr"
)

// AuditFields are the only fields ever written to the audit log.
// Strict allowlist (rule 02 + spec §10): never tokens, never bodies,
// never raw header values. The user-id is logged as-is because
// `service_account:system:serviceaccount:<ns>:ksa-<wsuid>` and
// `user:<email>` are the only shapes the FGA model accepts — but
// implementations adding new shapes (per JWT claim) MUST audit
// them only after redaction is added here.
type AuditFields struct {
	RequestID     string
	Path          string
	Method        string
	BindingName   string
	BindingNS     string
	FinalToolName string
	User          string
	Workspace     string // namespace/name
	Decision      string // "allow" | "deny"
	Reason        string // ReasonAllowed | ReasonNoMatch | ReasonSubjectError | ReasonFGADenied | ReasonFGAError
	Duration      time.Duration

	// Cross-tenant a2a audit fields (rule 05.10: (tuple, SA, from, to,
	// decision); never tokens, never bodies). Empty on the egress path.
	Tuple         string // canonical `object#relation@user`
	FromWorkspace string // W_from (caller workspace id)
	ToWorkspace   string // W_to (peer workspace id)
}

// LogAudit emits a single structured log line per request. Allow at
// debug level; deny at info; FGA errors at error.
func LogAudit(log logr.Logger, f AuditFields) {
	args := []any{
		"request_id", f.RequestID,
		"path", f.Path,
		"method", f.Method,
		"binding", f.BindingName,
		"binding_ns", f.BindingNS,
		"tool", f.FinalToolName,
		"user", f.User,
		"workspace", f.Workspace,
		"decision", f.Decision,
		"reason", f.Reason,
		"duration_ms", f.Duration.Milliseconds(),
	}
	// Cross-tenant a2a decisions carry the (tuple, from, to) shape per
	// rule 05.10. Only appended when present so the egress path's audit
	// line is unchanged.
	if f.Tuple != "" || f.FromWorkspace != "" || f.ToWorkspace != "" {
		args = append(args,
			"tuple", f.Tuple,
			"from_workspace", f.FromWorkspace,
			"to_workspace", f.ToWorkspace,
		)
	}
	switch f.Reason {
	case ReasonFGAError:
		log.Error(nil, "ext_authz check error", args...)
	case ReasonAllowed:
		log.V(1).Info("ext_authz allow", args...)
	default:
		log.Info("ext_authz deny", args...)
	}
}

// AuditFromDecision builds AuditFields from a Decision + the request
// shape. The gRPC server passes the request ID separately because
// it's pulled from `x-request-id` not the binding.
func AuditFromDecision(d *Decision, req *HTTPRequest, requestID string, dur time.Duration) AuditFields {
	decision := "deny"
	if d.Allowed {
		decision = "allow"
	}
	ws := ""
	if d.Workspace.Namespace != "" || d.Workspace.Name != "" {
		ws = d.Workspace.Namespace + "/" + d.Workspace.Name
	}
	f := AuditFields{
		RequestID:     requestID,
		Path:          req.Path,
		Method:        req.Method,
		BindingName:   d.BindingName,
		BindingNS:     d.BindingNS,
		FinalToolName: d.FinalToolName,
		User:          d.User,
		Workspace:     ws,
		Decision:      decision,
		Reason:        d.Reason,
		Duration:      dur,
	}
	if d.CrossTenant {
		f.Tuple = d.Tuple
		f.FromWorkspace = d.CallerWorkspace
		f.ToWorkspace = d.PeerWorkspace
	}
	return f
}
