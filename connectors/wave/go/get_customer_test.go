// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wave

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "wave", Name: "wave"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "wave-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-customer" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("sample-wave-access-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetCustomerReadsSelectedProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/graphql/public", request.URL.Path)
		require.Equal(t, "Bearer sample-wave-access-token", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		var fields struct {
			Query     string               `json:"query"`
			Variables getCustomerVariables `json:"variables"`
		}
		require.NoError(t, json.Unmarshal(body, &fields))
		require.Equal(t, getCustomerGraphQL, fields.Query)
		require.Equal(t, getCustomerVariables{BusinessID: "business-1", CustomerID: "customer-1"}, fields.Variables)
		_, err = writer.Write([]byte(`{"data":{"business":{"customer":{"id":"customer-1","name":"Example Customer"}}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetCustomer(), testConnection, GetCustomerInput{BusinessID: "business-1", CustomerID: "customer-1"})
	require.NoError(t, err)
	require.Equal(t, GetCustomerBranchFound, result.Branch)
	require.Equal(t, CustomerProfile{BusinessID: "business-1", ID: "customer-1", Name: "Example Customer"}, result.Value)
}

func TestGetCustomerClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "not found", status: 200, body: `{"data":{"business":{"customer":null}}}`, branch: GetCustomerBranchNotFound},
		{name: "forbidden", status: 403, branch: GetCustomerBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "GraphQL error", status: 200, body: `{"errors":[{"message":"invalid query"}],"data":null}`, branch: GetCustomerBranchInvalidResponse},
		{name: "malformed", status: 200, body: `{"data":{"business":{"customer":{"id":"wrong","name":"Example"}}}}`, branch: GetCustomerBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetCustomer(), testConnection, GetCustomerInput{BusinessID: "business-1", CustomerID: "customer-1"})
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

func TestGetCustomerRejectsInvalidInputAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://gql.waveapps.com", 0)
	for _, input := range []GetCustomerInput{{}, {BusinessID: " business-1", CustomerID: "customer-1"}, {BusinessID: "business-1", CustomerID: "bad\nID"}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetCustomer(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, GetCustomerBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"data":{"business":{"customer":{"id":"customer-1","name":"Example Customer"}}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).GetCustomer(), testConnection, GetCustomerInput{BusinessID: "business-1", CustomerID: "customer-1"})
	require.NoError(t, err)
	require.Equal(t, GetCustomerBranchInvalidResponse, result.Branch)
}
