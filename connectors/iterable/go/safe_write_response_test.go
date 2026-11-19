// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package iterable

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestSafeWriteRetriesUnconfirmedSuccessBody(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body string
	}{
		{"malformed", "{}"},
		{"oversized", strings.Repeat("x", 1024)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				requests++
				writer.WriteHeader(http.StatusOK)
				if _, err := writer.Write([]byte(testCase.body)); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 64}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("sample-iterable-api-key")}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = sdkgo.RunMutation(queryContext{}, client.UpsertUser(), testConnection, UpsertUserInput{Email: "user@example.test"})
			var retry *sdkgo.RetryError
			if !errors.As(err, &retry) {
				t.Fatalf("expected Retry for unconfirmed write, got %v", err)
			}
			if requests != 1 {
				t.Fatalf("sent %d requests, want one", requests)
			}
		})
	}
}
