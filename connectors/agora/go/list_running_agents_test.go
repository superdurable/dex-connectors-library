// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package agora

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "agora", Name: "agora"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "agora-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-running-agents" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credential := Credentials{AppID: "1234567890abcdef1234567890abcdef", CustomerID: "customer-id", CustomerSecret: sdkgo.NewSecretString("customer-secret")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credential})
	require.NoError(t, err)
	return client
}
func TestListRunningAgentsUsesBasicAuthAndReadsPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/conversational-ai-agent/v2/projects/1234567890abcdef1234567890abcdef/agents", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.Equal(t, "Basic Y3VzdG9tZXItaWQ6Y3VzdG9tZXItc2VjcmV0", request.Header.Get("Authorization"))
		_, err := io.WriteString(writer, `{"data":{"count":1,"list":[{"start_ts":1735035893,"status":"RUNNING","agent_id":"agent-123"}]},"meta":{"cursor":"next","total":2},"status":"ok"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListRunningAgents(), testConnection, ListRunningAgentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListRunningAgentsBranchListed, result.Branch)
	require.Equal(t, RunningAgents{Count: 1, Agents: []RunningAgent{{AgentID: "agent-123", Status: "RUNNING", StartTimestamp: 1735035893}}, Total: 2, Cursor: "next"}, result.Value)
}
func TestListRunningAgentsClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty page", 200, `{"data":{"count":0,"list":[]},"meta":{"cursor":"","total":0},"status":"ok"}`, ListRunningAgentsBranchListed, false},
		{"unauthorized", 401, `{"reason":"unauthorized"}`, ListRunningAgentsBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing data", 200, `{"status":"ok"}`, ListRunningAgentsBranchInvalidResponse, false},
		{"bad count", 200, `{"data":{"count":1,"list":[]},"meta":{"total":1},"status":"ok"}`, ListRunningAgentsBranchInvalidResponse, false},
		{"wrong status", 200, `{"data":{"count":1,"list":[{"start_ts":1735035893,"status":"STOPPED","agent_id":"a"}]},"meta":{"total":1},"status":"ok"}`, ListRunningAgentsBranchInvalidResponse, false},
		{"malformed", 200, `{"data":`, ListRunningAgentsBranchInvalidResponse, false},
		{"trailing", 200, `{"data":{"count":0,"list":[]},"meta":{"total":0},"status":"ok"} {}`, ListRunningAgentsBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListRunningAgents(), testConnection, ListRunningAgentsInput{})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestListRunningAgentsBoundsAndRejectsInvalidCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"data":{"count":0,"list":[]},"meta":{"total":0},"status":"ok"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).ListRunningAgents(), testConnection, ListRunningAgentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListRunningAgentsBranchInvalidResponse, result.Branch)
	require.False(t, validAppID("../unsafe"))
	require.False(t, validBasicUsername("a:b"))
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListRunningAgents(), testConnection, ListRunningAgentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListRunningAgentsBranchDefect, result.Branch)
}
