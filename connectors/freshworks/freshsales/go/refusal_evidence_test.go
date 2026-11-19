// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshsales

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
		{name: "documented refusal", status: http.StatusBadRequest, body: `{"errors":{"code":"400","message":"Invalid request"}}`, want: true},
		{name: "empty body", status: http.StatusBadRequest},
		{name: "contradictory body", status: http.StatusBadRequest, body: `{"errors":{"code":"400","message":"Invalid request"},"id":1}`},
		{name: "conflict", status: http.StatusConflict, body: `{"errors":{"code":"400","message":"Invalid request"}}`},
		{name: "other client error", status: http.StatusMethodNotAllowed, body: `{"errors":{"code":"400","message":"Invalid request"}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isDefiniteFreshsalesRefusal(testCase.status, []byte(testCase.body)); got != testCase.want {
				t.Fatalf("isDefiniteFreshsalesRefusal(%d, %q) = %t, want %t", testCase.status, testCase.body, got, testCase.want)
			}
		})
	}
}
