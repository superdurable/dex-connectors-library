// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package easemob

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "easemob", Name: "easemob"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "easemob-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-user-detail" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credential := Credentials{AppKey: "demo#chat", ClientID: "client-id", ClientSecret: sdkgo.NewSecretString("client-secret")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credential})
	require.NoError(t, err)
	return client
}
func userResponse() string {
	return `{"count":1,"entities":[{"uuid":"0ffe2d80-0000-0000-8d66-279e3e1c214b","created":1542795196504,"modified":1542795196504,"username":"user1","activated":true,"nickname":"Sample User","push_token":"DO-NOT-EXPOSE"}]}`
}
func TestGetUserDetailObtainsAndCachesAppToken(t *testing.T) {
	var tokenCalls, userCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/demo/chat/token":
			tokenCalls.Add(1)
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, "application/json", request.Header.Get("Content-Type"))
			var body map[string]any
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			require.Equal(t, "client_credentials", body["grant_type"])
			require.Equal(t, "client-id", body["client_id"])
			require.Equal(t, "client-secret", body["client_secret"])
			_, err := io.WriteString(writer, `{"access_token":"secret-token","expires_in":3600,"application":"uuid"}`)
			require.NoError(t, err)
		case "/demo/chat/users/user1":
			userCalls.Add(1)
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, "Bearer secret-token", request.Header.Get("Authorization"))
			_, err := io.WriteString(writer, userResponse())
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", request.URL.Path)
			writer.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	for range 2 {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetUserDetail(), testConnection, GetUserDetailInput{UserID: "user1"})
		require.NoError(t, err)
		require.Equal(t, GetUserDetailBranchFound, result.Branch)
		require.Equal(t, "user1", result.Value.UserID)
		require.True(t, result.Value.Activated)
		require.NotContains(t, result.Value.Nickname, "DO-NOT-EXPOSE")
	}
	require.EqualValues(t, 1, tokenCalls.Load())
	require.EqualValues(t, 2, userCalls.Load())
}
func TestGetUserDetailRefreshesTokenOnceOnUnauthorized(t *testing.T) {
	var tokenCalls, userCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/token") {
			tokenCalls.Add(1)
			_, err := io.WriteString(writer, `{"access_token":"token","expires_in":3600}`)
			require.NoError(t, err)
			return
		}
		if userCalls.Add(1) == 1 {
			writer.WriteHeader(401)
			return
		}
		_, err := io.WriteString(writer, userResponse())
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetUserDetail(), testConnection, GetUserDetailInput{UserID: "user1"})
	require.NoError(t, err)
	require.Equal(t, GetUserDetailBranchFound, result.Branch)
	require.EqualValues(t, 2, tokenCalls.Load())
	require.EqualValues(t, 2, userCalls.Load())
}
func TestGetUserDetailClassifiesProviderAndProtocolOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		tokenStatus, userStatus int
		tokenBody, userBody     string
		branch                  sdkgo.BranchID
		retry                   bool
	}{
		{"token rejected", 400, 0, `{"error":"invalid_grant"}`, "", GetUserDetailBranchProviderRejected, false},
		{"token rate limit", 429, 0, `{}`, "", "", true},
		{"token malformed", 200, 0, `{}`, "", GetUserDetailBranchInvalidResponse, false},
		{"user missing", 200, 404, `{"access_token":"token","expires_in":3600}`, `{}`, GetUserDetailBranchProviderRejected, false},
		{"user unavailable", 200, 503, `{"access_token":"token","expires_in":3600}`, `{}`, "", true},
		{"user malformed", 200, 200, `{"access_token":"token","expires_in":3600}`, `{"count":1}`, GetUserDetailBranchInvalidResponse, false},
		{"user wrong identity", 200, 200, `{"access_token":"token","expires_in":3600}`, `{"count":1,"entities":[{"uuid":"uuid","created":1,"modified":1,"username":"other","activated":true}]}`, GetUserDetailBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if strings.HasSuffix(request.URL.Path, "/token") {
					writer.WriteHeader(tc.tokenStatus)
					_, err := io.WriteString(writer, tc.tokenBody)
					require.NoError(t, err)
					return
				}
				writer.WriteHeader(tc.userStatus)
				_, err := io.WriteString(writer, tc.userBody)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetUserDetail(), testConnection, GetUserDetailInput{UserID: "user1"})
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
func TestGetUserDetailValidatesInputsAndBoundsResponses(t *testing.T) {
	require.False(t, validUserID("BAD"))
	_, _, isValid := parseAppKey("../bad#app")
	require.False(t, isValid)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/token") {
			_, err := io.WriteString(writer, `{"access_token":"token","expires_in":3600}`)
			require.NoError(t, err)
			return
		}
		_, err := io.WriteString(writer, userResponse())
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).GetUserDetail(), testConnection, GetUserDetailInput{UserID: "user1"})
	require.NoError(t, err)
	require.Equal(t, GetUserDetailBranchInvalidResponse, result.Branch)
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetUserDetail(), testConnection, GetUserDetailInput{UserID: "user1"})
	require.NoError(t, err)
	require.Equal(t, GetUserDetailBranchDefect, result.Branch)
}
