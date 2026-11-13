// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package istero

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

const testAPIKey = "private-test-token"

var testConnection = sdkgo.ConnectionRef{Provider: "istero", Name: "istero"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "istero-provider-test" }
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
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey), ISID: "60066023"}}
	client, err := New(Config{MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{destination: destination}}))
	require.NoError(t, err)
	return client
}
func TestGetAccountStatusRequestAndPrivacy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/resource/v1/isas/mine/information", request.URL.Path)
		require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
		require.Equal(t, "60066023", request.URL.Query().Get("isid"))
		_, err := writer.Write([]byte(`{"code":200,"message":"ok","data":{"isid":60066023,"today_used":3,"today_remaining":997,"nickname":"private"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetAccountStatus(), testConnection, GetAccountStatusInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountStatusBranchRetrieved, result.Branch)
	require.Equal(t, GetAccountStatusOutput{ISID: 60066023, TodayUsed: 3, TodayRemaining: 997}, result.Value)
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
		{"invalid key", 401, `{"code":403,"message":"bad key"}`, GetAccountStatusBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"frozen", 403, `{"code":502,"message":"frozen"}`, GetAccountStatusBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"rate limited", 429, `{"code":429,"message":"rate limit"}`, "", sdkgo.FailureRateLimit, true},
		{"temporary", 502, `{"code":500,"message":"temporary"}`, "", sdkgo.FailureAvailability, true},
		{"timeout", 504, `{"code":500,"message":"timeout"}`, "", sdkgo.FailureAvailability, true},
		{"unknown", 400, `{"code":400,"message":"bad request"}`, GetAccountStatusBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"unavailable with rejection code", 503, `{"code":403,"message":"bad key"}`, "", sdkgo.FailureAvailability, true},
		{"rate limited with rejection code", 429, `{"code":502,"message":"frozen"}`, "", sdkgo.FailureRateLimit, true},
		{"request timeout", 408, `{"code":403,"message":"bad key"}`, "", sdkgo.FailureAvailability, true},
		{"unavailable without JSON", 503, `<html>`, "", sdkgo.FailureAvailability, true},
		{"malformed", 200, `{`, GetAccountStatusBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing remaining", 200, `{"code":200,"data":{"isid":60066023,"today_used":3}}`, GetAccountStatusBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"wrong account", 200, `{"code":200,"data":{"isid":60066024,"today_used":3,"today_remaining":997}}`, GetAccountStatusBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
		_, err := writer.Write([]byte(`{"code":200,"data":{"isid":60066023,"today_used":3,"today_remaining":997}}`))
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
