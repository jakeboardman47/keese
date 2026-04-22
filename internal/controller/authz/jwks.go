// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// JwksFetcher probes an OIDC provider's JWKS endpoint for reachability.
// It does not parse or validate the key material — just confirms the endpoint
// returns a 200-range response with a parseable JSON body.
type JwksFetcher interface {
	// Fetch checks whether the given JWKS URI is reachable.
	// Returns nil on success, a non-nil error on any failure.
	Fetch(ctx context.Context, jwksURI string) error
}

// HTTPJwksFetcher is the production JwksFetcher backed by net/http.
type HTTPJwksFetcher struct {
	// Client is the HTTP client to use. If nil, http.DefaultClient is used.
	Client *http.Client
}

const jwksFetchTimeout = 10 * time.Second

// Fetch performs a GET against jwksURI and validates that the response is 2xx
// and contains a JSON object with a "keys" field (minimal JWKS check).
func (f *HTTPJwksFetcher) Fetch(ctx context.Context, jwksURI string) error {
	hc := f.Client
	if hc == nil {
		hc = &http.Client{Timeout: jwksFetchTimeout}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return fmt.Errorf("building JWKS request: %w", err)
	}

	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("JWKS fetch failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("JWKS endpoint returned HTTP %d", resp.StatusCode)
	}

	var body struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("JWKS response is not valid JSON: %w", err)
	}
	return nil
}

// DeriveJWKSURI derives the JWKS URI from an OIDC issuer URL via the
// OpenID Connect Discovery document at /.well-known/openid-configuration.
// This is used when spec.jwksUri is not set.
func DeriveJWKSURI(ctx context.Context, hc *http.Client, issuer string) (string, error) {
	if hc == nil {
		hc = &http.Client{Timeout: jwksFetchTimeout}
	}
	discoveryURL := issuer + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", fmt.Errorf("building discovery request: %w", err)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("discovery fetch failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("discovery endpoint returned HTTP %d", resp.StatusCode)
	}

	var doc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", fmt.Errorf("discovery response is not valid JSON: %w", err)
	}
	if doc.JWKSURI == "" {
		return "", fmt.Errorf("discovery document missing jwks_uri field")
	}
	return doc.JWKSURI, nil
}

// FakeJwksFetcher is a test double for JwksFetcher.
// Set Err to simulate a fetch failure; leave nil for success.
type FakeJwksFetcher struct {
	// Err is returned by Fetch if non-nil.
	Err error
	// Calls records how many times Fetch was called.
	Calls int
	// LastURI is the last URI passed to Fetch.
	LastURI string
}

func (f *FakeJwksFetcher) Fetch(_ context.Context, jwksURI string) error {
	f.Calls++
	f.LastURI = jwksURI
	return f.Err
}

var _ JwksFetcher = &FakeJwksFetcher{}
var _ JwksFetcher = &HTTPJwksFetcher{}
