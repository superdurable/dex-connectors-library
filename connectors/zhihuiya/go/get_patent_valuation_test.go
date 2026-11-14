// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zhihuiya

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "zhihuiya", Name: "zhihuiya"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "zhihuiya-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-patent-valuation" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sk-private")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetPatentValuationRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, patentValuationEndpoint+"?patent_number=JP6012277B2", request.URL.String())
		require.Equal(t, "Bearer sk-private", request.Header.Get("Authorization"))
		return reply(200, `{"data":[{"pn":"JP6012277B2","patent_id":"patent-1","patsnap_patent_valuation":{"patent_value":43000,"patsnap_legal":71,"patsnap_market":18,"patsnap_economy":23,"patsnap_strategic":71,"patsnap_technology":28},"abstract":"private"}],"status":true,"error_code":0}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetPatentValuation(), testConnection, GetPatentValuationInput{PublicationNumber: "JP6012277B2"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetPatentValuationBranchFound, result.Branch)
	require.Equal(t, GetPatentValuationOutput{PublicationNumber: "JP6012277B2", PatentID: "patent-1", PatentValueUSD: 43000, LegalScore: 71, MarketScore: 18, EconomyScore: 23, StrategicScore: 71, TechnologyScore: 28}, result.Value)
}
func TestGetPatentValuationClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"missing", 404, `{}`, GetPatentValuationBranchNoData, false},
		{"unauthorized", 401, `{}`, GetPatentValuationBranchProviderRejected, false},
		{"forbidden", 403, `{}`, GetPatentValuationBranchProviderRejected, false},
		{"rate", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, GetPatentValuationBranchInvalidResponse, false},
		{"missing status", 200, `{}`, GetPatentValuationBranchInvalidResponse, false},
		{"no data", 200, `{"data":[],"status":true,"error_code":0}`, GetPatentValuationBranchNoData, false},
		{"provider rate", 200, `{"status":false,"error_code":67200002}`, "", true},
		{"provider unavailable", 200, `{"status":false,"error_code":68300008}`, "", true},
		{"provider rejects", 200, `{"status":false,"error_code":67200004}`, GetPatentValuationBranchProviderRejected, false},
		{"wrong number", 200, `{"data":[{"pn":"US11205304B2","patent_id":"patent-1","patsnap_patent_valuation":{}}],"status":true,"error_code":0}`, GetPatentValuationBranchInvalidResponse, false},
		{"redirect", 302, `{}`, GetPatentValuationBranchProviderRejected, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				response := reply(scenario.status, scenario.body)
				if scenario.status == 302 {
					response.Header.Set("Location", "https://other.example/steal")
				}
				return response, nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetPatentValuation(), testConnection, GetPatentValuationInput{PublicationNumber: "JP6012277B2"})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "sk-private")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "sk-private")
		})
	}
}
func TestGetPatentValuationInvalidInputAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetPatentValuation(), testConnection, GetPatentValuationInput{PublicationNumber: "../bad"})
	require.NoError(t, err)
	require.Equal(t, GetPatentValuationBranchDefect, result.Branch)
	require.Zero(t, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).GetPatentValuation(), testConnection, GetPatentValuationInput{PublicationNumber: "JP6012277B2"})
	require.NoError(t, err)
	require.Equal(t, GetPatentValuationBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("sk-private") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetPatentValuation(), testConnection, GetPatentValuationInput{PublicationNumber: "JP6012277B2"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "sk-private")
}
