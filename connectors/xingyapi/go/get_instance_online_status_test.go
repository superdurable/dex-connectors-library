// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xingyapi

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

var testConnection = sdkgo.ConnectionRef{Provider: "xingyapi", Name: "xingyapi"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "xingyapi-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-instance-online-status" }
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
func TestGetInstanceOnlineStatusRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "https://api.xingyapi.com/api/personal/getInfo", request.URL.String())
		require.Equal(t, "test-secret", request.Header.Get("X-Nebula-Key"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"instance_guid":"inst_example123"}`, string(body))
		return reply(200, `{"ok":true,"code":0,"message":"success","request_id":"nb_example","data":{"uin":9007199254740993,"name":"private","mobile":"123","corpId":888}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetInstanceOnlineStatus(), testConnection, GetInstanceOnlineStatusInput{InstanceGUID: "inst_example123"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetInstanceOnlineStatusBranchChecked, result.Branch)
	require.Equal(t, GetInstanceOnlineStatusOutput{IsOnline: true, AccountID: "9007199254740993"}, result.Value)
}
func TestGetInstanceOnlineStatusClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		online bool
		retry  bool
	}{
		{"offline", 200, `{"ok":false,"code":50001,"message":"no account is currently logged in on this device"}`, GetInstanceOnlineStatusBranchChecked, false, false},
		{"zero ID", 200, `{"ok":true,"code":0,"data":{"uin":0}}`, GetInstanceOnlineStatusBranchChecked, false, false},
		{"string ID", 200, `{"ok":true,"code":0,"data":{"uin":"9007199254740993"}}`, GetInstanceOnlineStatusBranchChecked, true, false},
		{"bad key", 200, `{"ok":false,"code":401,"message":"test-secret"}`, GetInstanceOnlineStatusBranchProviderRejected, false, false},
		{"other 50001", 200, `{"ok":false,"code":50001,"message":"other"}`, GetInstanceOnlineStatusBranchProviderRejected, false, false},
		{"upstream", 200, `{"ok":false,"code":50000}`, "", false, true},
		{"HTTP rate", 429, `{}`, "", false, true},
		{"HTTP unavailable", 503, `{}`, "", false, true},
		{"missing code", 200, `{}`, GetInstanceOnlineStatusBranchInvalidResponse, false, false},
		{"missing data", 200, `{"ok":true,"code":0}`, GetInstanceOnlineStatusBranchInvalidResponse, false, false},
		{"bad ID", 200, `{"ok":true,"code":0,"data":{"uin":1.2}}`, GetInstanceOnlineStatusBranchInvalidResponse, false, false},
		{"redirect", 302, `{}`, GetInstanceOnlineStatusBranchProviderRejected, false, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetInstanceOnlineStatus(), testConnection, GetInstanceOnlineStatusInput{InstanceGUID: "inst_example123"})
			require.Equal(t, 1, calls)
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if scenario.branch == GetInstanceOnlineStatusBranchChecked {
				require.Equal(t, scenario.online, result.Value.IsOnline)
			}
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "test-secret")
			}
		})
	}
}
func TestGetInstanceOnlineStatusLocalValidationAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	client := newTestClient(t, transport, 0)
	for _, instanceGUID := range []string{"", "bad", "inst_bad\nvalue"} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetInstanceOnlineStatus(), testConnection, GetInstanceOnlineStatusInput{InstanceGUID: instanceGUID})
		require.NoError(t, err)
		require.Equal(t, GetInstanceOnlineStatusBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).GetInstanceOnlineStatus(), testConnection, GetInstanceOnlineStatusInput{InstanceGUID: "inst_example123"})
	require.NoError(t, err)
	require.Equal(t, GetInstanceOnlineStatusBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetInstanceOnlineStatus(), testConnection, GetInstanceOnlineStatusInput{InstanceGUID: "inst_example123"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
}
