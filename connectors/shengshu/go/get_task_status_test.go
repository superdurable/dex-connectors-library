// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shengshu

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

var testConnection = sdkgo.ConnectionRef{Provider: "shengshu", Name: "shengshu"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "vidu-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-task-status" }
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
	credentials := Credentials{APIKey: sdkgo.NewSecretString("secret-api-key")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetTaskStatusRequestAndSafeResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://api.vidu.cn/ent/v2/tasks/task-123/creations", request.URL.String())
		require.Equal(t, "Token secret-api-key", request.Header.Get("Authorization"))
		return reply(200, `{"id":"task-123","state":"success","err_code":"","credits":4,"payload":"private-payload","creations":[{"id":"creation-456","url":"https://example.invalid/private?token=media-secret","cover_url":"private-cover","watermarked_url":"private-watermarked"}]}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTaskStatus(), testConnection, GetTaskStatusInput{TaskID: "task-123"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetTaskStatusBranchFound, result.Branch)
	require.Equal(t, GetTaskStatusOutput{TaskID: "task-123", State: "success", Credits: 4, CreationIDs: []string{"creation-456"}}, result.Value)
	require.NotContains(t, result.Value.TaskID, "media-secret")
}
func TestGetTaskStatusClassifiesOutcomes(t *testing.T) {
	scenarios := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"pending", 200, `{"id":"task-123","state":"processing","credits":0}`, GetTaskStatusBranchFound, false},
		{"failed task", 200, `{"id":"task-123","state":"failed","err_code":"TaskPromptPolicyViolation","credits":0}`, GetTaskStatusBranchFound, false},
		{"not found", 404, `{}`, GetTaskStatusBranchNotFound, false},
		{"unauthorized", 401, `{"error":"secret-api-key"}`, GetTaskStatusBranchProviderRejected, false},
		{"mismatched id", 200, `{"id":"other","state":"success","credits":4}`, GetTaskStatusBranchInvalidResponse, false},
		{"unknown state", 200, `{"id":"task-123","state":"mystery","credits":4}`, GetTaskStatusBranchInvalidResponse, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTaskStatus(), testConnection, GetTaskStatusInput{TaskID: "task-123"})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "secret-api-key")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "secret-api-key")
			}
		})
	}
}
func TestGetTaskStatusRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, strings.Repeat("x", 100)), nil })
	client := newTestClient(t, transport, 16)
	for _, id := range []string{"", "../other", "bad/id", "bad?query", strings.Repeat("x", 129)} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetTaskStatus(), testConnection, GetTaskStatusInput{TaskID: id})
		require.NoError(t, err)
		require.Equal(t, GetTaskStatusBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetTaskStatus(), testConnection, GetTaskStatusInput{TaskID: "task-123"})
	require.NoError(t, err)
	require.Equal(t, GetTaskStatusBranchInvalidResponse, result.Branch)
}
func TestGetTaskStatusTransportAndRedirectAreSecretSafe(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret-api-key") })
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTaskStatus(), testConnection, GetTaskStatusInput{TaskID: "task-123"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "secret-api-key")
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTaskStatus(), testConnection, GetTaskStatusInput{TaskID: "task-123"})
	require.NoError(t, err)
	require.Equal(t, GetTaskStatusBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}
