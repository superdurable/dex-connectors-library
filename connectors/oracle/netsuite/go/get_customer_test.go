// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oraclenetsuite

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "oracle-netsuite", Name: "oracle-netsuite"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "oracle-netsuite-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-customer" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func testClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	credentials := Credentials{
		OAuthClientID: "test-client", OAuthClientSecret: sdkgo.NewSecretString("test-secret"),
		AccessToken: sdkgo.NewSecretString("test-access-token"), RefreshToken: sdkgo.NewSecretString("test-refresh-token"),
	}
	client, err := New(Config{BaseURL: serverURL, AuthorizationEndpoint: serverURL + "/authorize", TokenEndpoint: serverURL + "/token"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetCustomerReadsOnlySelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/services/rest/record/v1/customer/107", request.URL.Path)
		require.Equal(t, "Bearer test-access-token", request.Header.Get("Authorization"))
		require.Empty(t, request.URL.Query().Get("access_token"))
		_, err := writer.Write([]byte(`{"id":"107","entityId":"CUST107","private":"private"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).GetCustomer(), testConnection, GetCustomerInput{CustomerID: "107"})
	require.NoError(t, err)
	require.Equal(t, GetCustomerBranchFound, result.Branch)
	require.Equal(t, CustomerSummary{ID: "107", EntityID: "CUST107"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), "test-access-token")
}

func TestGetCustomerClassifiesProviderResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"missing", 404, `{}`, GetCustomerBranchNotFound, false},
		{"denied", 403, `{}`, GetCustomerBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"timeout", 408, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"invalid JSON", 200, `{`, GetCustomerBranchInvalidResponse, false},
		{"wrong customer", 200, `{"id":"108","entityId":"OTHER"}`, GetCustomerBranchInvalidResponse, false},
		{"missing customer ID", 200, `{"id":"107"}`, GetCustomerBranchInvalidResponse, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).GetCustomer(), testConnection, GetCustomerInput{CustomerID: "107"})
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

func TestGetCustomerRejectsUnsafeKeyBeforeProviderCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid customer internal ID reached provider")
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).GetCustomer(), testConnection, GetCustomerInput{CustomerID: "../other"})
	require.NoError(t, err)
	require.Equal(t, GetCustomerBranchDefect, result.Branch)
}

func TestGetCustomerRetriesTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := testClient(t, server.URL)
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, client.GetCustomer(), testConnection, GetCustomerInput{CustomerID: "107"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.False(t, errors.Is(err, context.Canceled))
}
