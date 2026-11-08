// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chargebee

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "chargebee", Name: "chargebee"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "chargebee-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-subscription" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sample-chargebee-api-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetSubscriptionReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/subscriptions/sub_123456", request.URL.Path)
		require.Equal(t, "Basic c2FtcGxlLWNoYXJnZWJlZS1hcGktdG9rZW46", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"subscription":{"id":"sub_123456","status":"active","currency_code":"USD","customer_id":"private"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetSubscription(), testConnection, GetSubscriptionInput{SubscriptionID: "sub_123456"})
	require.NoError(t, err)
	require.Equal(t, GetSubscriptionBranchFound, result.Branch)
	require.Equal(t, SubscriptionStatus{ID: "sub_123456", Status: "active", CurrencyCode: "USD"}, result.Value)
}

func TestGetSubscriptionClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetSubscriptionBranchNotFound},
		{name: "forbidden", status: 403, branch: GetSubscriptionBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"subscription":{"id":"other","status":"active","currency_code":"USD"}}`, branch: GetSubscriptionBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetSubscription(), testConnection, GetSubscriptionInput{SubscriptionID: "sub_123456"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestGetSubscriptionRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://acme-test.chargebee.com/api/v2", 0)
	for _, subscriptionID := range []string{"", "../other", "bad\nsubscription", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetSubscription(), testConnection, GetSubscriptionInput{SubscriptionID: subscriptionID})
		require.NoError(t, err)
		require.Equal(t, GetSubscriptionBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"subscription":{"id":"sub_123456","status":"active","currency_code":"USD"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 20).GetSubscription(), testConnection, GetSubscriptionInput{SubscriptionID: "sub_123456"})
	require.NoError(t, err)
	require.Equal(t, GetSubscriptionBranchInvalidResponse, result.Branch)
}
