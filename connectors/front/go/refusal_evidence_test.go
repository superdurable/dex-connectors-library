// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front

import (
	"net/http"
	"testing"
)

func TestNewMessageRefusalRequiresProviderEvidence(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{name: "documented refusal", status: http.StatusForbidden, body: `{"_error":{"status":403,"title":"Forbidden","message":"Send denied"}}`, want: true},
		{name: "empty body", status: http.StatusForbidden},
		{name: "contradictory body", status: http.StatusForbidden, body: `{"_error":{"status":403,"title":"Forbidden","message":"Send denied"},"uid":"msg_123"}`},
		{name: "conflict", status: http.StatusConflict, body: `{"_error":{"status":409,"title":"Conflict","message":"Unknown"}}`},
		{name: "other client error", status: http.StatusMethodNotAllowed, body: `{"_error":{"status":405,"title":"Invalid","message":"Unknown"}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := isDefiniteFrontMessageRefusal(frontResponse{statusCode: testCase.status, body: []byte(testCase.body)})
			if got != testCase.want {
				t.Fatalf("refusal classification = %t, want %t", got, testCase.want)
			}
		})
	}
}
