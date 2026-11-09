// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workday

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "workday", Name: "workday"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "workday-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-worker" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func testClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	credentials := Credentials{
		OAuthClientID: "test-client", OAuthClientSecret: sdkgo.NewSecretString("test-secret"),
		AccessToken: sdkgo.NewSecretString("test-access-token"), RefreshToken: sdkgo.NewSecretString("test-refresh-token"),
	}
	client, err := New(Config{BaseURL: serverURL + "/api/common/v1/demo", AuthorizationEndpoint: serverURL + "/authorize", TokenEndpoint: serverURL + "/token", Scopes: []string{"Staffing"}}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetWorkerReadsOnlySelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/common/v1/demo/workers/0123456789abcdef0123456789abcdef", request.URL.Path)
		require.Equal(t, "Bearer test-access-token", request.Header.Get("Authorization"))
		require.Empty(t, request.URL.Query().Get("access_token"))
		_, err := writer.Write([]byte(`{"id":"0123456789abcdef0123456789abcdef","descriptor":"Ada Lovelace","private":"private"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).GetWorker(), testConnection, GetWorkerInput{WorkerID: "0123456789abcdef0123456789abcdef"})
	require.NoError(t, err)
	require.Equal(t, GetWorkerBranchFound, result.Branch)
	require.Equal(t, WorkerSummary{ID: "0123456789abcdef0123456789abcdef", Descriptor: "Ada Lovelace"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), "test-access-token")
}

func TestGetWorkerClassifiesProviderResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"missing", 404, `{}`, GetWorkerBranchNotFound, false},
		{"denied", 403, `{}`, GetWorkerBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"timeout", 408, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"invalid JSON", 200, `{`, GetWorkerBranchInvalidResponse, false},
		{"wrong worker", 200, `{"id":"fedcba9876543210fedcba9876543210","descriptor":"Wrong Worker"}`, GetWorkerBranchInvalidResponse, false},
		{"missing worker ID", 200, `{"id":"0123456789abcdef0123456789abcdef"}`, GetWorkerBranchInvalidResponse, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).GetWorker(), testConnection, GetWorkerInput{WorkerID: "0123456789abcdef0123456789abcdef"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestGetWorkerRejectsUnsafeKeyBeforeProviderCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid worker WID reached provider")
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).GetWorker(), testConnection, GetWorkerInput{WorkerID: "../other"})
	require.NoError(t, err)
	require.Equal(t, GetWorkerBranchDefect, result.Branch)
}

func TestGetWorkerRetriesTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := testClient(t, server.URL)
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, client.GetWorker(), testConnection, GetWorkerInput{WorkerID: "0123456789abcdef0123456789abcdef"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.False(t, errors.Is(err, context.Canceled))
}
