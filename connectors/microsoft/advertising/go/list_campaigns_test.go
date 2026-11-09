// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package microsoftadvertising

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "microsoft-advertising", Name: "microsoft-advertising"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "microsoft-advertising-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-campaigns" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func testClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	credentials := Credentials{
		OAuthClientID: "test-client", OAuthClientSecret: sdkgo.NewSecretString("test-secret"),
		DeveloperToken: sdkgo.NewSecretString("test-developer-token"),
		AccessToken:    sdkgo.NewSecretString("test-access-token"), RefreshToken: sdkgo.NewSecretString("test-refresh-token"),
	}
	client, err := New(Config{BaseURL: serverURL, CustomerID: "73456"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestListCampaignsReadsOnlySelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, listCampaignsPath, request.URL.Path)
		require.Equal(t, "Bearer test-access-token", request.Header.Get("Authorization"))
		require.Equal(t, "test-developer-token", request.Header.Get("DeveloperToken"))
		require.Equal(t, "73456", request.Header.Get("CustomerId"))
		require.Equal(t, "12345", request.Header.Get("CustomerAccountId"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		var payload map[string]string
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Equal(t, "12345", payload["AccountId"])
		require.Equal(t, allCampaignTypes, payload["CampaignType"])
		_, err = writer.Write([]byte(`{"Campaigns":[{"Id":"98765","Name":"Search launch","CampaignType":"Search","Status":"Active","BudgetId":"private"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).ListCampaigns(), testConnection, ListCampaignsInput{AccountID: "12345"})
	require.NoError(t, err)
	require.Equal(t, ListCampaignsBranchListed, result.Branch)
	require.Equal(t, CampaignsPage{Campaigns: []CampaignSummary{{ID: "98765", Name: "Search launch", CampaignType: "Search", Status: "Active"}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), "test-access-token")
	require.NotContains(t, string(encoded), "test-developer-token")
}

func TestListCampaignsClassifiesProviderResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"Campaigns":[]}`, ListCampaignsBranchListed, false},
		{"denied", 403, `{}`, ListCampaignsBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"timeout", 408, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"invalid JSON", 200, `{`, ListCampaignsBranchInvalidResponse, false},
		{"missing campaigns", 200, `{}`, ListCampaignsBranchInvalidResponse, false},
		{"incomplete campaign", 200, `{"Campaigns":[{"Id":"98765"}]}`, ListCampaignsBranchInvalidResponse, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).ListCampaigns(), testConnection, ListCampaignsInput{AccountID: "12345"})
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

func TestListCampaignsRejectsUnsafeAccountIDBeforeProviderCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("invalid account ID reached provider") }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).ListCampaigns(), testConnection, ListCampaignsInput{AccountID: "../other"})
	require.NoError(t, err)
	require.Equal(t, ListCampaignsBranchDefect, result.Branch)
}

func TestListCampaignsRetriesTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := testClient(t, server.URL)
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, client.ListCampaigns(), testConnection, ListCampaignsInput{AccountID: "12345"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.False(t, errors.Is(err, context.Canceled))
}
