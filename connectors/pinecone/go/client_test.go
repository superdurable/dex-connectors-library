// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pinecone

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type testContext struct{ dex.Context }

func (testContext) FlowID() string              { return "provider-test" }
func (testContext) StepExecutionID() string     { return "provider-step" }
func (testContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (testContext) Done() <-chan struct{}       { return nil }
func (testContext) Err() error                  { return nil }
func (testContext) Value(key any) any           { return context.Background().Value(key) }

func TestUpsertVectorsHTTPAndRejection(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"happy", 200, `{"upsertedCount":1}`, UpsertVectorsBranchUpserted},
		{"rejected", 401, `{"message":"SECRET SENTINEL"}`, UpsertVectorsBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/vectors/upsert", r.URL.Path)
				require.Equal(t, "test-token", r.Header.Get("Api-Key"))
				require.NotContains(t, r.URL.String(), "test-token")
				require.Equal(t, "2025-04", r.Header.Get("X-Pinecone-Api-Version"))
				w.WriteHeader(test.status)
				_, err := fmt.Fprint(w, test.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "pinecone", Name: "pinecone"}
			client, err := New(Config{BaseURL: server.URL + ""}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}})
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(testContext{}, client.UpsertVectors(), reference, UpsertVectorsInput{Namespace: "example", Vectors: []Vector{{ID: "vec-meridian", Values: []float32{0.1, 0.2}}}})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.name == "rejected" {
				require.NotNil(t, result.Failure)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}

func TestQueryVectorsHTTPAndRejection(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"happy", 200, `{"matches":[{"id":"vec-meridian","score":0.98}],"namespace":"example"}`, QueryVectorsBranchQueried},
		{"rejected", 401, `{"message":"SECRET SENTINEL"}`, QueryVectorsBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/query", r.URL.Path)
				require.Equal(t, "test-token", r.Header.Get("Api-Key"))
				require.NotContains(t, r.URL.String(), "test-token")
				require.Equal(t, "2025-04", r.Header.Get("X-Pinecone-Api-Version"))
				w.WriteHeader(test.status)
				_, err := fmt.Fprint(w, test.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "pinecone", Name: "pinecone"}
			client, err := New(Config{BaseURL: server.URL + ""}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(testContext{}, client.QueryVectors(), reference, QueryVectorsInput{Namespace: "example", Vector: []float32{0.1, 0.2}, TopK: 3})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.name == "rejected" {
				require.NotNil(t, result.Failure)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}

func TestUpsertVectorsRejectsDuplicateIDs(t *testing.T) {
	_, _, _, err := encodeUpsertVectorsInput(UpsertVectorsInput{Vectors: []Vector{
		{ID: "same", Values: []float32{0.1}}, {ID: "same", Values: []float32{0.2}},
	}})
	require.ErrorContains(t, err, "unique")
}
