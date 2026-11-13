// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package likelic

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

var testConnection = sdkgo.ConnectionRef{Provider: "likelic", Name: "likelic"}

const testAPIKey = "sk-0123456789abcdef0123456789abcdef0123456789abcdef"

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "likelic-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-rooms" }
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
	credentials := Credentials{CustomerCode: "demo_company", APIKey: sdkgo.NewSecretString(testAPIKey)}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestListRoomsRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, roomListEndpoint+"?page=1&limit=20", request.URL.String())
		require.Equal(t, "demo_company", request.Header.Get("X-Customer-Code"))
		require.Equal(t, testAPIKey, request.Header.Get("X-API-Key"))
		return reply(200, `{"success":true,"data":[{"channel":"wework","instance_id":3,"room_id":"room-123","push_state":"ready","room_name":"private group","company_name":"private customer","member_count":12}],"meta":{"page":1,"limit":20,"total":1}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListRooms(), testConnection, ListRoomsInput{Page: 1})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListRoomsBranchFound, result.Branch)
	require.Equal(t, ListRoomsOutput{Page: 1, Total: 1, Rooms: []Room{{Channel: "wework", InstanceID: 3, RoomID: "room-123", PushState: "ready"}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), testAPIKey)
}
func TestListRoomsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"success":true,"data":[],"meta":{"page":1,"limit":20,"total":0}}`, ListRoomsBranchFound, false},
		{"unauthorized", 401, `{}`, ListRoomsBranchProviderRejected, false},
		{"forbidden", 403, `{}`, ListRoomsBranchProviderRejected, false},
		{"rate", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, ListRoomsBranchInvalidResponse, false},
		{"missing list", 200, `{"success":true,"meta":{"page":1,"limit":20,"total":0}}`, ListRoomsBranchInvalidResponse, false},
		{"bad ID", 200, `{"success":true,"data":[{"channel":"wework","instance_id":3,"room_id":"","push_state":"ready"}],"meta":{"page":1,"limit":20,"total":1}}`, ListRoomsBranchInvalidResponse, false},
		{"wrong page", 200, `{"success":true,"data":[],"meta":{"page":2,"limit":20,"total":0}}`, ListRoomsBranchInvalidResponse, false},
		{"redirect", 302, `{}`, ListRoomsBranchProviderRejected, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListRooms(), testConnection, ListRoomsInput{Page: 1})
			require.Equal(t, 1, calls)
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), testAPIKey)
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, testAPIKey)
			}
		})
	}
}
func TestListRoomsInputAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListRooms(), testConnection, ListRoomsInput{Page: 0})
	require.NoError(t, err)
	require.Equal(t, ListRoomsBranchDefect, result.Branch)
	require.Zero(t, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).ListRooms(), testConnection, ListRoomsInput{Page: 1})
	require.NoError(t, err)
	require.Equal(t, ListRoomsBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New(testAPIKey) })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListRooms(), testConnection, ListRoomsInput{Page: 1})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), testAPIKey)
}
