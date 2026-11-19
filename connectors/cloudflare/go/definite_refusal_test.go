// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudflare

import (
	"net/http"
	"testing"
)

func TestDefiniteRefusalRequiresCloudflareRejection(t *testing.T) {
	cases := []struct {
		name string
		status int
		body string
		want bool
	}{
		{"rejection", http.StatusBadRequest, `{"success":false,"errors":[{"code":1000}]}`, true},
		{"empty body", http.StatusBadRequest, "", false},
		{"contradictory acceptance", http.StatusBadRequest, `{"success":true,"errors":[{"code":1000}]}`, false},
		{"conflict", http.StatusConflict, `{"success":false}`, false},
		{"other client error", http.StatusMethodNotAllowed, `{"success":false}`, false},
		{"server error", http.StatusInternalServerError, `{"success":false}`, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			if got := isDefiniteRefusalStatus(scenario.status, []byte(scenario.body)); got != scenario.want {
				t.Fatalf("isDefiniteRefusalStatus(%d, %q) = %t, want %t", scenario.status, scenario.body, got, scenario.want)
			}
		})
	}
}
