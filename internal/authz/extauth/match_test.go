// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package extauth_test

import (
	"testing"

	authzv1alpha1 "github.com/keese-ai/keese/api/authz/v1alpha1"
	"github.com/keese-ai/keese/internal/authz/extauth"
)

func TestCompileMatch_BadJSONPath(t *testing.T) {
	t.Parallel()
	cases := []string{
		"$.foo[*]",  // wildcard
		"$.foo.bar.", // trailing dot
		"foo.bar",   // no $.
		"$..deep",   // recursive descent
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, err := extauth.CompileMatch(authzv1alpha1.HTTPRouteMatch{
				Paths: []authzv1alpha1.HTTPPathMatch{{Type: authzv1alpha1.PathMatchExact, Value: "/x"}},
			}, &authzv1alpha1.BodyDiscriminator{
				JSONPath: c, Map: map[string]string{"a": "b"},
			})
			if err == nil {
				t.Fatalf("expected compile error for jsonPath %q", c)
			}
		})
	}
}

func TestMatch_Path(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mtype   authzv1alpha1.PathMatchType
		mvalue  string
		path    string
		want    bool
	}{
		{"exact-hit", authzv1alpha1.PathMatchExact, "/foo", "/foo", true},
		{"exact-miss-trailing", authzv1alpha1.PathMatchExact, "/foo", "/foo/", false},
		{"prefix-hit", authzv1alpha1.PathMatchPathPrefix, "/foo", "/foo/bar", true},
		{"prefix-no-match", authzv1alpha1.PathMatchPathPrefix, "/foo", "/bar/foo", false},
		{"regex-hit", authzv1alpha1.PathMatchRegularExpression, `^/v\d+/.*$`, "/v1/x", true},
		{"regex-miss", authzv1alpha1.PathMatchRegularExpression, `^/v\d+/.*$`, "/api/x", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cm, err := extauth.CompileMatch(authzv1alpha1.HTTPRouteMatch{
				Paths: []authzv1alpha1.HTTPPathMatch{{Type: c.mtype, Value: c.mvalue}},
			}, nil)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			got := cm.Match(&extauth.HTTPRequest{Path: c.path}).Matched
			if got != c.want {
				t.Fatalf("got matched=%v want %v", got, c.want)
			}
		})
	}
}

func TestMatch_MethodAndHeaders(t *testing.T) {
	t.Parallel()
	cm, err := extauth.CompileMatch(authzv1alpha1.HTTPRouteMatch{
		Paths:   []authzv1alpha1.HTTPPathMatch{{Type: authzv1alpha1.PathMatchExact, Value: "/m"}},
		Methods: []authzv1alpha1.HTTPMethod{"POST"},
		Headers: []authzv1alpha1.HTTPHeaderMatch{
			{Name: "X-Model", Type: authzv1alpha1.HeaderMatchExact, Value: "opus"},
			{Name: "Content-Type", Type: authzv1alpha1.HeaderMatchRegularExpression, Value: `^application/json`},
		},
	}, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cases := []struct {
		name string
		req  extauth.HTTPRequest
		want bool
	}{
		{
			name: "all-pass",
			req: extauth.HTTPRequest{
				Path: "/m", Method: "POST",
				Headers: map[string]string{"x-model": "opus", "content-type": "application/json; charset=utf-8"},
			},
			want: true,
		},
		{
			name: "wrong-method",
			req: extauth.HTTPRequest{
				Path: "/m", Method: "GET",
				Headers: map[string]string{"x-model": "opus", "content-type": "application/json"},
			},
			want: false,
		},
		{
			name: "missing-header",
			req: extauth.HTTPRequest{
				Path: "/m", Method: "POST",
				Headers: map[string]string{"x-model": "opus"},
			},
			want: false,
		},
		{
			name: "header-regex-fail",
			req: extauth.HTTPRequest{
				Path: "/m", Method: "POST",
				Headers: map[string]string{"x-model": "opus", "content-type": "text/plain"},
			},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cm.Match(&c.req).Matched; got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestMatch_BodyDiscriminator(t *testing.T) {
	t.Parallel()
	cm, err := extauth.CompileMatch(authzv1alpha1.HTTPRouteMatch{
		Paths: []authzv1alpha1.HTTPPathMatch{{Type: authzv1alpha1.PathMatchExact, Value: "/v1/messages"}},
	}, &authzv1alpha1.BodyDiscriminator{
		JSONPath: "$.model",
		Map: map[string]string{
			"claude-opus-4-7":   "opus-4",
			"claude-haiku-4-5":  "haiku-4",
		},
		Default: "",
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cases := []struct {
		name string
		body string
		want string
	}{
		{"opus-hit", `{"model":"claude-opus-4-7"}`, "opus-4"},
		{"haiku-hit", `{"model":"claude-haiku-4-5","x":1}`, "haiku-4"},
		{"unknown-default", `{"model":"claude-experimental"}`, ""},
		{"missing-default", `{"foo":"bar"}`, ""},
		{"empty-body-default", "", ""},
		{"malformed-default", "not-json", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := cm.Match(&extauth.HTTPRequest{Path: "/v1/messages", Body: []byte(c.body)})
			if !r.Matched {
				t.Fatalf("expected match")
			}
			if r.SubTool != c.want {
				t.Fatalf("subTool: got %q want %q", r.SubTool, c.want)
			}
		})
	}
}

func TestMatch_BodyDiscriminator_NestedJSONPath(t *testing.T) {
	t.Parallel()
	cm, err := extauth.CompileMatch(authzv1alpha1.HTTPRouteMatch{
		Paths: []authzv1alpha1.HTTPPathMatch{{Type: authzv1alpha1.PathMatchExact, Value: "/x"}},
	}, &authzv1alpha1.BodyDiscriminator{
		JSONPath: "$.config.model",
		Map:      map[string]string{"opus": "o"},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	r := cm.Match(&extauth.HTTPRequest{Path: "/x", Body: []byte(`{"config":{"model":"opus"}}`)})
	if !r.Matched || r.SubTool != "o" {
		t.Fatalf("got %+v want matched=true sub=o", r)
	}
}

func TestMatch_QueryParams(t *testing.T) {
	t.Parallel()
	cm, err := extauth.CompileMatch(authzv1alpha1.HTTPRouteMatch{
		Paths: []authzv1alpha1.HTTPPathMatch{{Type: authzv1alpha1.PathMatchExact, Value: "/q"}},
		QueryParams: []authzv1alpha1.HTTPQueryParamMatch{
			{Name: "stream", Type: authzv1alpha1.HeaderMatchExact, Value: "true"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cases := []struct {
		params map[string]string
		want   bool
	}{
		{map[string]string{"stream": "true"}, true},
		{map[string]string{"stream": "false"}, false},
		{map[string]string{}, false},
	}
	for i, c := range cases {
		got := cm.Match(&extauth.HTTPRequest{Path: "/q", QueryParams: c.params}).Matched
		if got != c.want {
			t.Fatalf("case %d: got %v want %v (params=%v)", i, got, c.want, c.params)
		}
	}
}
