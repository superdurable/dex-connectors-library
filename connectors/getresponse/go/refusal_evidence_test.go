// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package getresponse

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
		{name: "documented refusal", status: http.StatusBadRequest, body: `{"codeDescription":"Invalid request","message":"Contact rejected"}`, want: true},
		{name: "empty body", status: http.StatusBadRequest},
		{name: "contradictory body", status: http.StatusBadRequest, body: `{"codeDescription":"Invalid request","message":"Contact rejected","contactId":"abc"}`},
		{name: "conflict", status: http.StatusConflict, body: `{"codeDescription":"Invalid request","message":"Contact rejected"}`},
		{name: "other client error", status: http.StatusMethodNotAllowed, body: `{"codeDescription":"Invalid request","message":"Contact rejected"}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isDefiniteGetResponseRefusal(testCase.status, []byte(testCase.body)); got != testCase.want {
				t.Fatalf("isDefiniteGetResponseRefusal(%d, %q) = %t, want %t", testCase.status, testCase.body, got, testCase.want)
			}
		})
	}
}
