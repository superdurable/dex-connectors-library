// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bmob

import (
	"context"
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

const testApplicationID = "test-bmob-app"
const testRESTAPIKey = "test-bmob-rest-key"

var testConnection = sdkgo.ConnectionRef{Provider: "bmob", Name: "bmob"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "bmob-provider-test" }
func (queryContext) StepExecutionID() string     { return "class-count" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {ApplicationID: sdkgo.NewSecretString(testApplicationID), RESTAPIKey: sdkgo.NewSecretString(testRESTAPIKey)}})
	require.NoError(t, err)
	return client
}
func TestCountClassObjectsRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/1/classes/GameScore", request.URL.Path)
		require.Equal(t, "1", request.URL.Query().Get("count"))
		require.Equal(t, "0", request.URL.Query().Get("limit"))
		require.Equal(t, testApplicationID, request.Header.Get("X-Bmob-Application-Id"))
		require.Equal(t, testRESTAPIKey, request.Header.Get("X-Bmob-REST-API-Key"))
		_, err := writer.Write([]byte(`{"results":[],"count":42}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).CountClassObjects(), testConnection, CountClassObjectsInput{ClassName: "GameScore"})
	require.NoError(t, err)
	require.Equal(t, CountClassObjectsBranchCounted, result.Branch)
	require.Equal(t, int64(42), result.Value.Count)
	require.EqualValues(t, 1, calls.Load())
}
func TestCountClassObjectsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"bad credentials", 401, `{"error":"unauthorized"}`, CountClassObjectsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, `{"error":"forbidden"}`, CountClassObjectsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"missing class", 404, `{"code":101,"error":"private"}`, CountClassObjectsBranchProviderRejected, sdkgo.FailureValidation, false},
		{"rate limit", 429, `private provider detail`, "", sdkgo.FailureRateLimit, true},
		{"server", 500, `private provider detail`, "", sdkgo.FailureAvailability, true},
		{"bad JSON", 200, `{`, CountClassObjectsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing count", 200, `{"results":[]}`, CountClassObjectsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"negative count", 200, `{"results":[],"count":-1}`, CountClassObjectsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"unexpected records", 200, `{"results":[{"secret":"private"}],"count":1}`, CountClassObjectsBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).CountClassObjects(), testConnection, CountClassObjectsInput{ClassName: "GameScore"})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
			}
			if err != nil {
				require.NotContains(t, err.Error(), testRESTAPIKey)
				require.NotContains(t, err.Error(), "private provider detail")
			}
		})
	}
}
func TestInvalidClassNameSendsNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, className := range []string{"", "_Users", "9Class", "Class/Other", strings.Repeat("A", 129)} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).CountClassObjects(), testConnection, CountClassObjectsInput{ClassName: className})
		require.NoError(t, err)
		require.Equal(t, CountClassObjectsBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}
func TestCountClassObjectsTransportAndSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"results":[],"count":42}`))
		require.NoError(t, err)
	}))
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {ApplicationID: sdkgo.NewSecretString(testApplicationID), RESTAPIKey: sdkgo.NewSecretString(testRESTAPIKey)}}
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 8}, credentials)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.CountClassObjects(), testConnection, CountClassObjectsInput{ClassName: "GameScore"})
	require.NoError(t, err)
	require.Equal(t, CountClassObjectsBranchInvalidResponse, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).CountClassObjects(), testConnection, CountClassObjectsInput{ClassName: "GameScore"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testRESTAPIKey)
}
