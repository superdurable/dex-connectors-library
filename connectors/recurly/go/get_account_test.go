// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recurly

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "recurly", Name: "recurly"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "recurly-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-account" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sample-recurly-api-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetAccountReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/accounts/e28zov4fw0v2", request.URL.Path)
		require.Equal(t, "Basic c2FtcGxlLXJlY3VybHktYXBpLWtleTo=", request.Header.Get("Authorization"))
		require.Equal(t, "application/vnd.recurly.v2021-02-25+json", request.Header.Get("Accept"))
		require.Equal(t, "application/vnd.recurly.v2021-02-25+json", request.Header.Get("Accept"))
		_, err := writer.Write([]byte(`{"id":"e28zov4fw0v2","code":"acme-corp","email":"private@example.com"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetAccount(), testConnection, GetAccountInput{AccountID: "e28zov4fw0v2"})
	require.NoError(t, err)
	require.Equal(t, GetAccountBranchFound, result.Branch)
	require.Equal(t, AccountIdentity{ID: "e28zov4fw0v2", Code: "acme-corp"}, result.Value)
}

func TestGetAccountClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetAccountBranchNotFound},
		{name: "forbidden", status: 403, branch: GetAccountBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"id":"other","code":"acme-corp"}`, branch: GetAccountBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetAccount(), testConnection, GetAccountInput{AccountID: "e28zov4fw0v2"})
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

func TestGetAccountRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	require.True(t, validAccountID(strings.Repeat("a", 50)))
	require.True(t, validAccountID("code-"+strings.Repeat("a", 50)))
	client := newTestClient(t, "https://v3.recurly.com", 0)
	for _, accountID := range []string{"", "code-", "../other", "bad\naccount", string([]byte{0xff}), strings.Repeat("a", 51), "code-" + strings.Repeat("a", 51)} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetAccount(), testConnection, GetAccountInput{AccountID: accountID})
		require.NoError(t, err)
		require.Equal(t, GetAccountBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"id":"e28zov4fw0v2","code":"acme-corp"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 20).GetAccount(), testConnection, GetAccountInput{AccountID: "e28zov4fw0v2"})
	require.NoError(t, err)
	require.Equal(t, GetAccountBranchInvalidResponse, result.Branch)
}
