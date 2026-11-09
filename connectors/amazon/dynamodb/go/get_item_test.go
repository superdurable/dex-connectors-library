// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
package dynamodb

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "amazon-dynamodb", Name: "amazon-dynamodb"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "dynamodb-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-item" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AccessKeyID: sdkgo.NewSecretString("AKIADYNAMOCONNECTOR"), SecretAccessKey: sdkgo.NewSecretString("dynamodb-test-secret"), SessionToken: sdkgo.NewSecretString("")}
	client, err := New(Config{Region: "eu-west-1", Endpoint: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, signingTimeOption{now: func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }})
	require.NoError(t, err)
	return client
}
func exampleInput() GetItemInput {
	return GetItemInput{TableName: "Examples", Key: map[string]json.RawMessage{"pk": json.RawMessage(`{"S":"example"}`)}, ConsistentRead: true}
}
func TestGetItemSignsAndReadsItem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/", r.URL.Path)
		require.Equal(t, "application/x-amz-json-1.0", r.Header.Get("Content-Type"))
		require.Equal(t, "DynamoDB_20120810.GetItem", r.Header.Get("X-Amz-Target"))
		require.Equal(t, "20261030T000000Z", r.Header.Get("X-Amz-Date"))
		require.True(t, strings.Contains(r.Header.Get("Authorization"), "/eu-west-1/dynamodb/aws4_request"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"TableName":"Examples","Key":{"pk":{"S":"example"}},"ConsistentRead":true}`, string(body))
		_, err = w.Write([]byte(`{"Item":{"pk":{"S":"example"},"count":{"N":"3"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetItem(), testConnection, exampleInput())
	require.NoError(t, err)
	require.Equal(t, GetItemBranchFound, result.Branch)
	require.True(t, result.Value.Exists)
	require.JSONEq(t, `{"S":"example"}`, string(result.Value.Item["pk"]))
}
func TestGetItemClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"missing", 200, `{}`, GetItemBranchMissing, false},
		{"forbidden", 403, `{"__type":"AccessDeniedException"}`, GetItemBranchProviderRejected, false},
		{"throttled", 400, `{"__type":"com.amazonaws.dynamodb.v20120810#ProvisionedThroughputExceededException"}`, "", true},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"bad response", 200, `{"Item":{"pk":false}}`, GetItemBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetItem(), testConnection, exampleInput())
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestGetItemRejectsInvalidKeyAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://dynamodb.eu-west-1.amazonaws.com", 0)
	input := exampleInput()
	input.Key["pk"] = json.RawMessage(`{"L":[]}`)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetItem(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetItemBranchDefect, result.Branch)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(`{"Item":{"pk":{"S":"example"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 10).GetItem(), testConnection, exampleInput())
	require.NoError(t, err)
	require.Equal(t, GetItemBranchInvalidResponse, result.Branch)
}
