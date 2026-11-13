// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zego

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

var testConnection = sdkgo.ConnectionRef{Provider: "zego", Name: "zego"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "zego-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-room-user-count" }
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
	credentials := Credentials{AppID: "12345", ServerSecret: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetRoomUserCountRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https", request.URL.Scheme)
		require.Equal(t, "zim-api.zego.im", request.URL.Host)
		query := request.URL.Query()
		require.Equal(t, "QueryRoomUserList", query.Get("Action"))
		require.Equal(t, "12345", query.Get("AppId"))
		require.Equal(t, "room-1", query.Get("RoomId"))
		require.Equal(t, "1", query.Get("Limit"))
		require.Equal(t, "2.0", query.Get("SignatureVersion"))
		require.Len(t, query.Get("SignatureNonce"), 16)
		require.Equal(t, roomSignature("12345", query.Get("SignatureNonce"), "test-secret", query.Get("Timestamp")), query.Get("Signature"))
		return reply(200, `{"Code":0,"Count":2,"UserList":[{"UserId":"private-cover","UserName":"private-trace"}],"Message":"private-trace"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetRoomUserCount(), testConnection, GetRoomUserCountInput{RoomID: "room-1"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetRoomUserCountBranchFound, result.Branch)
	require.Equal(t, GetRoomUserCountOutput{Count: 2}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-cover")
	require.NotContains(t, string(encoded), "private-trace")
}
func TestGetRoomUserCountClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"room missing", 200, `{"Code":660300001,"Message":"test-secret"}`, GetRoomUserCountBranchProviderRejected, false},
		{"bad signature", 200, `{"Code":100000005,"Message":"test-secret"}`, GetRoomUserCountBranchProviderRejected, false},
		{"provider rate limit", 200, `{"Code":660300006}`, "", true},
		{"signature expired", 200, `{"Code":100000004}`, "", true},
		{"room service error", 200, `{"Code":660300007}`, "", true},
		{"HTTP rate limit", 429, `{}`, "", true},
		{"HTTP unavailable", 503, `{}`, "", true},
		{"missing code", 200, `{"Count":2}`, GetRoomUserCountBranchInvalidResponse, false},
		{"missing count", 200, `{"Code":0}`, GetRoomUserCountBranchInvalidResponse, false},
		{"negative count", 200, `{"Code":0,"Count":-1}`, GetRoomUserCountBranchInvalidResponse, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetRoomUserCount(), testConnection, GetRoomUserCountInput{RoomID: "room-1"})
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
func TestGetRoomUserCountBoundTransportAndRedirect(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).GetRoomUserCount(), testConnection, GetRoomUserCountInput{RoomID: "room-1"})
	require.NoError(t, err)
	require.Equal(t, GetRoomUserCountBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetRoomUserCount(), testConnection, GetRoomUserCountInput{RoomID: "room-1"})
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
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetRoomUserCount(), testConnection, GetRoomUserCountInput{RoomID: "room-1"})
	require.NoError(t, err)
	require.Equal(t, GetRoomUserCountBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}

func TestRoomSignatureOfficialVector(t *testing.T) {
	require.Equal(t, "43e5cfcca828314675f91b001390566a", roomSignature("12345", "4fd24687296dd9f3", "9193cc662a4c0ec135ec71fb57194b38", "1615186943"))
}
func TestGetRoomUserCountRejectsInvalidRoomWithoutRequest(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetRoomUserCount(), testConnection, GetRoomUserCountInput{RoomID: "bad/room"})
	require.NoError(t, err)
	require.Equal(t, GetRoomUserCountBranchDefect, result.Branch)
	require.Zero(t, calls)
}
