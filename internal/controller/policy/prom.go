// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package policy

import "context"

// QueryResult holds a single PromQL scalar result.
type QueryResult struct {
	// Value is the scalar float64 returned by the query (e.g. increase() over window).
	Value float64
}

// PrometheusQuerier executes a PromQL instant query and returns the scalar result.
// Production wiring is HTTPPrometheusQuerier (prom_http.go); FakePrometheusQuerier
// is used in tests.
type PrometheusQuerier interface {
	// Query executes a PromQL expression and returns the scalar result.
	// Returns an error if the Prometheus endpoint is unreachable or the query fails.
	Query(ctx context.Context, expr string) (QueryResult, error)
}

// FakePrometheusQuerier is a test double for PrometheusQuerier.
// It returns the configured result for any expression, or an error when FailNext is set.
type FakePrometheusQuerier struct {
	// Results maps PromQL expression prefix to return value.
	// If the expression is not found, DefaultValue is returned.
	Results map[string]float64

	// DefaultValue is returned for expressions not in Results.
	DefaultValue float64

	// FailNext causes the next Query call to return ErrPromQueryFailed.
	FailNext bool

	// Calls records the expressions queried, for assertion.
	Calls []string
}

// ErrPromQueryFailed is returned by FakePrometheusQuerier when FailNext is true.
type promQueryError struct{ expr string }

func (e promQueryError) Error() string {
	return "prometheus: query failed for expression: " + e.expr
}

// Query implements PrometheusQuerier.
func (f *FakePrometheusQuerier) Query(_ context.Context, expr string) (QueryResult, error) {
	f.Calls = append(f.Calls, expr)
	if f.FailNext {
		f.FailNext = false
		return QueryResult{}, promQueryError{expr: expr}
	}
	if v, ok := f.Results[expr]; ok {
		return QueryResult{Value: v}, nil
	}
	return QueryResult{Value: f.DefaultValue}, nil
}

var _ PrometheusQuerier = &FakePrometheusQuerier{}
