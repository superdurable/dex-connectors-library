// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lever

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

var testConnection = sdkgo.ConnectionRef{Provider: "lever", Name: "lever"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "lever-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-opportunity" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sample-lever-api-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetOpportunityReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/opportunities/250d8f03-738a-4bba-a671-8a3d73477145", request.URL.Path)
		require.Equal(t, "Basic c2FtcGxlLWxldmVyLWFwaS1rZXk6", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"data":{"id":"250d8f03-738a-4bba-a671-8a3d73477145","name":"Sample Candidate","stage":"00922a60-7c15-422b-b086-f62000824fd7","emails":["private@example.com"]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetOpportunity(), testConnection, GetOpportunityInput{OpportunityID: "250d8f03-738a-4bba-a671-8a3d73477145"})
	require.NoError(t, err)
	require.Equal(t, GetOpportunityBranchFound, result.Branch)
	require.Equal(t, OpportunityStage{ID: "250d8f03-738a-4bba-a671-8a3d73477145", Name: "Sample Candidate", Stage: "00922a60-7c15-422b-b086-f62000824fd7"}, result.Value)
}

func TestGetOpportunityClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetOpportunityBranchNotFound},
		{name: "forbidden", status: 403, branch: GetOpportunityBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"data":{"id":"other","name":"Sample Candidate","stage":"00922a60-7c15-422b-b086-f62000824fd7"}}`, branch: GetOpportunityBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetOpportunity(), testConnection, GetOpportunityInput{OpportunityID: "250d8f03-738a-4bba-a671-8a3d73477145"})
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

func TestGetOpportunityRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.lever.co/v1", 0)
	for _, opportunityID := range []string{"", "../other", "bad\nopportunity", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetOpportunity(), testConnection, GetOpportunityInput{OpportunityID: opportunityID})
		require.NoError(t, err)
		require.Equal(t, GetOpportunityBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"data":{"id":"250d8f03-738a-4bba-a671-8a3d73477145","name":"Sample Candidate","stage":"00922a60-7c15-422b-b086-f62000824fd7"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 20).GetOpportunity(), testConnection, GetOpportunityInput{OpportunityID: "250d8f03-738a-4bba-a671-8a3d73477145"})
	require.NoError(t, err)
	require.Equal(t, GetOpportunityBranchInvalidResponse, result.Branch)
}
