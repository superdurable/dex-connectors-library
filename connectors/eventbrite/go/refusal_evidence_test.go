// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventbrite

import (
	"net/http"
	"testing"
)

func TestLimitedWriteRefusalRequiresProviderEvidence(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{name: "documented refusal", status: http.StatusBadRequest, body: `{"error":"BAD_REQUEST","error_description":"Invalid event","status_code":400}`, want: true},
		{name: "empty body", status: http.StatusBadRequest},
		{name: "contradictory body", status: http.StatusBadRequest, body: `{"error":"BAD_REQUEST","error_description":"Invalid event","status_code":400,"id":"123"}`},
		{name: "conflict", status: http.StatusConflict, body: `{"error":"BAD_REQUEST","error_description":"Invalid event","status_code":400}`},
		{name: "other client error", status: http.StatusMethodNotAllowed, body: `{"error":"BAD_REQUEST","error_description":"Invalid event","status_code":400}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isDefiniteEventbriteRefusal(testCase.status, []byte(testCase.body)); got != testCase.want {
				t.Fatalf("isDefiniteEventbriteRefusal(%d, %q) = %t, want %t", testCase.status, testCase.body, got, testCase.want)
			}
		})
	}
}
