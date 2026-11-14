// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meiqia

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

var testConnection = sdkgo.ConnectionRef{Provider: "meiqia", Name: "meiqia"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "meiqia-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-conversation" }
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
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("private-token")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetConversationRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://api.meiqia.com/v1/conversations/123", request.URL.String())
		require.Equal(t, "Bearer private-token", request.Header.Get("Authorization"))
		return reply(200, `{"conv_id":123,"conv_start_tm":"2026-11-13 10:00:00","conv_end_tm":"2026-11-13 10:20:00","main_channel":"web","conv_content":[{"content":"private message"}],"client_info":{"name":"private"}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetConversation(), testConnection, GetConversationInput{ConversationID: 123})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetConversationBranchFound, result.Branch)
	require.Equal(t, GetConversationOutput{ConversationID: 123, StartedAt: "2026-11-13 10:00:00", EndedAt: "2026-11-13 10:20:00", MainChannel: "web"}, result.Value)
}
func TestGetConversationClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"missing", 404, `{}`, GetConversationBranchNoData, false},
		{"unauthorized", 401, `{}`, GetConversationBranchProviderRejected, false},
		{"forbidden", 403, `{}`, GetConversationBranchProviderRejected, false},
		{"rate", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, GetConversationBranchInvalidResponse, false},
		{"missing ticket", 200, `{}`, GetConversationBranchInvalidResponse, false},
		{"wrong ID", 200, `{"conv_id":124,"conv_start_tm":"2026-11-13 10:00:00"}`, GetConversationBranchInvalidResponse, false},
		{"redirect", 302, `{}`, GetConversationBranchProviderRejected, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetConversation(), testConnection, GetConversationInput{ConversationID: 123})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "private-token")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "private-token")
		})
	}
}
func TestGetConversationInvalidInputAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetConversation(), testConnection, GetConversationInput{ConversationID: 0})
	require.NoError(t, err)
	require.Equal(t, GetConversationBranchDefect, result.Branch)
	require.Zero(t, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).GetConversation(), testConnection, GetConversationInput{ConversationID: 123})
	require.NoError(t, err)
	require.Equal(t, GetConversationBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private-token") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetConversation(), testConnection, GetConversationInput{ConversationID: 123})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "private-token")
}
