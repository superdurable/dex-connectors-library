// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threatbook

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

var testConnection = sdkgo.ConnectionRef{Provider: "threatbook", Name: "threatbook"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "threatbook-provider-test" }
func (queryContext) StepExecutionID() string     { return "check-ip-reputation" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(token any) any         { return context.Background().Value(token) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestCheckIPReputationRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://api.threatbook.cn/v3/scene/ip_reputation", request.URL.Scheme+"://"+request.URL.Host+request.URL.Path)
		require.Equal(t, "test-secret", request.URL.Query().Get("apikey"))
		require.Equal(t, "159.203.93.255", request.URL.Query().Get("resource"))
		require.Equal(t, "en", request.URL.Query().Get("lang"))
		return reply(200, `{"response_code":0,"verbose_msg":"Ok","data":{"159.203.93.255":{"is_malicious":true,"confidence_level":"high","severity":"medium","judgments":["Scanner"],"permalink":"private-link"}}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).CheckIPReputation(), testConnection, CheckIPReputationInput{IPAddress: "159.203.93.255"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, CheckIPReputationBranchFound, result.Branch)
	require.Equal(t, CheckIPReputationOutput{IsMalicious: true, ConfidenceLevel: "high", Severity: "medium", Judgments: []string{"Scanner"}}, result.Value)
}
func TestCheckIPReputationClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"no data", 200, `{"response_code":2,"verbose_msg":"No Data"}`, CheckIPReputationBranchNoData, false},
		{"invalid key", 200, `{"response_code":-1,"verbose_msg":"test-secret"}`, CheckIPReputationBranchProviderRejected, false},
		{"frequency", 200, `{"response_code":-4,"verbose_msg":"Frequent Limitation"}`, "", true},
		{"quota", 200, `{"response_code":-4,"verbose_msg":"Beyond Daily Limitation"}`, CheckIPReputationBranchProviderRejected, false},
		{"system", 200, `{"response_code":-5}`, "", true},
		{"HTTP rate", 429, `{}`, "", true},
		{"HTTP unavailable", 503, `{}`, "", true},
		{"missing code", 200, `{}`, CheckIPReputationBranchInvalidResponse, false},
		{"missing IP", 200, `{"response_code":0,"data":{}}`, CheckIPReputationBranchInvalidResponse, false},
		{"missing verdict", 200, `{"response_code":0,"data":{"159.203.93.255":{}}}`, CheckIPReputationBranchInvalidResponse, false},
		{"redirect", 302, `{}`, CheckIPReputationBranchProviderRejected, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				response := reply(scenario.status, scenario.body)
				if scenario.status == 302 {
					response.Header.Set("Location", "https://other.example/steal")
				}
				return response, nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).CheckIPReputation(), testConnection, CheckIPReputationInput{IPAddress: "159.203.93.255"})
			require.Equal(t, 1, calls)
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "test-secret")
			}
		})
	}
}
func TestCheckIPReputationLocalValidationAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	client := newTestClient(t, transport, 0)
	for _, address := range []string{"", "example.com", "192.0.2.0/24", "127.0.0.1:80"} {
		result, err := sdkgo.RunQuery(queryContext{}, client.CheckIPReputation(), testConnection, CheckIPReputationInput{IPAddress: address})
		require.NoError(t, err)
		require.Equal(t, CheckIPReputationBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).CheckIPReputation(), testConnection, CheckIPReputationInput{IPAddress: "159.203.93.255"})
	require.NoError(t, err)
	require.Equal(t, CheckIPReputationBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).CheckIPReputation(), testConnection, CheckIPReputationInput{IPAddress: "159.203.93.255"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
}
