// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package extauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
)

// Subject is the resolved (FGA user-id, FGA workspace-id) pair the
// Check needs.
type Subject struct {
	User      string // OpenFGA `user:<id>` string (already prefixed)
	Workspace WorkspaceID
}

// Errors specific to subject extraction.
var (
	ErrMissingAuthorization = errors.New("authorization header missing")
	ErrMalformedJWT         = errors.New("malformed JWT")
	ErrMissingSubject       = errors.New("subject missing in JWT")
	ErrMissingClaim         = errors.New("requested JWT claim missing")
	ErrUnknownSubject       = errors.New("subject does not look like a keese SA")
)

// ExtractSubject pulls the user + workspace out of the request per
// the binding's subjectFrom / workspaceFrom config. Returns a clean
// error rather than a zero-value subject.
//
// Authorization header is read but NEVER returned in errors or
// audited (rule 02 + spec §10).
func ExtractSubject(req *HTTPRequest, subjectFrom authzv1alpha1.SubjectFromSource, workspaceFrom authzv1alpha1.WorkspaceFromSource, claimName string) (*Subject, error) {
	authz, ok := req.Headers["authorization"]
	if !ok {
		return nil, ErrMissingAuthorization
	}
	const bearer = "Bearer "
	if !strings.HasPrefix(authz, bearer) {
		return nil, ErrMalformedJWT
	}
	claims, err := parseJWTClaims(strings.TrimPrefix(authz, bearer))
	if err != nil {
		return nil, fmt.Errorf("parse jwt: %w", err)
	}

	user, err := extractUser(claims, subjectFrom, claimName)
	if err != nil {
		return nil, err
	}
	ws, err := extractWorkspace(claims, workspaceFrom, claimName)
	if err != nil {
		return nil, err
	}
	return &Subject{User: user, Workspace: ws}, nil
}

func extractUser(claims map[string]any, source authzv1alpha1.SubjectFromSource, claimName string) (string, error) {
	switch source {
	case authzv1alpha1.SubjectFromServiceAccountSubject, "":
		sub, ok := claims["sub"].(string)
		if !ok || sub == "" {
			return "", ErrMissingSubject
		}
		// Sub shape: `system:serviceaccount:<ns>:<sa>`. Use just
		// the SA name as the FGA user-id — that's what the keese
		// Workspace controller writes the `service_account:<sa>`
		// tuple as. Embedding the full sub would produce
		// `service_account:system:serviceaccount:<ns>:<sa>`, which
		// OpenFGA rejects (too many colons in the user field).
		parts := strings.Split(sub, ":")
		saName := parts[len(parts)-1]
		if saName == "" {
			return "", ErrMissingSubject
		}
		return "service_account:" + saName, nil
	case authzv1alpha1.SubjectFromJWTClaim:
		if claimName == "" {
			return "", fmt.Errorf("subjectFrom=JWTClaim but jwtClaimName is empty")
		}
		v, ok := claims[claimName].(string)
		if !ok || v == "" {
			return "", ErrMissingClaim
		}
		// Caller-supplied claim values are already final FGA user
		// strings (e.g. "user:alice@example.com").
		if !strings.Contains(v, ":") {
			return "user:" + v, nil
		}
		return v, nil
	default:
		return "", fmt.Errorf("unknown subjectFrom %q", source)
	}
}

func extractWorkspace(claims map[string]any, source authzv1alpha1.WorkspaceFromSource, claimName string) (WorkspaceID, error) {
	switch source {
	case authzv1alpha1.WorkspaceFromServiceAccountName, "":
		sub, _ := claims["sub"].(string)
		// Expected shape `system:serviceaccount:<ns>:ksa-<wsuid>`.
		parts := strings.SplitN(sub, ":", 4)
		if len(parts) != 4 || parts[0] != "system" || parts[1] != "serviceaccount" {
			return WorkspaceID{}, ErrUnknownSubject
		}
		ns := parts[2]
		saName := parts[3]
		if !strings.HasPrefix(saName, "ksa-") {
			return WorkspaceID{}, ErrUnknownSubject
		}
		uid := strings.TrimPrefix(saName, "ksa-")
		// Workspace.Name is filled by the controller-runtime client
		// (an SA-name lookup); for the keese-authz hot path we only
		// need the UID (FGA `workspace:<uid>` user string) and the
		// namespace (to scope WorkspaceTool matches).
		return WorkspaceID{Namespace: ns, Name: uid, UID: uid}, nil
	case authzv1alpha1.WorkspaceFromJWTClaim:
		if claimName == "" {
			return WorkspaceID{}, fmt.Errorf("workspaceFrom=JWTClaim but jwtClaimName is empty")
		}
		v, ok := claims[claimName].(string)
		if !ok || v == "" {
			return WorkspaceID{}, ErrMissingClaim
		}
		// Claim format: `<namespace>/<name>` (caller-controlled).
		parts := strings.SplitN(v, "/", 2)
		if len(parts) != 2 {
			return WorkspaceID{}, fmt.Errorf("workspace claim %q must be `<namespace>/<name>`", v)
		}
		return WorkspaceID{Namespace: parts[0], Name: parts[1], UID: parts[1]}, nil
	default:
		return WorkspaceID{}, fmt.Errorf("unknown workspaceFrom %q", source)
	}
}

// parseJWTClaims decodes the payload segment of a JWT. We do NOT
// verify the signature — Envoy's `jwt_authn` filter is the
// signature-verification authority; keese-authz consumes the
// already-validated claims via header pass-through.
func parseJWTClaims(jwt string) (map[string]any, error) {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return nil, ErrMalformedJWT
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrMalformedJWT
	}
	var claims map[string]any
	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, ErrMalformedJWT
	}
	return claims, nil
}
