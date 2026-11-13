// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lixinger

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

const testToken = "private-test-token"

var testConnection = sdkgo.ConnectionRef{Provider: "lixinger", Name: "lixinger"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "lixinger-provider-test" }
func (queryContext) StepExecutionID() string     { return "stock-company-list" }
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
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {Token: sdkgo.NewSecretString(testToken)}}
	client, err := New(Config{BaseURL: "https://open.lixinger.com", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}
func TestListStockCompaniesRequestAndPrivacy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, stockCompaniesPath, request.URL.Path)
		var body struct {
			Token      string   `json:"token"`
			StockCodes []string `json:"stockCodes"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, testToken, body.Token)
		require.Equal(t, []string{"300750", "600519"}, body.StockCodes)
		_, err := writer.Write([]byte(`{"code":1,"message":"success","data":[{"stockCode":"300750","name":"宁德时代","exchange":"sz","listingStatus":"normally_listed","ipoDate":"2018-06-11","finance":"private"},{"stockCode":"600519","name":"贵州茅台","exchange":"sh"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListStockCompanies(), testConnection, ListStockCompaniesInput{StockCodes: []string{"300750", "600519"}})
	require.NoError(t, err)
	require.Equal(t, ListStockCompaniesBranchListed, result.Branch)
	require.Equal(t, ListStockCompaniesOutput{Companies: []StockCompany{{StockCode: "300750", Name: "宁德时代", Exchange: "sz", ListingStatus: "normally_listed"}, {StockCode: "600519", Name: "贵州茅台", Exchange: "sh"}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), testToken)
	require.NotContains(t, string(encoded), "private")
}
func TestListStockCompaniesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"bad token", 401, "private", ListStockCompaniesBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, "private", ListStockCompaniesBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"provider code", 200, `{"code":0,"message":"private"}`, ListStockCompaniesBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"rate limit", 429, "private", "", sdkgo.FailureRateLimit, true},
		{"server", 503, "private", "", sdkgo.FailureAvailability, true},
		{"malformed", 200, "{", ListStockCompaniesBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing data", 200, `{"code":1}`, ListStockCompaniesBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"wrong code", 200, `{"code":1,"data":[{"stockCode":"999999","name":"Unknown","exchange":"sz"}]}`, ListStockCompaniesBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListStockCompanies(), testConnection, ListStockCompaniesInput{StockCodes: []string{"300750"}})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testToken)
			}
		})
	}
}
func TestListStockCompaniesValidationAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":1,"data":[]}`))
		require.NoError(t, err)
	}))
	for _, codes := range [][]string{nil, {"ABCDEF"}, {"300750", "300750"}} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListStockCompanies(), testConnection, ListStockCompaniesInput{StockCodes: codes})
		require.NoError(t, err)
		require.Equal(t, ListStockCompaniesBranchDefect, result.Branch)
	}
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 8).ListStockCompanies(), testConnection, ListStockCompaniesInput{StockCodes: []string{"300750"}})
	require.NoError(t, err)
	require.Equal(t, ListStockCompaniesBranchInvalidResponse, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListStockCompanies(), testConnection, ListStockCompaniesInput{StockCodes: []string{"300750"}})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
func TestListStockCompaniesRejectsInsecureHost(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {Token: sdkgo.NewSecretString(testToken)}}
	_, err := New(Config{BaseURL: "http://open.lixinger.com"}, credentials)
	require.Error(t, err)
	_, err = New(Config{BaseURL: "https://other.example"}, credentials)
	require.Error(t, err)
}
