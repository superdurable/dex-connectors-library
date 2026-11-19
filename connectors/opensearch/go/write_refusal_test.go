// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package opensearch

import (
	"net/http"
	"testing"
)

func TestWriteRefusalRequiresClearNonAcceptance(t *testing.T) {
	cases := []struct {
		name string
		status int
		body string
		want bool
	}{
		{"clear refusal", http.StatusBadRequest, `{"error":{"type":"validation_exception","reason":"invalid"}}`, true},
		{"conflict", http.StatusConflict, `{"error":{"type":"validation_exception","reason":"invalid"}}`, false},
		{"method not allowed", http.StatusMethodNotAllowed, `{"error":{"type":"validation_exception","reason":"invalid"}}`, false},
		{"empty refusal", http.StatusForbidden, "", false},
		{"contradictory refusal", http.StatusForbidden, `{"_id":"doc-1","error":{"type":"validation_exception"}}`, false},
		{"unparseable refusal", http.StatusForbidden, "{", false},
		{"server error", http.StatusInternalServerError, `{"error":{"type":"validation_exception","reason":"invalid"}}`, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isDefiniteRefusalStatus(testCase.status, []byte(testCase.body)); got != testCase.want {
				t.Fatalf("refusal classification = %t, want %t", got, testCase.want)
			}
		})
	}
}
