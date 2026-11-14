// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kf5

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

var testConnection = sdkgo.ConnectionRef{Provider: "kf5", Name: "kf5"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "kf5-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-ticket" }
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
	credentials := Credentials{Subdomain: "sample", Email: sdkgo.NewSecretString("agent@example.com"), APIToken: sdkgo.NewSecretString("private-token")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetTicketRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://sample.kf5.com/apiv2/tickets/123.json", request.URL.String())
		username, password, ok := request.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "agent@example.com/token", username)
		require.Equal(t, "private-token", password)
		return reply(200, `{"ticket":{"id":123,"status":"open","priority":"normal","type":"question","created_at":"2026-11-13 10:00:00","updated_at":"2026-11-13 11:00:00","title":"private title","description":"private body","requester_id":456}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTicket(), testConnection, GetTicketInput{TicketID: 123})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetTicketBranchFound, result.Branch)
	require.Equal(t, GetTicketOutput{TicketID: 123, Status: "open", Priority: "normal", Type: "question", CreatedAt: "2026-11-13 10:00:00", UpdatedAt: "2026-11-13 11:00:00"}, result.Value)
}
func TestGetTicketClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"missing", 404, `{}`, GetTicketBranchNoData, false},
		{"unauthorized", 401, `{}`, GetTicketBranchProviderRejected, false},
		{"forbidden", 403, `{}`, GetTicketBranchProviderRejected, false},
		{"rate", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, GetTicketBranchInvalidResponse, false},
		{"missing ticket", 200, `{}`, GetTicketBranchInvalidResponse, false},
		{"wrong ID", 200, `{"ticket":{"id":124,"status":"open"}}`, GetTicketBranchInvalidResponse, false},
		{"redirect", 302, `{}`, GetTicketBranchProviderRejected, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTicket(), testConnection, GetTicketInput{TicketID: 123})
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
func TestGetTicketInvalidInputAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTicket(), testConnection, GetTicketInput{TicketID: 0})
	require.NoError(t, err)
	require.Equal(t, GetTicketBranchDefect, result.Branch)
	require.Zero(t, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).GetTicket(), testConnection, GetTicketInput{TicketID: 123})
	require.NoError(t, err)
	require.Equal(t, GetTicketBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private-token") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTicket(), testConnection, GetTicketInput{TicketID: 123})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "private-token")
}
