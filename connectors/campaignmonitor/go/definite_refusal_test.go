// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaignmonitor

import (
	"net/http"
	"testing"
)

func TestDefiniteRefusalRequiresParsedProviderError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"documented refusal", http.StatusBadRequest, `{"error":"invalid request"}`, true},
		{"empty body", http.StatusBadRequest, "", false},
		{"empty object", http.StatusUnprocessableEntity, "{}", false},
		{"contradictory acceptance", http.StatusBadRequest, `{"success":true,"error":"invalid request"}`, false},
		{"contradictory data", http.StatusBadRequest, `{"error":"invalid request","data":{"id":"created"}}`, false},
		{"spaced acceptance", http.StatusBadRequest, `{"success": true, "error":"invalid request"}`, false},
		{"malformed body", http.StatusBadRequest, "not-json", false},
		{"conflict", http.StatusConflict, `{"error":"conflict"}`, false},
		{"other client error", http.StatusMethodNotAllowed, `{"error":"invalid request"}`, false},
		{"server error", http.StatusInternalServerError, `{"error":"server failure"}`, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			if got := isDefiniteRefusalStatus(scenario.status, []byte(scenario.body)); got != scenario.want {
				t.Fatalf("isDefiniteRefusalStatus(%d, %q) = %t, want %t", scenario.status, scenario.body, got, scenario.want)
			}
		})
	}
}
