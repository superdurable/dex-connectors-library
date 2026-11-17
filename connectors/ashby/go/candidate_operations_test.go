// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListCandidatesReadsOnePage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/candidate.list", request.URL.Path)
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "Basic c2FtcGxlLWFzaGJ5LWFwaS1rZXk6", request.Header.Get("Authorization"))
		var input map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&input))
		require.Equal(t, map[string]any{"cursor": "start", "limit": float64(25)}, input)
		_, err := writer.Write([]byte(`{"success":true,"results":[{"id":"e9ed20fd-d45f-4aad-8a00-a19bfba0083e","name":"Example Candidate","createdAt":"2024-01-15T10:30:00.000Z","updatedAt":"2024-01-16T10:30:00.000Z","primaryEmailAddress":{"value":"private@example.com"}}],"nextCursor":"next","moreDataAvailable":true}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).ListCandidates(), testConnection, ListCandidatesInput{Cursor: "start", Limit: 25})
	require.NoError(t, err)
	require.Equal(t, ListCandidatesBranchListed, result.Branch)
	require.Equal(t, "next", result.Value.NextCursor)
	require.Len(t, result.Value.Candidates, 1)
	require.NotContains(t, result.Value.Candidates[0].Name, "private@example.com")
}

func TestListCandidatesClassifiesFailures(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "rejected", status: 403, branch: ListCandidatesBranchProviderRejected},
		{name: "provider error", status: 200, body: `{"success":false,"errorInfo":{"code":"forbidden"}}`, branch: ListCandidatesBranchProviderRejected},
		{name: "invalid", status: 200, body: `{"success":true,"results":null}`, branch: ListCandidatesBranchInvalidResponse},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).ListCandidates(), testConnection, ListCandidatesInput{})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, "https://api.ashbyhq.com", 0).ListCandidates(), testConnection, ListCandidatesInput{Limit: 101})
	require.NoError(t, err)
	require.Equal(t, ListCandidatesBranchDefect, result.Branch)
}

func TestUpdateCandidateReplacesName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/candidate.update", request.URL.Path)
		var input UpdateCandidateInput
		require.NoError(t, json.NewDecoder(request.Body).Decode(&input))
		require.Equal(t, "Updated Candidate", input.Name)
		_, err := writer.Write([]byte(`{"success":true,"results":{"id":"e9ed20fd-d45f-4aad-8a00-a19bfba0083e","name":"Updated Candidate","createdAt":"2024-01-15T10:30:00.000Z","updatedAt":"2024-01-16T10:30:00.000Z"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL+"/api", 0).UpdateCandidate(), testConnection, UpdateCandidateInput{CandidateID: "e9ed20fd-d45f-4aad-8a00-a19bfba0083e", Name: "Updated Candidate"})
	require.NoError(t, err)
	require.Equal(t, UpdateCandidateBranchUpdated, result.Branch)
	require.Equal(t, "Updated Candidate", result.Value.Name)
}

func TestUpdateCandidateClassifiesFailures(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: UpdateCandidateBranchNotFound},
		{name: "rejected", status: 403, branch: UpdateCandidateBranchProviderRejected},
		{name: "provider missing", status: 200, body: `{"success":false,"errorInfo":{"code":"candidate_not_found"}}`, branch: UpdateCandidateBranchNotFound},
		{name: "invalid", status: 200, body: `{"success":true,"results":{}}`, branch: UpdateCandidateBranchInvalidResponse},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL+"/api", 0).UpdateCandidate(), testConnection, UpdateCandidateInput{CandidateID: "e9ed20fd-d45f-4aad-8a00-a19bfba0083e", Name: "Updated Candidate"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.False(t, strings.Contains(result.Failure.Message, "sample-ashby-api-key"))
		})
	}
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, "https://api.ashbyhq.com", 0).UpdateCandidate(), testConnection, UpdateCandidateInput{CandidateID: "invalid", Name: "Updated Candidate"})
	require.NoError(t, err)
	require.Equal(t, UpdateCandidateBranchDefect, result.Branch)
}
