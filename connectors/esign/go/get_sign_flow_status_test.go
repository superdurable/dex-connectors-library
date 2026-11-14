// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
package esign

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "esign", Name: "esign"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "esign-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-sign-flow-status" }
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
	credentials := Credentials{AppID: "app-123", AppSecret: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestSignReadRequestMatchesCanonicalVector(t *testing.T) {
	require.Equal(t, "C9P4BkL3d1BJ0EE390AVvnpUMYhDTix72iBm4yWPiZs=", signReadRequest("test-secret", "/v3/sign-flow/flow123/detail"))
}
func TestGetSignFlowStatusRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://openapi.esign.cn/v3/sign-flow/flow123/detail", request.URL.String())
		require.Equal(t, "app-123", request.Header.Get("X-Tsign-Open-App-Id"))
		require.Equal(t, "Signature", request.Header.Get("X-Tsign-Open-Auth-Mode"))
		require.Equal(t, "*/*", request.Header.Get("Accept"))
		require.Equal(t, "application/json; charset=UTF-8", request.Header.Get("Content-Type"))
		require.Equal(t, "C9P4BkL3d1BJ0EE390AVvnpUMYhDTix72iBm4yWPiZs=", request.Header.Get("X-Tsign-Open-Ca-Signature"))
		stamp, err := strconv.ParseInt(request.Header.Get("X-Tsign-Open-Ca-Timestamp"), 10, 64)
		require.NoError(t, err)
		require.WithinDuration(t, time.Now(), time.UnixMilli(stamp), time.Minute)
		return reply(200, `{"code":0,"data":{"signFlowStatus":1,"signers":[{"accountMobile":"private-phone"}],"docs":[{"fileName":"private-contract"}]}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetSignFlowStatus(), testConnection, GetSignFlowStatusInput{SignFlowID: "flow123"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetSignFlowStatusBranchFound, result.Branch)
	require.Equal(t, GetSignFlowStatusOutput{SignFlowID: "flow123", SignFlowStatus: 1}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-phone")
	require.NotContains(t, string(encoded), "private-contract")
}
func TestGetSignFlowStatusClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"business rejection", 200, `{"code":403,"message":"test-secret"}`, GetSignFlowStatusBranchProviderRejected, false},
		{"unauthorized", 401, `{"message":"test-secret"}`, GetSignFlowStatusBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true}, {"unavailable", 503, `{}`, "", true},
		{"missing code", 200, `{"data":{"signFlowStatus":1}}`, GetSignFlowStatusBranchInvalidResponse, false},
		{"missing data", 200, `{"code":0}`, GetSignFlowStatusBranchInvalidResponse, false},
		{"missing status", 200, `{"code":0,"data":{}}`, GetSignFlowStatusBranchInvalidResponse, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetSignFlowStatus(), testConnection, GetSignFlowStatusInput{SignFlowID: "flow123"})
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
func TestGetSignFlowStatusInputValidationAndBounds(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, strings.Repeat("x", 100)), nil })
	client := newTestClient(t, transport, 16)
	for _, id := range []string{"", "../other", "flow/1", "flow?x=1", " flow"} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetSignFlowStatus(), testConnection, GetSignFlowStatusInput{SignFlowID: id})
		require.NoError(t, err)
		require.Equal(t, GetSignFlowStatusBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetSignFlowStatus(), testConnection, GetSignFlowStatusInput{SignFlowID: "flow123"})
	require.NoError(t, err)
	require.Equal(t, GetSignFlowStatusBranchInvalidResponse, result.Branch)
}
func TestGetSignFlowStatusTransportAndRedirectAreSecretSafe(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetSignFlowStatus(), testConnection, GetSignFlowStatusInput{SignFlowID: "flow123"})
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
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetSignFlowStatus(), testConnection, GetSignFlowStatusInput{SignFlowID: "flow123"})
	require.NoError(t, err)
	require.Equal(t, GetSignFlowStatusBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}
