// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paypal

import (
	"net/http"
	"testing"
)

func TestWriteRefusalRequiresClearNonAcceptance(t *testing.T) {
	cases := []struct {
		name string
		status int
		body string
		want bool
	}{
		{"clear refusal", http.StatusBadRequest, `{"name":"VALIDATION_ERROR","message":"invalid"}`, true},
		{"conflict", http.StatusConflict, `{"name":"VALIDATION_ERROR","message":"invalid"}`, false},
		{"method not allowed", http.StatusMethodNotAllowed, `{"name":"VALIDATION_ERROR","message":"invalid"}`, false},
		{"empty refusal", http.StatusForbidden, "", false},
		{"contradictory refusal", http.StatusForbidden, `{"name":"VALIDATION_ERROR","message":"invalid","batch_header":{"payout_batch_id":"B1"}}`, false},
		{"unparseable refusal", http.StatusForbidden, "{", false},
		{"server error", http.StatusInternalServerError, `{"name":"VALIDATION_ERROR","message":"invalid"}`, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isDefinitePayPalRefusal(testCase.status, []byte(testCase.body)); got != testCase.want {
				t.Fatalf("refusal classification = %t, want %t", got, testCase.want)
			}
		})
	}
}
