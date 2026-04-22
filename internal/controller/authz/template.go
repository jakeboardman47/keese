// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package authz

import (
	"fmt"
	"text/template"
)

// allowedSprigFuncs is the restricted function set permitted in OIDCProvider templates.
// Any template referencing a function outside this set is rejected at admission and at
// reconcile time. See OIDCProviderSpec comment and rule 05.3.
var allowedSprigFuncs = map[string]any{
	"trimPrefix": func(prefix, s string) string {
		if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			return s[len(prefix):]
		}
		return s
	},
	"trimSuffix": func(suffix, s string) string {
		if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
			return s[:len(s)-len(suffix)]
		}
		return s
	},
	"lower": func(s string) string {
		result := make([]byte, len(s))
		for i := range s {
			c := s[i]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			result[i] = c
		}
		return string(result)
	},
	"upper": func(s string) string {
		result := make([]byte, len(s))
		for i := range s {
			c := s[i]
			if c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
			result[i] = c
		}
		return string(result)
	},
	"split": func(sep, s string) []string {
		// Returns a slice split by sep; mirrors sprig split (sep first, string second).
		var parts []string
		start := 0
		for i := 0; i <= len(s)-len(sep); i++ {
			if s[i:i+len(sep)] == sep {
				parts = append(parts, s[start:i])
				start = i + len(sep)
				i += len(sep) - 1
			}
		}
		parts = append(parts, s[start:])
		return parts
	},
	"replace": func(old, new, s string) string {
		// Mirrors sprig replace(old, new, src).
		if old == "" {
			return s
		}
		result := []byte{}
		for len(s) > 0 {
			if len(s) >= len(old) && s[:len(old)] == old {
				result = append(result, []byte(new)...)
				s = s[len(old):]
			} else {
				result = append(result, s[0])
				s = s[1:]
			}
		}
		return string(result)
	},
}

// ParseTemplate parses a Go template string with the restricted Sprig allow-list.
// Returns a parsed *template.Template or an error describing the failure.
// This is the single source of truth for allow-list enforcement in the controller.
func ParseTemplate(name, text string) (*template.Template, error) {
	t, err := template.New(name).Funcs(template.FuncMap(allowedSprigFuncs)).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("template %q parse error: %w", name, err)
	}
	return t, nil
}

// ValidateTemplates validates a subject template and a slice of (name, template-text) pairs.
// Returns a non-nil error at the first failure, naming the offending template.
func ValidateTemplates(subjectTemplate string, audienceTemplates []struct{ Name, Template string }) error {
	if _, err := ParseTemplate("subjectTemplate", subjectTemplate); err != nil {
		return fmt.Errorf("subjectTemplate: %w", err)
	}
	for _, at := range audienceTemplates {
		if _, err := ParseTemplate("audienceTemplate/"+at.Name, at.Template); err != nil {
			return fmt.Errorf("audienceTemplates[%s]: %w", at.Name, err)
		}
	}
	return nil
}
