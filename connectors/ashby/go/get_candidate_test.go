// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "ashby", Name: "ashby"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "ashby-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-candidate" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sample-ashby-api-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetCandidateReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/candidate.info", request.URL.Path)
		require.Equal(t, "Basic c2FtcGxlLWFzaGJ5LWFwaS1rZXk6", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"success":true,"results":{"id":"e9ed20fd-d45f-4aad-8a00-a19bfba0083e","name":"Example Candidate","createdAt":"2024-01-15T10:30:00.000Z","updatedAt":"2024-01-16T10:30:00.000Z","primaryEmailAddress":{"value":"private@example.com"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetCandidate(), testConnection, GetCandidateInput{CandidateID: "e9ed20fd-d45f-4aad-8a00-a19bfba0083e"})
	require.NoError(t, err)
	require.Equal(t, GetCandidateBranchFound, result.Branch)
	require.Equal(t, CandidateProfile{ID: "e9ed20fd-d45f-4aad-8a00-a19bfba0083e", Name: "Example Candidate", CreatedAt: "2024-01-15T10:30:00.000Z", UpdatedAt: "2024-01-16T10:30:00.000Z"}, result.Value)
}

func TestGetCandidateMatchesIDWithoutCase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"success":true,"results":{"id":"e9ed20fd-d45f-4aad-8a00-a19bfba0083e","name":"Example Candidate","createdAt":"2024-01-15T10:30:00.000Z","updatedAt":"2024-01-16T10:30:00.000Z"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetCandidate(), testConnection, GetCandidateInput{CandidateID: "E9ED20FD-D45F-4AAD-8A00-A19BFBA0083E"})
	require.NoError(t, err)
	require.Equal(t, GetCandidateBranchFound, result.Branch)
	require.Equal(t, "e9ed20fd-d45f-4aad-8a00-a19bfba0083e", result.Value.ID)
}

func TestGetCandidateClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetCandidateBranchNotFound},
		{name: "forbidden", status: 403, branch: GetCandidateBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"success":true,"results":{"id":"other","name":"Example Candidate","createdAt":"2024-01-15T10:30:00.000Z","updatedAt":"2024-01-16T10:30:00.000Z"}}`, branch: GetCandidateBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetCandidate(), testConnection, GetCandidateInput{CandidateID: "e9ed20fd-d45f-4aad-8a00-a19bfba0083e"})
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

func TestGetCandidateRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.ashbyhq.com", 0)
	for _, candidateID := range []string{"", "../other", "bad\ncandidate", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetCandidate(), testConnection, GetCandidateInput{CandidateID: candidateID})
		require.NoError(t, err)
		require.Equal(t, GetCandidateBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"success":true,"results":{"id":"e9ed20fd-d45f-4aad-8a00-a19bfba0083e","name":"Example Candidate","createdAt":"2024-01-15T10:30:00.000Z","updatedAt":"2024-01-16T10:30:00.000Z"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 20).GetCandidate(), testConnection, GetCandidateInput{CandidateID: "e9ed20fd-d45f-4aad-8a00-a19bfba0083e"})
	require.NoError(t, err)
	require.Equal(t, GetCandidateBranchInvalidResponse, result.Branch)
}
