// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package caoliao

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testKey = "caoliao-test-secret"

var testConnection = sdkgo.ConnectionRef{Provider: "caoliao", Name: "caoliao"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "caoliao-provider-test" }
func (queryContext) StepExecutionID() string     { return "qrcode-list" }
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
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testKey)}}
	client, err := New(Config{BaseURL: "https://open.cli.im", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}
func TestListQRCodesRequestResponsePaginationAndPrivacy(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, qrCodesPath, request.URL.Path)
		require.Equal(t, "Bearer "+testKey, request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, float64(2), body["page_size"])
		require.Equal(t, "next-1", body["page_token"])
		require.NotContains(t, request.URL.String(), testKey)
		_, err := writer.Write([]byte(`{"code":0,"message":"ok","data":{"list":[{"name":"巡逻点-1","coding":"qk3mJHJ","private":"omitted"}],"next_page_token":"next-2","total":3334}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListQRCodes(), testConnection, ListQRCodesInput{PageSize: 2, PageToken: "next-1"})
	require.NoError(t, err)
	require.Equal(t, ListQRCodesBranchListed, result.Branch)
	require.Equal(t, ListQRCodesOutput{QRCodes: []QRCodeSummary{{Name: "巡逻点-1", Coding: "qk3mJHJ"}}, NextPageToken: "next-2", Total: 3334}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "omitted")
	require.NotContains(t, string(encoded), testKey)
	require.Equal(t, 1, requests)
}
func TestListQRCodesFailureClasses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"invalid key", 401, "private", ListQRCodesBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"rate limit", 429, "private", "", sdkgo.FailureRateLimit, true},
		{"service outage", 502, "private", "", sdkgo.FailureAvailability, true},
		{"business auth", 200, `{"code":401,"error_code":100315,"message":"private"}`, ListQRCodesBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"business rate", 200, `{"code":429,"message":"private"}`, "", sdkgo.FailureRateLimit, true},
		{"business outage", 200, `{"code":502,"message":"private"}`, "", sdkgo.FailureAvailability, true},
		{"business rejection", 200, `{"code":400,"message":"private"}`, ListQRCodesBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"missing list", 200, `{"code":0,"data":{}}`, ListQRCodesBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"empty list", 200, `{"code":0,"data":{"list":[],"next_page_token":null,"total":0}}`, ListQRCodesBranchListed, "", false},
		{"bad item", 200, `{"code":0,"data":{"list":[{"name":"x"}],"total":1}}`, ListQRCodesBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListQRCodes(), testConnection, ListQRCodesInput{})
			if tc.retry {
				if tc.status == 429 {
					var retry *dex.RetryAfterError
					require.ErrorAs(t, err, &retry)
					require.Equal(t, 2*time.Second, retry.After)
				} else {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, err, &retry)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				if tc.kind != "" {
					require.Equal(t, tc.kind, result.Failure.Kind)
					require.NotContains(t, result.Failure.Message, "private")
				}
			}
		})
	}
}
func TestListQRCodesValidationAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":0,"data":{"list":[],"total":0}}`))
		require.NoError(t, err)
	}))
	client := newTestClient(t, server, 0)
	for _, input := range []ListQRCodesInput{{PageSize: 101}, {PageToken: "bad\nvalue"}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListQRCodes(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListQRCodesBranchDefect, result.Branch)
	}
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, client.ListQRCodes(), testConnection, ListQRCodesInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testKey)}}
	_, err = New(Config{BaseURL: "http://open.cli.im"}, credentials)
	require.Error(t, err)
	_, err = New(Config{BaseURL: "https://other.example"}, credentials)
	require.Error(t, err)
}
