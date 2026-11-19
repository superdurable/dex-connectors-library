// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contacts

import (
	"net/http"
	"testing"
)

func TestContactWriteRefusalRequiresProviderEvidence(t *testing.T) {
	valid := []byte(`{"error":{"code":403,"message":"Permission denied","status":"PERMISSION_DENIED"}}`)
	if !isDefiniteContactsWriteRefusal(http.StatusForbidden, valid) {
		t.Fatal("provider error proves refusal")
	}
	for _, testCase := range []struct {
		status int
		body   string
	}{
		{http.StatusForbidden, ""},
		{http.StatusForbidden, `{"error":{"code":403,"message":"Denied","status":"PERMISSION_DENIED"},"resourceName":"people/c123"}`},
		{http.StatusConflict, string(valid)},
		{http.StatusMethodNotAllowed, string(valid)},
	} {
		if isDefiniteContactsWriteRefusal(testCase.status, []byte(testCase.body)) {
			t.Fatalf("HTTP %d with %q cannot prove refusal", testCase.status, testCase.body)
		}
	}
}
