// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ip138

import (
	"context"
	"encoding/json"
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

var testConnection = sdkgo.ConnectionRef{Provider: "ip138", Name: "ip138"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "ip138-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-token-usage" }
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
	credentials := Credentials{Token: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetTokenUsageRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://api.ip138.com/status/?token=test-secret", request.URL.String())
		require.Equal(t, "test-secret", request.URL.Query().Get("token"))
		return reply(200, `{"ret":"ok","data":{"reqs":10,"hour":3000,"package":19767,"private":"private-cover"},"msg":"private-trace"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTokenUsage(), testConnection, GetTokenUsageInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetTokenUsageBranchFound, result.Branch)
	require.Equal(t, GetTokenUsageOutput{CurrentHourRequests: 10, HourlyAllowance: 3000, PackageQuota: 19767}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-cover")
	require.NotContains(t, string(encoded), "private-trace")
}
func TestGetTokenUsageClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"business rejection", 200, `{"ret":"err","msg":"test-secret"}`, GetTokenUsageBranchProviderRejected, false},
		{"HTTP 202 token or quota rejection", 202, `{"msg":"test-secret"}`, GetTokenUsageBranchProviderRejected, false},
		{"unauthorized", 401, `{"msg":"test-secret"}`, GetTokenUsageBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing ret", 200, `{"data":{}}`, GetTokenUsageBranchInvalidResponse, false},
		{"missing count", 200, `{"ret":"ok","data":{"reqs":0,"hour":1}}`, GetTokenUsageBranchInvalidResponse, false},
		{"negative count", 200, `{"ret":"ok","data":{"reqs":-1,"hour":1,"package":2}}`, GetTokenUsageBranchInvalidResponse, false},
		{"unknown ret", 200, `{"ret":"unknown"}`, GetTokenUsageBranchInvalidResponse, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTokenUsage(), testConnection, GetTokenUsageInput{})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "test-secret")
		})
	}
}
func TestGetTokenUsageBoundTransportAndRedirect(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).GetTokenUsage(), testConnection, GetTokenUsageInput{})
	require.NoError(t, err)
	require.Equal(t, GetTokenUsageBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTokenUsage(), testConnection, GetTokenUsageInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTokenUsage(), testConnection, GetTokenUsageInput{})
	require.NoError(t, err)
	require.Equal(t, GetTokenUsageBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}

func TestGetTokenUsageRejectsInvalidTokenWithoutRequest(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	credentials := Credentials{Token: sdkgo.NewSecretString("bad token")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetTokenUsage(), testConnection, GetTokenUsageInput{})
	require.NoError(t, err)
	require.Equal(t, GetTokenUsageBranchDefect, result.Branch)
	require.Zero(t, calls)
}
