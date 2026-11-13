// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dedao

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

var testConnection = sdkgo.ConnectionRef{Provider: "dedao", Name: "dedao"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "dedao-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-note-task-status" }
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
	credentials := Credentials{APIKey: sdkgo.NewSecretString("gk_live_test-secret"), ClientID: sdkgo.NewSecretString("cli_test-client")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetNoteTaskStatusRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, taskStatusEndpoint, request.URL.String())
		require.Equal(t, "gk_live_test-secret", request.Header.Get("Authorization"))
		require.Equal(t, "cli_test-client", request.Header.Get("X-Client-ID"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"task_id":"69c3995e99f5a67e"}`, string(body))
		return reply(200, `{"success":true,"data":{"status":"success","note_id":"1907975006118548200","content":"test-secret"}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetNoteTaskStatus(), testConnection, GetNoteTaskStatusInput{TaskID: "69c3995e99f5a67e"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetNoteTaskStatusBranchFound, result.Branch)
	require.Equal(t, GetNoteTaskStatusOutput{TaskID: "69c3995e99f5a67e", Status: "success", NoteID: "1907975006118548200"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "test-secret")
}
func TestGetNoteTaskStatusOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"pending", 200, `{"success":true,"data":{"status":"pending"}}`, GetNoteTaskStatusBranchFound, false},
		{"failed task", 200, `{"success":true,"data":{"status":"failed"}}`, GetNoteTaskStatusBranchFound, false},
		{"rejected", 200, `{"success":false,"message":"gk_live_test-secret"}`, GetNoteTaskStatusBranchProviderRejected, false},
		{"unauthorized", 401, `{"message":"gk_live_test-secret"}`, GetNoteTaskStatusBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"server", 503, `{}`, "", true},
		{"unknown status", 200, `{"success":true,"data":{"status":"unknown"}}`, GetNoteTaskStatusBranchInvalidResponse, false},
		{"missing data", 200, `{"success":true}`, GetNoteTaskStatusBranchInvalidResponse, false},
		{"redirect", 302, `{}`, GetNoteTaskStatusBranchProviderRejected, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetNoteTaskStatus(), testConnection, GetNoteTaskStatusInput{TaskID: "69c3995e99f5a67e"})
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
func TestGetNoteTaskStatusValidationAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("test-secret") })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetNoteTaskStatus(), testConnection, GetNoteTaskStatusInput{TaskID: "bad id"})
	require.NoError(t, err)
	require.Equal(t, GetNoteTaskStatusBranchDefect, result.Branch)
	require.Zero(t, calls)
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetNoteTaskStatus(), testConnection, GetNoteTaskStatusInput{TaskID: "69c3995e99f5a67e"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
}
