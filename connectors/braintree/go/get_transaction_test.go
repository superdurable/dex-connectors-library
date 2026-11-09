// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braintree

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "braintree", Name: "braintree"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "braintree-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-transaction" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credential := Credentials{PublicKey: "sample-public-key", PrivateKey: sdkgo.NewSecretString("sample-private-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credential})
	require.NoError(t, err)
	return client
}
func TestGetTransactionReadsNode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/graphql", request.URL.Path)
		require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("sample-public-key:sample-private-key")), request.Header.Get("Authorization"))
		require.Equal(t, "2019-01-01", request.Header.Get("Braintree-Version"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		var fields struct {
			Query     string `json:"query"`
			Variables struct {
				ID string `json:"id"`
			} `json:"variables"`
		}
		require.NoError(t, json.Unmarshal(body, &fields))
		require.Equal(t, getTransactionGraphQL, fields.Query)
		require.Equal(t, "txn-1", fields.Variables.ID)
		_, err = io.WriteString(writer, `{"data":{"node":{"__typename":"Transaction","id":"txn-1","status":"SETTLED"}}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetTransaction(), testConnection, GetTransactionInput{TransactionID: "txn-1"})
	require.NoError(t, err)
	require.Equal(t, GetTransactionBranchFound, result.Branch)
	require.Equal(t, TransactionProfile{ID: "txn-1", Status: "SETTLED"}, result.Value)
}
func TestGetTransactionClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"not found", 200, `{"data":{"node":null}}`, GetTransactionBranchNotFound, false},
		{"other node", 200, `{"data":{"node":{"__typename":"Refund"}}}`, GetTransactionBranchNotFound, false},
		{"missing node type", 200, `{"data":{"node":{"id":"txn-1","status":"SETTLED"}}}`, GetTransactionBranchInvalidResponse, false},
		{"GraphQL error", 200, `{"errors":[{"message":"denied"}],"data":null}`, GetTransactionBranchProviderRejected, false},
		{"GraphQL authorization", 200, `{"errors":[{"message":"denied","extensions":{"errorClass":"AUTHORIZATION"}}],"data":null}`, GetTransactionBranchProviderRejected, false},
		{"GraphQL not found", 200, `{"errors":[{"message":"missing","extensions":{"errorClass":"NOT_FOUND"}}],"data":{"node":null}}`, GetTransactionBranchNotFound, false},
		{"GraphQL internal", 200, `{"errors":[{"message":"oops","extensions":{"errorClass":"INTERNAL"}}],"data":null}`, "", true},
		{"GraphQL service availability", 200, `{"errors":[{"message":"down","extensions":{"errorClass":"SERVICE_AVAILABILITY"}}],"data":null}`, "", true},
		{"GraphQL resource limit", 200, `{"errors":[{"message":"limit","extensions":{"errorClass":"RESOURCE_LIMIT"}}],"data":null}`, "", true},
		{"GraphQL rejection wins", 200, `{"errors":[{"extensions":{"errorClass":"INTERNAL"}},{"extensions":{"errorClass":"VALIDATION"}}],"data":null}`, GetTransactionBranchProviderRejected, false},
		{"GraphQL retry wins over not found", 200, `{"errors":[{"extensions":{"errorClass":"NOT_FOUND"}},{"extensions":{"errorClass":"INTERNAL"}}],"data":null}`, "", true},
		{"forbidden", 403, "", GetTransactionBranchProviderRejected, false},
		{"rate limit", 429, "", "", true},
		{"unavailable", 503, "", "", true},
		{"malformed", 200, `{"data":{"node":{"__typename":"Transaction","id":"wrong","status":"SETTLED"}}}`, GetTransactionBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetTransaction(), testConnection, GetTransactionInput{TransactionID: "txn-1"})
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
func TestGetTransactionRejectsInputAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://payments.sandbox.braintree-api.com", 0)
	for _, input := range []GetTransactionInput{{}, {TransactionID: " txn-1"}, {TransactionID: "bad\nID"}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetTransaction(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, GetTransactionBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"data":{"node":{"__typename":"Transaction","id":"txn-1","status":"SETTLED"}}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).GetTransaction(), testConnection, GetTransactionInput{TransactionID: "txn-1"})
	require.NoError(t, err)
	require.Equal(t, GetTransactionBranchInvalidResponse, result.Branch)
}
