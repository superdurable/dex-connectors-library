// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tianxiagongchang

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "sk-tx-live-test-secret"

var testConnection = sdkgo.ConnectionRef{Provider: "tianxiagongchang", Name: "tianxiagongchang"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "tianxiagongchang-provider-test" }
func (queryContext) StepExecutionID() string     { return "account-status" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type redirectTransport struct{ destination *url.URL }

func (transport redirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.URL.Scheme = transport.destination.Scheme
	copy.URL.Host = transport.destination.Host
	return http.DefaultTransport.RoundTrip(copy)
}
func newTestClient(t *testing.T, serverURL string, maxResponseBytes int64) *Client {
	t.Helper()
	destination, err := url.Parse(serverURL)
	require.NoError(t, err)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	client, err := New(Config{MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{destination: destination}}))
	require.NoError(t, err)
	return client
}
func TestGetAccountStatusRequestAndPrivacy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/open/v1/account", request.URL.Path)
		require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
		require.Empty(t, request.URL.RawQuery)
		_, err := writer.Write([]byte(`{"code":0,"message":"ok","request_id":"private","credits_charged":0,"credits_balance":880,"data":{"app_id":1,"frozen_credits":2,"is_sandbox":false,"email":"private"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetAccountStatus(), testConnection, GetAccountStatusInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountStatusBranchRetrieved, result.Branch)
	require.Equal(t, GetAccountStatusOutput{AppID: 1, CreditsBalance: 880, FrozenCredits: 2, IsSandbox: false}, result.Value)
}
func TestGetAccountStatusBusinessCodes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"invalid key", 401, `{"code":40100,"message":"bad key"}`, GetAccountStatusBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"frozen", 403, `{"code":42901,"message":"frozen"}`, GetAccountStatusBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"rate limited", 429, `{"code":42900,"message":"rate limit"}`, "", sdkgo.FailureRateLimit, true},
		{"temporary", 502, `{"code":50000,"message":"temporary"}`, "", sdkgo.FailureAvailability, true},
		{"timeout", 504, `{"code":50400,"message":"timeout"}`, "", sdkgo.FailureAvailability, true},
		{"unknown", 400, `{"code":40000,"message":"bad request"}`, GetAccountStatusBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"malformed", 200, `{`, GetAccountStatusBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing balance", 200, `{"code":0,"credits_charged":0,"data":{"app_id":1,"frozen_credits":0,"is_sandbox":false}}`, GetAccountStatusBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"charged free query", 200, `{"code":0,"credits_charged":1,"credits_balance":880,"data":{"app_id":1,"frozen_credits":0,"is_sandbox":false}}`, GetAccountStatusBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetAccountStatus(), testConnection, GetAccountStatusInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testAPIKey)
			}
		})
	}
}
func TestGetAccountStatusBoundAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":0,"credits_charged":0,"credits_balance":880,"data":{"app_id":1,"frozen_credits":0,"is_sandbox":false}}`))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 8).GetAccountStatus(), testConnection, GetAccountStatusInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountStatusBranchInvalidResponse, result.Branch)
	client := newTestClient(t, server.URL, 0)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, client.GetAccountStatus(), testConnection, GetAccountStatusInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAPIKey)
}
