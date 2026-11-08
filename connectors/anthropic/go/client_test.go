// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "sk-ant-test-no-real-secret"

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "anthropic-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-model" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "anthropic", Name: "anthropic"}

func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	})
	require.NoError(t, err)
	return client
}

func TestGetModelRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/models/claude-sonnet-4-6", request.URL.Path)
		require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
		require.Equal(t, anthropicAPIVersion, request.Header.Get("anthropic-version"))
		require.NotContains(t, request.URL.String(), testAPIKey)
		_, err := fmt.Fprint(response, `{"id":"claude-sonnet-4-6","display_name":"Claude Sonnet 4.6","created_at":"2026-02-17T00:00:00Z","type":"model"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetModel(), testConnection, GetModelInput{ModelID: "claude-sonnet-4-6"})
	require.NoError(t, err)
	require.Equal(t, GetModelBranchFound, result.Branch)
	require.Equal(t, ModelInfo{ID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6", CreatedAt: "2026-02-17T00:00:00Z"}, result.Value)
}

func TestGetModelClassifiesProviderFailures(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"missing", 404, GetModelBranchNotFound, sdkgo.FailureNotFound, false},
		{"unauthorized", 401, GetModelBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, GetModelBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"bad request", 400, GetModelBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"rate limit", 429, "", sdkgo.FailureRateLimit, true},
		{"unavailable", 503, "", sdkgo.FailureAvailability, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(testCase.status)
				_, err := fmt.Fprint(response, `{"error":"SENTINEL"}`)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetModel(), testConnection, GetModelInput{ModelID: "claude-sonnet-4-6"})
			if testCase.retry {
				require.Error(t, err)
				require.Contains(t, err.Error(), string(testCase.kind))
				require.NotContains(t, err.Error(), "SENTINEL")
				require.NotContains(t, err.Error(), testAPIKey)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
		})
	}
}

func TestGetModelRejectsInvalidInputBeforeRequest(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requestCount++
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	for _, modelID := range []string{"", "../secret", "bad?query", strings.Repeat("x", 201)} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetModel(), testConnection, GetModelInput{ModelID: modelID})
		require.NoError(t, err)
		require.Equal(t, GetModelBranchDefect, result.Branch)
	}
	require.Zero(t, requestCount)
}

func TestGetModelRejectsMalformedAndOversizedResponses(t *testing.T) {
	for _, responseBody := range []string{`{"id":"model","type":"model"}`, `{"id":"model","display_name":"Name","created_at":"bad","type":"model"}`, strings.Repeat("a", 1048577)} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			_, err := fmt.Fprint(response, responseBody)
			require.NoError(t, err)
		}))
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetModel(), testConnection, GetModelInput{ModelID: "model"})
		require.NoError(t, err)
		require.Equal(t, GetModelBranchInvalidResponse, result.Branch)
		server.Close()
	}
}
