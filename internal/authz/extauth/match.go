// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package extauth

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
)

// HTTPRequest is the subset of an Envoy CheckRequest the matcher
// needs. Decoupled from envoy proto so the matcher is unit-testable.
type HTTPRequest struct {
	Path        string
	Method      string
	Headers     map[string]string // canonicalized lowercase keys
	QueryParams map[string]string
	Body        []byte // empty if Envoy didn't buffer the body
}

// CompiledMatch is a pre-compiled HTTPRouteMatch ready for hot-path
// evaluation. Regex matchers are pre-compiled to avoid per-request
// re-compilation.
type CompiledMatch struct {
	paths       []compiledPath
	methods     map[string]struct{} // empty = match any
	headers     []compiledHeader
	queryParams []compiledQueryParam

	body *compiledBody // nil when no body discriminator
}

type compiledPath struct {
	kind  authzv1alpha1.PathMatchType
	value string
	re    *regexp.Regexp // populated when kind == RegularExpression
}

type compiledHeader struct {
	name  string // lowercased
	kind  authzv1alpha1.HeaderMatchType
	value string
	re    *regexp.Regexp
}

type compiledQueryParam struct {
	name  string
	kind  authzv1alpha1.HeaderMatchType
	value string
	re    *regexp.Regexp
}

type compiledBody struct {
	jsonPath  []string // ["model"] for $.model; ["a","b"] for $.a.b
	mapping   map[string]string
	defaultTo string
}

// CompileMatch compiles a CRD HTTPRouteMatch + optional
// BodyDiscriminator into the pre-compiled form.
func CompileMatch(m authzv1alpha1.HTTPRouteMatch, b *authzv1alpha1.BodyDiscriminator) (*CompiledMatch, error) {
	cm := &CompiledMatch{methods: map[string]struct{}{}}

	for _, p := range m.Paths {
		cp := compiledPath{kind: p.Type, value: p.Value}
		if cp.kind == authzv1alpha1.PathMatchRegularExpression {
			re, err := regexp.Compile(p.Value)
			if err != nil {
				return nil, fmt.Errorf("compile path regex %q: %w", p.Value, err)
			}
			cp.re = re
		}
		cm.paths = append(cm.paths, cp)
	}
	for _, mth := range m.Methods {
		cm.methods[strings.ToUpper(string(mth))] = struct{}{}
	}
	for _, h := range m.Headers {
		ch := compiledHeader{name: strings.ToLower(h.Name), kind: h.Type, value: h.Value}
		if ch.kind == authzv1alpha1.HeaderMatchRegularExpression {
			re, err := regexp.Compile(h.Value)
			if err != nil {
				return nil, fmt.Errorf("compile header regex %q: %w", h.Value, err)
			}
			ch.re = re
		}
		cm.headers = append(cm.headers, ch)
	}
	for _, q := range m.QueryParams {
		cq := compiledQueryParam{name: q.Name, kind: q.Type, value: q.Value}
		if cq.kind == authzv1alpha1.HeaderMatchRegularExpression {
			re, err := regexp.Compile(q.Value)
			if err != nil {
				return nil, fmt.Errorf("compile query regex %q: %w", q.Value, err)
			}
			cq.re = re
		}
		cm.queryParams = append(cm.queryParams, cq)
	}
	if b != nil {
		segs, err := parseRestrictedJSONPath(b.JSONPath)
		if err != nil {
			return nil, fmt.Errorf("compile bodyDiscriminator jsonPath %q: %w", b.JSONPath, err)
		}
		cm.body = &compiledBody{
			jsonPath:  segs,
			mapping:   b.Map,
			defaultTo: b.Default,
		}
	}
	return cm, nil
}

// MatchResult is what Match returns. SubTool is the body-discriminator
// output — empty when the binding has no discriminator or the body
// value did not map to a sub-tool.
type MatchResult struct {
	Matched bool
	SubTool string
}

// Match evaluates the request against the compiled match. Returns
// (matched=false, "") when any AND-clause fails. Body discriminator
// is evaluated last so we don't decode JSON unless every other clause
// passed.
func (c *CompiledMatch) Match(req *HTTPRequest) MatchResult {
	if !c.matchPath(req.Path) {
		return MatchResult{}
	}
	if !c.matchMethod(req.Method) {
		return MatchResult{}
	}
	if !c.matchHeaders(req.Headers) {
		return MatchResult{}
	}
	if !c.matchQueryParams(req.QueryParams) {
		return MatchResult{}
	}
	sub := c.evalBody(req.Body)
	return MatchResult{Matched: true, SubTool: sub}
}

func (c *CompiledMatch) matchPath(path string) bool {
	if len(c.paths) == 0 {
		return false
	}
	for _, p := range c.paths {
		switch p.kind {
		case authzv1alpha1.PathMatchExact:
			if path == p.value {
				return true
			}
		case authzv1alpha1.PathMatchPathPrefix:
			if strings.HasPrefix(path, p.value) {
				return true
			}
		case authzv1alpha1.PathMatchRegularExpression:
			if p.re != nil && p.re.MatchString(path) {
				return true
			}
		}
	}
	return false
}

func (c *CompiledMatch) matchMethod(method string) bool {
	if len(c.methods) == 0 {
		return true // empty = any method
	}
	_, ok := c.methods[strings.ToUpper(method)]
	return ok
}

func (c *CompiledMatch) matchHeaders(headers map[string]string) bool {
	for _, h := range c.headers {
		got, ok := headers[h.name]
		if !ok {
			return false
		}
		switch h.kind {
		case authzv1alpha1.HeaderMatchExact:
			if got != h.value {
				return false
			}
		case authzv1alpha1.HeaderMatchRegularExpression:
			if h.re == nil || !h.re.MatchString(got) {
				return false
			}
		}
	}
	return true
}

func (c *CompiledMatch) matchQueryParams(params map[string]string) bool {
	for _, q := range c.queryParams {
		got, ok := params[q.name]
		if !ok {
			return false
		}
		switch q.kind {
		case authzv1alpha1.HeaderMatchExact:
			if got != q.value {
				return false
			}
		case authzv1alpha1.HeaderMatchRegularExpression:
			if q.re == nil || !q.re.MatchString(got) {
				return false
			}
		}
	}
	return true
}

// evalBody returns the sub-tool name. Empty string means "no
// discriminator OR no match — fall back to the parent toolName".
func (c *CompiledMatch) evalBody(body []byte) string {
	if c.body == nil {
		return ""
	}
	if len(body) == 0 {
		return c.body.defaultTo
	}
	value := lookupJSON(body, c.body.jsonPath)
	if value == "" {
		return c.body.defaultTo
	}
	if sub, ok := c.body.mapping[value]; ok {
		return sub
	}
	return c.body.defaultTo
}

// parseRestrictedJSONPath validates and tokenizes a JSONPath
// expression. Restricted grammar: `$.field` or `$.parent.child` —
// dot-separated identifiers only. No wildcards, filters, or array
// indexing — those are evaluation hazards on the hot path.
func parseRestrictedJSONPath(jp string) ([]string, error) {
	if !strings.HasPrefix(jp, "$.") {
		return nil, fmt.Errorf("jsonPath must start with '$.'; got %q", jp)
	}
	segs := strings.Split(jp[2:], ".")
	if len(segs) == 0 {
		return nil, fmt.Errorf("jsonPath has no segments")
	}
	for _, s := range segs {
		if s == "" {
			return nil, fmt.Errorf("empty segment in jsonPath %q", jp)
		}
		for _, r := range s {
			if !((r >= 'a' && r <= 'z') ||
				(r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') ||
				r == '_') {
				return nil, fmt.Errorf("illegal char %q in segment %q", r, s)
			}
		}
	}
	return segs, nil
}

// lookupJSON walks a parsed body for the value at jsonPath segs.
// Returns the string form of the value, or empty string when the
// path does not resolve to a JSON string.
func lookupJSON(body []byte, segs []string) string {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return ""
	}
	for _, seg := range segs {
		m, ok := v.(map[string]any)
		if !ok {
			return ""
		}
		v, ok = m[seg]
		if !ok {
			return ""
		}
	}
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	default:
		return ""
	}
}
