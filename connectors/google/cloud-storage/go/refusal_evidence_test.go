// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudstorage

import (
	"net/http"
	"testing"
)

func TestBucketCreationRefusalRequiresProviderEvidence(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{name: "documented refusal", status: http.StatusBadRequest, body: `{"error":{"code":400,"message":"Invalid bucket"}}`, want: true},
		{name: "empty body", status: http.StatusBadRequest},
		{name: "contradictory body", status: http.StatusBadRequest, body: `{"error":{"code":400,"message":"Invalid bucket"},"id":"bucket-1"}`},
		{name: "conflict", status: http.StatusConflict, body: `{"error":{"code":409,"message":"Bucket exists"}}`},
		{name: "other client error", status: http.StatusMethodNotAllowed, body: `{"error":{"code":405,"message":"Unknown"}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isDefiniteGoogleStorageRefusal(testCase.status, []byte(testCase.body)); got != testCase.want {
				t.Fatalf("refusal classification = %t, want %t", got, testCase.want)
			}
		})
	}
	if !isDefiniteBucketNameConflict([]byte(`{"error":{"code":409,"message":"Bucket exists","errors":[{"reason":"conflict"}]}}`)) {
		t.Fatal("documented bucket name conflict must be definite")
	}
	if isDefiniteBucketNameConflict([]byte(`{"error":{"code":409,"message":"Bucket exists"}}`)) {
		t.Fatal("undocumented conflict must remain unknown")
	}
}
