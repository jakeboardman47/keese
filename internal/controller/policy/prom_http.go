// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package policy

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// HTTPPrometheusQuerier is the production PrometheusQuerier. It issues an
// instant query against the Prometheus Query API. Returns a Scalar's value
// directly; for a Vector it sums the samples (typical for a `sum(...)` PromQL
// expression that still came back as a single-sample Vector).
type HTTPPrometheusQuerier struct {
	api v1.API
}

// NewHTTPPrometheusQuerier constructs a real querier against the given
// Prometheus base URL (e.g. http://prometheus.monitoring.svc:9090).
// Empty address returns an error — callers should fall back to the fake.
func NewHTTPPrometheusQuerier(address string) (*HTTPPrometheusQuerier, error) {
	if address == "" {
		return nil, fmt.Errorf("prometheus: empty address")
	}
	c, err := api.NewClient(api.Config{Address: address})
	if err != nil {
		return nil, fmt.Errorf("prometheus: build client: %w", err)
	}
	return &HTTPPrometheusQuerier{api: v1.NewAPI(c)}, nil
}

// Query implements PrometheusQuerier. Returns the scalar value of an instant
// query at "now". Warnings from Prometheus are intentionally dropped (the
// caller's caller surfaces query failures via the TokenBudget Degraded
// condition; per-warning surfacing would be noisy).
func (q *HTTPPrometheusQuerier) Query(ctx context.Context, expr string) (QueryResult, error) {
	val, _, err := q.api.Query(ctx, expr, time.Now())
	if err != nil {
		return QueryResult{}, fmt.Errorf("prometheus query %q: %w", expr, err)
	}
	switch v := val.(type) {
	case *model.Scalar:
		return QueryResult{Value: float64(v.Value)}, nil
	case model.Vector:
		var sum float64
		for _, s := range v {
			sum += float64(s.Value)
		}
		return QueryResult{Value: sum}, nil
	default:
		// Matrix / String / unknown — return zero rather than error so a
		// no-sample window doesn't fail the reconcile.
		return QueryResult{Value: 0}, nil
	}
}

var _ PrometheusQuerier = (*HTTPPrometheusQuerier)(nil)
