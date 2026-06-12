// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// errRateLimited is returned by ModelDiscoverer.Discover when the provider
// responds 429. The reconciler treats it specially: back off 2x (capped at 30m)
// and requeue without flipping to Degraded.
var errRateLimited = fmt.Errorf("model discovery rate-limited (429)")

// isRateLimited reports whether err is (or wraps) the rate-limit sentinel.
func isRateLimited(err error) bool {
	return err != nil && err == errRateLimited //nolint:errorlint // sentinel is never wrapped here
}

// ModelDiscoverer fetches the available model IDs for a ModelProvider. The real
// implementation (HTTPModelDiscoverer) performs an HTTP GET against the
// provider's model-list endpoint; tests inject a fake that returns a fixed list
// (or errRateLimited) without any network call.
//
// SECURITY: the discoverer is given only the resolved endpoint URL and provider
// type — never a credential value. It does not read or carry upstream API keys;
// model-list endpoints used here are unauthenticated or use the controller's own
// projected identity out of band. Credentials for inference traffic are injected
// at the Envoy AI Gateway (rule 05.6), not here.
type ModelDiscoverer interface {
	// Discover returns the sorted list of model IDs advertised by the provider.
	Discover(ctx context.Context, provider keesev1alpha1.ModelProviderType, endpoint string) ([]string, error)
}

// providerModelListPath maps a provider to the path appended to its base
// endpoint for model discovery. Providers without a standard list endpoint are
// absent and yield ErrDiscoveryUnsupported.
var providerModelListPath = map[keesev1alpha1.ModelProviderType]string{
	keesev1alpha1.ModelProviderOpenAI:      "/v1/models",
	keesev1alpha1.ModelProviderAzureOpenAI: "/openai/models?api-version=2024-02-01",
	keesev1alpha1.ModelProviderOllama:      "/api/tags",
	keesev1alpha1.ModelProviderGemini:      "/v1beta/models",
}

// ErrDiscoveryUnsupported is returned when a provider has no known model-list
// endpoint. The reconciler treats it as a non-fatal Synced=False condition.
var ErrDiscoveryUnsupported = fmt.Errorf("model discovery not supported for provider")

// defaultEndpoints supplies the well-known base URL for providers whose endpoint
// is optional in the spec.
var defaultEndpoints = map[keesev1alpha1.ModelProviderType]string{
	keesev1alpha1.ModelProviderOpenAI:    "https://api.openai.com",
	keesev1alpha1.ModelProviderAnthropic: "https://api.anthropic.com",
	keesev1alpha1.ModelProviderGemini:    "https://generativelanguage.googleapis.com",
}

// resolveEndpoint returns the spec endpoint, or the provider default when empty.
func resolveEndpoint(provider keesev1alpha1.ModelProviderType, endpoint string) string {
	if endpoint != "" {
		return endpoint
	}
	return defaultEndpoints[provider]
}

// HTTPModelDiscoverer is the production ModelDiscoverer. Timeout bounds each poll.
type HTTPModelDiscoverer struct {
	Client *http.Client
}

// NewHTTPModelDiscoverer builds an HTTPModelDiscoverer with a bounded timeout.
func NewHTTPModelDiscoverer() *HTTPModelDiscoverer {
	return &HTTPModelDiscoverer{Client: &http.Client{Timeout: 15 * time.Second}}
}

// Discover implements ModelDiscoverer.
func (d *HTTPModelDiscoverer) Discover(
	ctx context.Context,
	provider keesev1alpha1.ModelProviderType,
	endpoint string,
) ([]string, error) {
	path, ok := providerModelListPath[provider]
	if !ok {
		return nil, ErrDiscoveryUnsupported
	}
	base := resolveEndpoint(provider, endpoint)
	if base == "" {
		return nil, fmt.Errorf("no endpoint resolved for provider %s", provider)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build discovery request: %w", err)
	}
	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discovery request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery returned status %d", resp.StatusCode)
	}

	return parseModelList(resp.Body)
}

// parseModelList decodes the union of the shapes the supported provider
// model-list endpoints return: OpenAI/Gemini-style {"data":[{"id":...}]} or
// {"models":[{"name":...}]}, and Ollama-style {"models":[{"name":...}]}.
func parseModelList(r io.Reader) ([]string, error) {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode model list: %w", err)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(payload.Data)+len(payload.Models))
	for _, m := range payload.Data {
		if m.ID != "" {
			if _, dup := seen[m.ID]; !dup {
				seen[m.ID] = struct{}{}
				out = append(out, m.ID)
			}
		}
	}
	for _, m := range payload.Models {
		if m.Name != "" {
			if _, dup := seen[m.Name]; !dup {
				seen[m.Name] = struct{}{}
				out = append(out, m.Name)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}
