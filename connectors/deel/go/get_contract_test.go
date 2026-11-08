// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package deel

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

var testConnection = sdkgo.ConnectionRef{Provider: "deel", Name: "deel"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "deel-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-contract" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIToken: sdkgo.NewSecretString("sample-deel-api-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetContractReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/contracts/123e4567-e89b-12d3-a456-426614174000", request.URL.Path)
		require.Equal(t, "Bearer sample-deel-api-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"data":{"id":"123e4567-e89b-12d3-a456-426614174000","status":"in_progress","type":"ongoing_time_based","customer_id":"private"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetContract(), testConnection, GetContractInput{ContractID: "123e4567-e89b-12d3-a456-426614174000"})
	require.NoError(t, err)
	require.Equal(t, GetContractBranchFound, result.Branch)
	require.Equal(t, ContractStatus{ID: "123e4567-e89b-12d3-a456-426614174000", Status: "in_progress", Type: "ongoing_time_based"}, result.Value)
}

func TestGetContractClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetContractBranchNotFound},
		{name: "forbidden", status: 403, branch: GetContractBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"data":{"id":"other","status":"in_progress","type":"ongoing_time_based"}}`, branch: GetContractBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetContract(), testConnection, GetContractInput{ContractID: "123e4567-e89b-12d3-a456-426614174000"})
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

func TestGetContractRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api-staging.letsdeel.com/rest", 0)
	for _, contractID := range []string{"", "../other", "bad\ncontract", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetContract(), testConnection, GetContractInput{ContractID: contractID})
		require.NoError(t, err)
		require.Equal(t, GetContractBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"data":{"id":"123e4567-e89b-12d3-a456-426614174000","status":"in_progress","type":"ongoing_time_based"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 20).GetContract(), testConnection, GetContractInput{ContractID: "123e4567-e89b-12d3-a456-426614174000"})
	require.NoError(t, err)
	require.Equal(t, GetContractBranchInvalidResponse, result.Branch)
}
