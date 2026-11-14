// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tianapi

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

var testConnection = sdkgo.ConnectionRef{Provider: "tianapi", Name: "tianapi"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "tianapi-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-holiday-status" }
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
	credentials := Credentials{APIKey: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetHolidayStatusRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "https://apis.tianapi.com/jiejiari/index", request.URL.String())
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		require.NoError(t, request.ParseForm())
		require.Equal(t, "test-secret", request.PostForm.Get("key"))
		require.Equal(t, "2021-01-01", request.PostForm.Get("date"))
		require.Equal(t, "0", request.PostForm.Get("type"))
		return reply(200, `{"code":200,"result":{"list":[{"date":"2021-01-01","daycode":1,"isnotwork":1,"private":"private-cover"}]},"msg":"private-trace"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetHolidayStatus(), testConnection, GetHolidayStatusInput{Date: "2021-01-01"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetHolidayStatusBranchFound, result.Branch)
	require.Equal(t, GetHolidayStatusOutput{Date: "2021-01-01", DayCode: 1, IsNotWork: 1}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-cover")
	require.NotContains(t, string(encoded), "private-trace")
}
func TestGetHolidayStatusClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"business rejection", 200, `{"code":230,"msg":"test-secret"}`, GetHolidayStatusBranchProviderRejected, false},
		{"quota exhausted", 200, `{"code":150,"msg":"test-secret"}`, GetHolidayStatusBranchProviderRejected, false},
		{"unauthorized", 401, `{"message":"test-secret"}`, GetHolidayStatusBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"provider rate limit", 200, `{"code":130,"msg":"test-secret"}`, "", true},
		{"server error", 200, `{"code":100,"msg":"test-secret"}`, "", true},
		{"maintenance", 200, `{"code":120,"msg":"test-secret"}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing status", 200, `{"result":{"list":[]}}`, GetHolidayStatusBranchInvalidResponse, false},
		{"missing result", 200, `{"code":200}`, GetHolidayStatusBranchInvalidResponse, false},
		{"invalid holiday status", 200, `{"code":200,"result":{"list":[{"date":"2021-01-01","daycode":4,"isnotwork":1}]}}`, GetHolidayStatusBranchInvalidResponse, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetHolidayStatus(), testConnection, GetHolidayStatusInput{Date: "2021-01-01"})
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
func TestGetHolidayStatusBoundTransportAndRedirect(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).GetHolidayStatus(), testConnection, GetHolidayStatusInput{Date: "2021-01-01"})
	require.NoError(t, err)
	require.Equal(t, GetHolidayStatusBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetHolidayStatus(), testConnection, GetHolidayStatusInput{Date: "2021-01-01"})
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
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetHolidayStatus(), testConnection, GetHolidayStatusInput{Date: "2021-01-01"})
	require.NoError(t, err)
	require.Equal(t, GetHolidayStatusBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}

func TestGetHolidayStatusRejectsInvalidDateWithoutRequest(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetHolidayStatus(), testConnection, GetHolidayStatusInput{Date: "2021-02-30"})
	require.NoError(t, err)
	require.Equal(t, GetHolidayStatusBranchDefect, result.Branch)
	require.Zero(t, calls)
}
