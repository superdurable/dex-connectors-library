// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ones

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "private-test-key"

var testConnection = sdkgo.ConnectionRef{Provider: "ones", Name: "ones"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "ones-provider-test" }
func (queryContext) StepExecutionID() string     { return "team-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	client, err := New(Config{BaseURL: baseURL, MaxResponseBytes: maxResponseBytes}, credentials)
	require.NoError(t, err)
	return client
}
func TestTeamCatalogRequestAndPrivacy(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/openapi/v2/account/teams", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"result":"SUCCESS","data":{"teams":[{"id":"team-1","name":"Engineering","status":1,"owner":"private","createTime":123}]},"errorData":{"email":"private"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListTeams(), testConnection, ListTeamsInput{})
	require.NoError(t, err)
	require.Equal(t, ListTeamsBranchListed, result.Branch)
	require.Equal(t, ListTeamsOutput{Teams: []Team{{ID: "team-1", Name: "Engineering", Status: 1}}}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestTeamCatalogOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"unauthorized", 401, `{"error":{"code":"UNAUTHORIZED","message":"private-test-key"}}`, ListTeamsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, `private`, ListTeamsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"bad request", 400, `private`, ListTeamsBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"application forbidden", 200, `{"result":"FAIL","errorCode":"Forbidden","errorMsg":"private"}`, ListTeamsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"rate limited", 429, `private`, "", sdkgo.FailureRateLimit, true},
		{"bad gateway", 502, `private`, "", sdkgo.FailureAvailability, true},
		{"malformed", 200, `{`, ListTeamsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing data", 200, `{"result":"SUCCESS"}`, ListTeamsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing team ID", 200, `{"result":"SUCCESS","data":{"teams":[{"name":"private","status":1}]}}`, ListTeamsBranchInvalidResponse, sdkgo.FailureProtocol, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListTeams(), testConnection, ListTeamsInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testAPIKey)
			}
			if err != nil {
				require.NotContains(t, err.Error(), testAPIKey)
			}
		})
	}
}
func TestTeamCatalogSizeAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"result":"SUCCESS","data":{"teams":[{"id":"team-1","name":"Engineering","status":1}]}}`))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 8).ListTeams(), testConnection, ListTeamsInput{})
	require.NoError(t, err)
	require.Equal(t, ListTeamsBranchInvalidResponse, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListTeams(), testConnection, ListTeamsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAPIKey)
}
