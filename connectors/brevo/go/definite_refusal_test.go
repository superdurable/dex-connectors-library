// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package brevo

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
		{"rejection", http.StatusBadRequest, `{"code":"invalid_parameter","message":"invalid request"}`, true},
		{"empty body", http.StatusBadRequest, "", false},
		{"contradictory acceptance", http.StatusBadRequest, `{"success":true,"error":"invalid request"}`, false},
		{"contradictory data", http.StatusBadRequest, `{"error":"invalid request","data":{"id":1}}`, false},
		{"conflict", http.StatusConflict, `{"code":"conflict"}`, false},
		{"other client error", http.StatusMethodNotAllowed, `{"code":"unsupported"}`, false},
		{"server error", http.StatusInternalServerError, `{"code":"failure"}`, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			if got := isDefiniteRefusalStatus(scenario.status, []byte(scenario.body)); got != scenario.want {
				t.Fatalf("isDefiniteRefusalStatus(%d, %q) = %t, want %t", scenario.status, scenario.body, got, scenario.want)
			}
		})
	}
}
