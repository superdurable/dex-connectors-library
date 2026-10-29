// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testAPIKey = "test-secret-key"

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func TestListProjectsRequestAndClassification(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"success", 200, `{"projects":[{"id":3001,"name":"Meridian Website","is_active":true}],"links":{"next":null}}`, ListProjectsBranchListed, false},
		{"unauthorized", 401, `{"error":"SENTINEL"}`, ListProjectsBranchProviderRejected, false},
		{"rate limit", 429, `{"error":"SENTINEL"}`, "", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "/projects", request.URL.Path)
				require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
				require.Equal(t, "123", request.Header.Get("Harvest-Account-Id"))
				require.NotEmpty(t, request.Header.Get("User-Agent"))
				response.WriteHeader(testCase.status)
				_, err := response.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "harvest", Name: "harvest"}
			client, err := New(Config{BaseURL: server.URL, AccountID: "123"}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.ListProjects(), reference, ListProjectsInput{})
			if testCase.retry {
				require.Error(t, err)
				require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
				require.NotContains(t, err.Error(), "SENTINEL")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if testCase.status == 200 {
				require.Len(t, result.Value.Projects, 1)
			} else {
				require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}

func TestListTimeEntriesRequestAndClassification(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"success", 200, `{"time_entries":[{"id":4001,"project":{"id":3001},"hours":2.5,"spent_date":"2026-10-28"}],"links":{"next":null}}`, ListTimeEntriesBranchListed, false},
		{"unauthorized", 401, `{"error":"SENTINEL"}`, ListTimeEntriesBranchProviderRejected, false},
		{"rate limit", 429, `{"error":"SENTINEL"}`, "", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "/time_entries", request.URL.Path)
				require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
				require.Equal(t, "123", request.Header.Get("Harvest-Account-Id"))
				require.NotEmpty(t, request.Header.Get("User-Agent"))
				response.WriteHeader(testCase.status)
				_, err := response.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "harvest", Name: "harvest"}
			client, err := New(Config{BaseURL: server.URL, AccountID: "123"}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.ListTimeEntries(), reference, ListTimeEntriesInput{})
			if testCase.retry {
				require.Error(t, err)
				require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
				require.NotContains(t, err.Error(), "SENTINEL")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if testCase.status == 200 {
				require.Len(t, result.Value.TimeEntries, 1)
			} else {
				require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}
