// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDefiniteRefusalRequiresBitbucketError(t *testing.T) {
	cases := []struct {
		name string
		status int
		body string
		want bool
	}{
		{"rejection", http.StatusBadRequest, `{"type":"error","error":{"message":"invalid request"}}`, true},
		{"empty body", http.StatusBadRequest, "", false},
		{"contradictory success", http.StatusBadRequest, `{"type":"repository","error":{"message":"invalid request"}}`, false},
		{"conflict", http.StatusConflict, `{"type":"error","error":{"message":"conflict"}}`, false},
		{"other client error", http.StatusMethodNotAllowed, `{"type":"error","error":{"message":"unsupported"}}`, false},
		{"server error", http.StatusInternalServerError, `{"type":"error","error":{"message":"failure"}}`, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			response := &http.Response{StatusCode: scenario.status, Body: io.NopCloser(strings.NewReader(scenario.body))}
			if got := isDefiniteRefusalResponse(response, 1024); got != scenario.want {
				t.Fatalf("isDefiniteRefusalResponse(%d, %q) = %t, want %t", scenario.status, scenario.body, got, scenario.want)
			}
		})
	}
}
