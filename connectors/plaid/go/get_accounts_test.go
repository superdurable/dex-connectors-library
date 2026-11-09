// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package plaid

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "plaid", Name: "plaid"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "plaid-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-accounts" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{
		ClientID:    sdkgo.NewSecretString("sample-plaid-client-id"),
		Secret:      sdkgo.NewSecretString("sample-plaid-secret"),
		AccessToken: sdkgo.NewSecretString("sample-plaid-access-token"),
	}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func readRequestBody(t *testing.T, request *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	return string(body)
}

func TestGetAccountsReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/accounts/get", request.URL.Path)
		require.Equal(t, "sample-plaid-client-id", request.Header.Get("PLAID-CLIENT-ID"))
		require.Equal(t, "sample-plaid-secret", request.Header.Get("PLAID-SECRET"))
		require.JSONEq(t, `{"access_token":"sample-plaid-access-token"}`, readRequestBody(t, request))
		_, err := writer.Write([]byte(`{"item":{"item_id":"item_123456"},"accounts":[{"account_id":"account_123456","type":"depository","subtype":"checking","name":"Private Checking","mask":"1234"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetAccounts(), testConnection, GetAccountsInput{ItemID: "item_123456"})
	require.NoError(t, err)
	require.Equal(t, GetAccountsBranchFound, result.Branch)
	require.Equal(t, AccountsSnapshot{ItemID: "item_123456", Accounts: []AccountSummary{{ID: "account_123456", Type: "depository", Subtype: "checking"}}}, result.Value)
}

func TestGetAccountsClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetAccountsBranchNotFound},
		{name: "item not found", status: 400, body: `{"error_type":"ITEM_ERROR","error_code":"ITEM_NOT_FOUND","error_message":"The Item you requested cannot be found."}`, branch: GetAccountsBranchNotFound},
		{name: "invalid access token", status: 400, body: `{"error_type":"INVALID_INPUT","error_code":"INVALID_ACCESS_TOKEN","error_message":"could not find matching access token"}`, branch: GetAccountsBranchProviderRejected},
		{name: "forbidden", status: 403, branch: GetAccountsBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"item":{"item_id":"other"},"accounts":[]}`, branch: GetAccountsBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetAccounts(), testConnection, GetAccountsInput{ItemID: "item_123456"})
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

func TestGetAccountsRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://sandbox.plaid.com", 0)
	for _, accountsID := range []string{"", "../other", "bad\naccounts", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetAccounts(), testConnection, GetAccountsInput{ItemID: accountsID})
		require.NoError(t, err)
		require.Equal(t, GetAccountsBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"item":{"item_id":"item_123456"},"accounts":[{"account_id":"account_123456","type":"depository","subtype":"checking"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 20).GetAccounts(), testConnection, GetAccountsInput{ItemID: "item_123456"})
	require.NoError(t, err)
	require.Equal(t, GetAccountsBranchInvalidResponse, result.Branch)
}
