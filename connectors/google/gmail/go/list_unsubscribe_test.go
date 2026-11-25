// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import (
	"strings"
	"testing"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestValidateListUnsubscribe(t *testing.T) {
	cases := []struct {
		name       string
		targets    []string
		isOneClick bool
		isValid    bool
	}{
		{name: "none", isValid: true},
		{name: "https and mailto", targets: []string{"https://example.com/unsubscribe?token=abc", "mailto:news@example.com?subject=unsubscribe"}, isValid: true},
		{name: "one click with https", targets: []string{"https://example.com/u/1"}, isOneClick: true, isValid: true},
		{name: "one click without https", targets: []string{"mailto:news@example.com"}, isOneClick: true},
		{name: "http", targets: []string{"http://example.com/u"}},
		{name: "javascript", targets: []string{"javascript:alert(1)"}},
		{name: "CRLF header injection", targets: []string{"https://example.com/u\r\nBcc: victim@example.com"}},
		{name: "LF", targets: []string{"https://example.com/u\nX: y"}},
		{name: "closing bracket adds an entry", targets: []string{"https://example.com/u>, <https://evil.example"}},
		{name: "comma", targets: []string{"https://example.com/a,b"}},
		{name: "space", targets: []string{"https://example.com/a b"}},
		{name: "non-ASCII", targets: []string{"https://example.com/é"}},
		{name: "https without host", targets: []string{"https:///path"}},
		{name: "https with user info", targets: []string{"https://user:pass@example.com/u"}},
		{name: "mailto without address", targets: []string{"mailto:"}},
		{name: "mailto with invalid address", targets: []string{"mailto:not-an-address"}},
		{name: "empty", targets: []string{""}},
		{name: "duplicate", targets: []string{"https://example.com/u", "https://example.com/u"}},
		{name: "three targets", targets: []string{"https://example.com/1", "https://example.com/2", "mailto:a@example.com"}},
		{name: "upper-case scheme and port", targets: []string{"HTTPS://Example.com:8443/u"}, isValid: true},
		{name: "percent-encoded mailto", targets: []string{"mailto:news%40example.com"}, isValid: true},
		{name: "https without slashes", targets: []string{"https:example.com/u"}},
		{name: "https with bad port", targets: []string{"https://example.com:80a/u"}},
		{name: "https with underscore host", targets: []string{"https://exa_mple.com/u"}},
		{name: "backslash", targets: []string{"https://example.com\\evil.com/u"}},
		{name: "invalid percent escape", targets: []string{"mailto:%ZZ@example.com"}},
		{name: "mailto encoded newline", targets: []string{"mailto:a@example.com%0D%0ABcc:b@example.com"}},
		{name: "too long", targets: []string{"https://example.com/" + strings.Repeat("a", maxListUnsubscribeTargetLength)}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			failure := validateListUnsubscribe(testCase.targets, testCase.isOneClick)
			if testCase.isValid && failure != nil {
				t.Fatalf("validateListUnsubscribe() = %v, want nil", failure)
			}
			if !testCase.isValid && (failure == nil || failure.Kind != sdkgo.FailureValidation) {
				t.Fatalf("validateListUnsubscribe() = %v, want a validation failure", failure)
			}
		})
	}
}
