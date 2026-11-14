// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pingpp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "sk_test_sampleSecret"

var testConnection = sdkgo.ConnectionRef{Provider: "pingpp", Name: "pingpp"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "pingpp-provider-test" }
func (queryContext) StepExecutionID() string     { return "charge-status" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	return client
}

func TestBasicAuthReadAndPrivateFieldsDropped(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/charges/ch_sample123", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		username, password, ok := request.BasicAuth()
		require.True(t, ok)
		require.Equal(t, testAPIKey, username)
		require.Empty(t, password)
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		_, err := writer.Write([]byte(`{"id":"ch_sample123","object":"charge","paid":true,"refunded":false,"reversed":false,"livemode":false,"client_ip":"private","order_no":"private","credential":{"secret":"private"},"metadata":{"secret":"private"},"transaction_no":"private"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetChargeStatus(), testConnection, GetChargeStatusInput{ChargeID: "ch_sample123"})
	require.NoError(t, err)
	require.Equal(t, GetChargeStatusBranchFetched, result.Branch)
	require.Equal(t, GetChargeStatusOutput{ChargeID: "ch_sample123", Paid: true}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}

func TestProviderOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"not found", 404, `{"error":"private"}`, GetChargeStatusBranchNotFound, false},
		{"invalid request", 400, `{"error":"private"}`, GetChargeStatusBranchProviderRejected, false},
		{"wrong key", 401, `{"error":"private"}`, GetChargeStatusBranchProviderRejected, false},
		{"business error", 402, `{"error":"private"}`, GetChargeStatusBranchProviderRejected, false},
		{"concurrency limit", 403, `{"error":"private"}`, "", true},
		{"rate limit", 429, `{"error":"private"}`, "", true},
		{"server error", 503, `{"error":"private"}`, "", true},
		{"malformed", 200, `{`, GetChargeStatusBranchInvalidResponse, false},
		{"missing flags", 200, `{"id":"ch_sample123","object":"charge","paid":false}`, GetChargeStatusBranchInvalidResponse, false},
		{"wrong object", 200, `{"id":"ch_sample123","object":"refund","paid":false,"refunded":false,"reversed":false,"livemode":false}`, GetChargeStatusBranchInvalidResponse, false},
		{"wrong id", 200, `{"id":"ch_other","object":"charge","paid":false,"refunded":false,"reversed":false,"livemode":false}`, GetChargeStatusBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetChargeStatus(), testConnection, GetChargeStatusInput{ChargeID: "ch_sample123"})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAPIKey)
				require.NotContains(t, err.Error(), "private")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "private")
		})
	}
}

func TestInvalidInputSendsNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { calls.Add(1); writer.WriteHeader(http.StatusOK) }))
	defer server.Close()
	for _, chargeID := range []string{"", "ch_", "bad", "ch_a/b", "ch_ bad", "ch_" + strings.Repeat("x", 129)} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetChargeStatus(), testConnection, GetChargeStatusInput{ChargeID: chargeID})
		require.NoError(t, err)
		require.Equal(t, GetChargeStatusBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}

func TestOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"id":"ch_sample123","object":"charge","paid":false,"refunded":false,"reversed":false,"livemode":false}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 4}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetChargeStatus(), testConnection, GetChargeStatusInput{ChargeID: "ch_sample123"})
	require.NoError(t, err)
	require.Equal(t, GetChargeStatusBranchInvalidResponse, result.Branch)
}
