// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cohere

import (
	"context"
	"encoding/json"
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

func TestEmbedRequestAndClassification(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		retry  bool
	}{
		{"success", 200, `{"id":"embed_1","embeddings":{"float":[[0.1,0.2]]}}`, false},
		{"unauthorized", 401, `{"message":"SENTINEL"}`, false},
		{"rate limit", 429, `{"message":"SENTINEL"}`, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodPost, request.Method)
				require.Equal(t, "/v2/embed", request.URL.Path)
				require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
				var payload struct {
					EmbeddingTypes []string `json:"embedding_types"`
					Texts          []string `json:"texts"`
					InputType      string   `json:"input_type"`
				}
				require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
				require.Equal(t, []string{"float"}, payload.EmbeddingTypes)
				require.Equal(t, []string{"hello"}, payload.Texts)
				require.Equal(t, "search_query", payload.InputType)
				response.WriteHeader(testCase.status)
				_, err := response.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "cohere", Name: "cohere"}
			client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.Embed(), reference, EmbedInput{Model: "embed-v4.0", Texts: []string{"hello"}, InputType: "search_query"})
			if testCase.retry {
				require.Error(t, err)
				require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
				require.NotContains(t, err.Error(), "SENTINEL")
				return
			}
			require.NoError(t, err)
			if testCase.status == 200 {
				require.Equal(t, EmbedBranchEmbedded, result.Branch)
				require.Len(t, result.Value.Embeddings, 1)
			} else {
				require.Equal(t, EmbedBranchProviderRejected, result.Branch)
				require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			}
		})
	}
}
func TestRerankRequestAndClassification(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		retry  bool
	}{
		{"success", 200, `{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.1}]}`, false},
		{"unauthorized", 401, `{"message":"SENTINEL"}`, false},
		{"unavailable", 503, `{"message":"SENTINEL"}`, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodPost, request.Method)
				require.Equal(t, "/v2/rerank", request.URL.Path)
				require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
				var payload struct {
					Query     string   `json:"query"`
					Documents []string `json:"documents"`
				}
				require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
				require.Equal(t, "release", payload.Query)
				require.Equal(t, []string{"weather", "release note"}, payload.Documents)
				response.WriteHeader(testCase.status)
				_, err := response.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "cohere", Name: "cohere"}
			client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.Rerank(), reference, RerankInput{Model: "rerank-v5.0-pro", Query: "release", Documents: []string{"weather", "release note"}})
			if testCase.retry {
				require.Error(t, err)
				require.Contains(t, err.Error(), string(sdkgo.FailureAvailability))
				require.NotContains(t, err.Error(), "SENTINEL")
				return
			}
			require.NoError(t, err)
			if testCase.status == 200 {
				require.Equal(t, RerankBranchRanked, result.Branch)
				require.Equal(t, 1, result.Value.Results[0].Index)
			} else {
				require.Equal(t, RerankBranchProviderRejected, result.Branch)
				require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			}
		})
	}
}
