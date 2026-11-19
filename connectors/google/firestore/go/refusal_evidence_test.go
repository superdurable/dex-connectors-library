// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package firestore

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
		{name: "documented refusal", status: http.StatusBadRequest, body: `{"error":{"code":400,"message":"Invalid request","status":"INVALID_ARGUMENT"}}`, want: true},
		{name: "empty body", status: http.StatusBadRequest},
		{name: "contradictory body", status: http.StatusBadRequest, body: `{"error":{"code":400,"message":"Invalid request","status":"INVALID_ARGUMENT"},"name":"projects/x"}`},
		{name: "conflict", status: http.StatusConflict, body: `{"error":{"code":400,"message":"Invalid request","status":"INVALID_ARGUMENT"}}`},
		{name: "other client error", status: http.StatusMethodNotAllowed, body: `{"error":{"code":400,"message":"Invalid request","status":"INVALID_ARGUMENT"}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isDefiniteFirestoreRefusal(testCase.status, []byte(testCase.body)); got != testCase.want {
				t.Fatalf("isDefiniteFirestoreRefusal(%d, %q) = %t, want %t", testCase.status, testCase.body, got, testCase.want)
			}
		})
	}
}
