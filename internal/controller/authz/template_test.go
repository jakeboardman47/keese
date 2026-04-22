// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"bytes"
	"testing"
)

// TestParseTemplate_AllowedFunctions verifies all six allowed Sprig functions.
func TestParseTemplate_AllowedFunctions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		tmpl     string
		data     any
		expected string
	}{
		{
			name:     "trimPrefix removes prefix",
			tmpl:     `{{ trimPrefix "service_account:" "service_account:default" }}`,
			data:     nil,
			expected: "default",
		},
		{
			name:     "trimSuffix removes suffix",
			tmpl:     `{{ trimSuffix "-v1" "myapp-v1" }}`,
			data:     nil,
			expected: "myapp",
		},
		{
			name:     "lower converts to lowercase",
			tmpl:     `{{ lower "ALICE" }}`,
			data:     nil,
			expected: "alice",
		},
		{
			name:     "upper converts to uppercase",
			tmpl:     `{{ upper "alice" }}`,
			data:     nil,
			expected: "ALICE",
		},
		{
			name:     "replace substitutes substring",
			tmpl:     `{{ replace "@" "+" "alice@example.com" }}`,
			data:     nil,
			expected: "alice+example.com",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tmpl, err := ParseTemplate(tc.name, tc.tmpl)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, tc.data); err != nil {
				t.Fatalf("unexpected execute error: %v", err)
			}
			if buf.String() != tc.expected {
				t.Errorf("got %q; want %q", buf.String(), tc.expected)
			}
		})
	}
}

// TestParseTemplate_SplitFunction verifies the split function (slice return value).
func TestParseTemplate_SplitFunction(t *testing.T) {
	t.Parallel()

	// split returns a []string; template range over it.
	tmpl, err := ParseTemplate("split-test", `{{ range split "," "a,b,c" }}[{{ . }}]{{ end }}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}
	got := buf.String()
	if got != "[a][b][c]" {
		t.Errorf("got %q; want %q", got, "[a][b][c]")
	}
}

// TestParseTemplate_DisallowedFunctions verifies the allow-list rejects unknown Sprig functions.
// Note: Go text/template built-ins (printf, print, println, len, index, etc.) remain
// available because they are part of the template engine itself and cannot be removed via
// FuncMap. The allow-list only gates Sprig extensions. If stricter enforcement is needed,
// an AST-walker pre-parse pass would be required.
// TODO(spec-followup): decide whether Go built-ins should also be blocked via AST analysis.
func TestParseTemplate_DisallowedFunctions(t *testing.T) {
	t.Parallel()

	disallowed := []struct {
		name string
		tmpl string
	}{
		{"env", `{{ env "HOME" }}`},
		{"exec", `{{ exec "ls" }}`},
		{"readFile", `{{ readFile "/etc/passwd" }}`},
		{"sprigDate", `{{ now | date "2006" }}`},
		{"toJson", `{{ toJson . }}`},
		{"b64enc", `{{ b64enc "secret" }}`},
		{"randAlphaNum", `{{ randAlphaNum 16 }}`},
		{"sha256sum", `{{ sha256sum "input" }}`},
	}

	for _, tc := range disallowed {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseTemplate(tc.name, tc.tmpl)
			if err == nil {
				t.Errorf("expected parse error for disallowed function %q but got none", tc.name)
			}
		})
	}
}

// TestValidateTemplates_SubjectAndAudience verifies the combined validator.
func TestValidateTemplates_SubjectAndAudience(t *testing.T) {
	t.Parallel()

	t.Run("valid subject and audience templates", func(t *testing.T) {
		t.Parallel()
		err := ValidateTemplates(
			`service_account:{{ .Claims.sub }}`,
			[]struct{ Name, Template string }{
				{Name: "egress", Template: `keese-egress-{{ lower .Claims.namespace }}`},
				{Name: "supervisor", Template: `keese-supervisor-{{ .Claims.uid }}`},
			},
		)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid subject template fails fast", func(t *testing.T) {
		t.Parallel()
		err := ValidateTemplates(
			`{{ unclosed`,
			[]struct{ Name, Template string }{
				{Name: "egress", Template: `keese-egress-{{ .Claims.ns }}`},
			},
		)
		if err == nil {
			t.Error("expected error for broken subjectTemplate but got none")
		}
	})

	t.Run("invalid audience template fails", func(t *testing.T) {
		t.Parallel()
		err := ValidateTemplates(
			`service_account:{{ .Claims.sub }}`,
			[]struct{ Name, Template string }{
				{Name: "egress", Template: `{{ .Claims.ns | env "X" }}`},
			},
		)
		if err == nil {
			t.Error("expected error for broken audience template but got none")
		}
	})
}

// TestTrimPrefix_NoMatch verifies trimPrefix returns the original string when prefix is absent.
func TestTrimPrefix_NoMatch(t *testing.T) {
	t.Parallel()
	tmpl, err := ParseTemplate("trim-no-match", `{{ trimPrefix "xyz" "abc" }}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if buf.String() != "abc" {
		t.Errorf("got %q; want %q", buf.String(), "abc")
	}
}

// TestReplace_MultipleOccurrences verifies replace substitutes all occurrences.
func TestReplace_MultipleOccurrences(t *testing.T) {
	t.Parallel()
	tmpl, err := ParseTemplate("replace-multi", `{{ replace "a" "X" "banana" }}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if buf.String() != "bXnXnX" {
		t.Errorf("got %q; want %q", buf.String(), "bXnXnX")
	}
}
