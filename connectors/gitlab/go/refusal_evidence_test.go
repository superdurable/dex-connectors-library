// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitlab

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
		{name: "documented refusal", status: http.StatusBadRequest, body: `{"message":"Invalid request"}`, want: true},
		{name: "empty body", status: http.StatusBadRequest},
		{name: "contradictory body", status: http.StatusBadRequest, body: `{"message":"Invalid request","id":1}`},
		{name: "conflict", status: http.StatusConflict, body: `{"message":"Invalid request"}`},
		{name: "other client error", status: http.StatusMethodNotAllowed, body: `{"message":"Invalid request"}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isDefiniteGitLabRefusal(testCase.status, []byte(testCase.body)); got != testCase.want {
				t.Fatalf("isDefiniteGitLabRefusal(%d, %q) = %t, want %t", testCase.status, testCase.body, got, testCase.want)
			}
		})
	}
}
