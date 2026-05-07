// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package rebac

import (
	"context"
	"errors"
	"fmt"
	"strings"

	openfga "github.com/openfga/go-sdk"
	"github.com/openfga/go-sdk/client"
)

// Config carries the operator-level OpenFGA connection parameters.
// All three fields are required for a real client; cmd/main.go decides
// whether to construct one based on whether APIURL is set.
type Config struct {
	APIURL               string
	StoreID              string
	AuthorizationModelID string
}

// Validate returns an error if any required field is missing.
func (c Config) Validate() error {
	if c.APIURL == "" {
		return errors.New("rebac: OPENFGA_API_URL is empty")
	}
	if c.StoreID == "" {
		return errors.New("rebac: OPENFGA_STORE_ID is empty")
	}
	if c.AuthorizationModelID == "" {
		return errors.New("rebac: OPENFGA_AUTHORIZATION_MODEL_ID is empty")
	}
	return nil
}

// Client wraps the OpenFGA SDK with the keese-side idempotency helpers.
// The zero value is not usable; use New.
type Client struct {
	fga                  *client.OpenFgaClient
	authorizationModelID string
}

// New constructs a real OpenFGA-backed Client.
func New(cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	fgaClient, err := client.NewSdkClient(&client.ClientConfiguration{
		ApiUrl:               cfg.APIURL,
		StoreId:              cfg.StoreID,
		AuthorizationModelId: cfg.AuthorizationModelID,
	})
	if err != nil {
		return nil, fmt.Errorf("rebac: build FGA client: %w", err)
	}
	return &Client{
		fga:                  fgaClient,
		authorizationModelID: cfg.AuthorizationModelID,
	}, nil
}

// Write emits a single (object, relation, user) tuple. Returns nil if the
// tuple already exists (idempotent semantics — matches the noop/test-fake
// writer interface contract used in fallback/test paths).
func (c *Client) Write(ctx context.Context, object, relation, user string) error {
	body := client.ClientWriteRequest{
		Writes: []client.ClientTupleKey{{
			Object:   object,
			Relation: relation,
			User:     user,
		}},
	}
	_, err := c.fga.Write(ctx).Body(body).Execute()
	if err == nil || isAlreadyExists(err) {
		return nil
	}
	return fmt.Errorf("rebac: write %s#%s@%s: %w", object, relation, user, err)
}

// Delete removes a single (object, relation, user) tuple. Returns nil if
// the tuple does not exist (idempotent).
func (c *Client) Delete(ctx context.Context, object, relation, user string) error {
	body := client.ClientWriteRequest{
		Deletes: []openfga.TupleKeyWithoutCondition{{
			Object:   object,
			Relation: relation,
			User:     user,
		}},
	}
	_, err := c.fga.Write(ctx).Body(body).Execute()
	if err == nil || isNotFound(err) {
		return nil
	}
	return fmt.Errorf("rebac: delete %s#%s@%s: %w", object, relation, user, err)
}

// Tuple is the (object, relation, user) triple returned by Read. Each
// field is the canonical OpenFGA string form: "<type>:<id>".
type Tuple struct {
	Object   string
	Relation string
	User     string
}

// Read returns every tuple matching the given filter. Empty fields are
// wildcards. The filter must specify at least one of object or user
// (OpenFGA's API requires this); relation alone is not a valid filter.
func (c *Client) Read(ctx context.Context, object, relation, user string) ([]Tuple, error) {
	body := client.ClientReadRequest{}
	if object != "" {
		body.Object = openfga.PtrString(object)
	}
	if relation != "" {
		body.Relation = openfga.PtrString(relation)
	}
	if user != "" {
		body.User = openfga.PtrString(user)
	}
	out := []Tuple{}
	var continuationToken string
	for {
		req := c.fga.Read(ctx).Body(body)
		if continuationToken != "" {
			req = req.Options(client.ClientReadOptions{
				ContinuationToken: openfga.PtrString(continuationToken),
			})
		}
		resp, err := req.Execute()
		if err != nil {
			return nil, fmt.Errorf("rebac: read %s#%s@%s: %w", object, relation, user, err)
		}
		for _, t := range resp.GetTuples() {
			k := t.GetKey()
			out = append(out, Tuple{
				Object:   k.GetObject(),
				Relation: k.GetRelation(),
				User:     k.GetUser(),
			})
		}
		continuationToken = resp.GetContinuationToken()
		if continuationToken == "" {
			break
		}
	}
	return out, nil
}

// Check evaluates whether `user` has `relation` on `object`. Wraps
// OpenFGA's `Check` API. Returns (true, nil) on allow, (false, nil)
// on deny, (false, err) on transport or server error.
//
// Used by the keese-authz ext_authz service to answer
// `tool:<name>#can_call@<subject>` per inbound request. The default
// authorization model resolves `can_call` from
// `tenant_member from allowed_in`, so a successful Check requires
// both halves of the tuple set:
//   - tenant:<t>#member@<sa> (written by Workspace controller)
//   - tool:<n>#allowed_in@workspace:<w> (written by Workspace
//     controller from spec.egress.allowedTools[])
func (c *Client) Check(ctx context.Context, user, relation, object string) (bool, error) {
	resp, err := c.fga.Check(ctx).Body(client.ClientCheckRequest{
		User:     user,
		Relation: relation,
		Object:   object,
	}).Execute()
	if err != nil {
		return false, fmt.Errorf("rebac: check %s#%s@%s: %w", object, relation, user, err)
	}
	return resp.GetAllowed(), nil
}

// isAlreadyExists matches the OpenFGA "tuple already exists" error
// signature. The SDK does not expose a typed sentinel for this; the
// HTTP API returns 400 with code "write_failed_due_to_invalid_input"
// and a body containing "already exists".
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "already exists") ||
		strings.Contains(s, "write_failed_due_to_invalid_input")
}

// isNotFound matches the OpenFGA "tuple not found" error returned by
// Delete when the tuple is not present.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "not found") ||
		strings.Contains(s, "cannot delete a tuple which does not exist")
}
