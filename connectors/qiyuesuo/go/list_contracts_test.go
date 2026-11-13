// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qiyuesuo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAppToken = "app-token"
const testAppSecret = "app-secret"

var testConnection = sdkgo.ConnectionRef{Provider: "qiyuesuo", Name: "qiyuesuo"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "qiyuesuo-provider-test" }
func (queryContext) StepExecutionID() string     { return "contract-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type redirectTransport struct{ serverURL *url.URL }

func (transport redirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.URL.Scheme = transport.serverURL.Scheme
	cloned.URL.Host = transport.serverURL.Host
	return http.DefaultTransport.RoundTrip(cloned)
}
func newTestClient(t *testing.T, server *httptest.Server, maxResponseBytes int64) *Client {
	t.Helper()
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppToken: sdkgo.NewSecretString(testAppToken), AppSecret: sdkgo.NewSecretString(testAppSecret)}}
	client, err := New(Config{BaseURL: "https://openapi.qiyuesuo.com", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}
func TestSignRequestFixedVector(t *testing.T) {
	require.Equal(t, "b838c094b4e275f5f92d75e0167972d1", signRequest("app-token", "app-secret", "1565753984643", "254775d7-3c7c-4e9d-9880-1d4c5fb3dbd0"))
}
func TestListContractsRequestAndPrivacy(t *testing.T) {
	seenNonces := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, contractsPath, request.URL.Path)
		require.Equal(t, "5", request.URL.Query().Get("selectOffset"))
		require.Equal(t, "10", request.URL.Query().Get("selectLimit"))
		require.Equal(t, testAppToken, request.Header.Get("x-qys-open-accesstoken"))
		timestamp := request.Header.Get("x-qys-open-timestamp")
		unixMillis, err := strconv.ParseInt(timestamp, 10, 64)
		require.NoError(t, err)
		require.WithinDuration(t, time.Now(), time.UnixMilli(unixMillis), time.Minute)
		nonce := request.Header.Get("x-qys-open-nonce")
		require.Regexp(t, regexp.MustCompile("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"), nonce)
		require.False(t, seenNonces[nonce])
		seenNonces[nonce] = true
		require.Equal(t, signRequest(testAppToken, testAppSecret, timestamp, nonce), request.Header.Get("x-qys-open-signature"))
		require.NotContains(t, request.URL.String(), testAppSecret)
		_, err = writer.Write([]byte(`{"responseCode":"00000000","result":{"totalCount":20,"list":[{"id":2966583458579853505,"status":"SIGNING","subject":"private contract","tenantName":"private person"}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server, 0)
	for range 2 {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListContracts(), testConnection, ListContractsInput{Offset: 5, Limit: 10})
		require.NoError(t, err)
		require.Equal(t, ListContractsBranchListed, result.Branch)
		require.Equal(t, ListContractsOutput{Contracts: []ContractSummary{{ID: "2966583458579853505", Status: "SIGNING"}}, TotalCount: 20, NextOffset: 6}, result.Value)
	}
	require.Len(t, seenNonces, 2)
}
func TestListContractsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"http unauthorized", 442, "secret", ListContractsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"http forbidden", 449, "secret", ListContractsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"http invalid", 400, "secret", ListContractsBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"http rate", 429, "secret", "", sdkgo.FailureRateLimit, true},
		{"http unavailable", 503, "secret", "", sdkgo.FailureAvailability, true},
		{"app auth", 200, `{"responseCode":"11990442"}`, ListContractsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"app permission", 200, `{"responseCode":"14990002"}`, ListContractsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"app rate", 200, `{"responseCode":"11990006"}`, "", sdkgo.FailureRateLimit, true},
		{"app unavailable", 200, `{"responseCode":"21990001"}`, "", sdkgo.FailureAvailability, true},
		{"app invalid", 200, `{"responseCode":"11990005"}`, ListContractsBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"malformed", 200, "{", ListContractsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"no result", 200, `{"responseCode":"00000000"}`, ListContractsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"bad status", 200, `{"responseCode":"00000000","result":{"totalCount":1,"list":[{"id":1,"status":"UNKNOWN"}]}}`, ListContractsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"bad id", 200, `{"responseCode":"00000000","result":{"totalCount":1,"list":[{"id":0,"status":"SIGNING"}]}}`, ListContractsBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListContracts(), testConnection, ListContractsInput{})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				if tc.status == 429 || tc.status == 503 {
					var delayed *dex.RetryAfterError
					require.ErrorAs(t, err, &delayed)
					require.Equal(t, 2*time.Second, delayed.After)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testAppSecret)
			}
		})
	}
}
func TestListContractsSizeValidationTransportAndURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"responseCode":"00000000","result":{"totalCount":0,"list":[]}}`))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 8).ListContracts(), testConnection, ListContractsInput{})
	require.NoError(t, err)
	require.Equal(t, ListContractsBranchInvalidResponse, result.Branch)
	for _, input := range []ListContractsInput{{Offset: -1}, {Limit: 101}, {Offset: 1000001}} {
		result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListContracts(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListContractsBranchDefect, result.Branch)
	}
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListContracts(), testConnection, ListContractsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppToken: sdkgo.NewSecretString(testAppToken), AppSecret: sdkgo.NewSecretString(testAppSecret)}}
	for _, baseURL := range []string{"http://openapi.qiyuesuo.com", "https://example.com", "https://openapi.qiyuesuo.com/other"} {
		_, err = New(Config{BaseURL: baseURL}, credentials)
		require.Error(t, err)
	}
}
