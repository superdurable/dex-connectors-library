// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kit

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
			client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 64}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = sdkgo.RunMutation(queryContext{}, client.CreateTag(), testConnection, CreateTagInput{Name: "Example"})
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

func TestSafeDeletesRetryUnconfirmedResponses(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(*Client) error
	}{
		{"deleteCustomField", func(client *Client) error {
			_, err := sdkgo.RunMutation(queryContext{}, client.DeleteCustomField(), testConnection, CustomFieldIDInput{CustomFieldID: 8})
			return err
		}},
		{"removeTagFromSubscriber", func(client *Client) error {
			_, err := sdkgo.RunMutation(queryContext{}, client.RemoveTagFromSubscriber(), testConnection, TagSubscriberInput{TagID: 12, SubscriberID: 204})
			return err
		}},
	} {
		for _, status := range []int{http.StatusOK, http.StatusAccepted, http.StatusFound} {
			t.Run(operation.name+"/"+http.StatusText(status), func(t *testing.T) {
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					requests++
					writer.WriteHeader(status)
				}))
				defer server.Close()
				client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
				if err != nil {
					t.Fatal(err)
				}
				var retry *sdkgo.RetryError
				if err := operation.run(client); !errors.As(err, &retry) {
					t.Fatalf("expected Retry for unconfirmed deletion, got %v", err)
				}
				if requests != 1 {
					t.Fatalf("sent %d requests, want one", requests)
				}
			})
		}
	}
}
