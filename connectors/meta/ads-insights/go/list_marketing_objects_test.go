// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package metaadsinsights

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListMarketingObjectsReadsCollections(t *testing.T) {
	for _, testCase := range []struct {
		name, collection string
		invoke           func(*Client) sdkgo.Query[ListMarketingObjectsInput, MarketingObjectsPage]
	}{
		{"ads", "ads", func(client *Client) sdkgo.Query[ListMarketingObjectsInput, MarketingObjectsPage] {
			return client.ListAds()
		}},
		{"adsets", "adsets", func(client *Client) sdkgo.Query[ListMarketingObjectsInput, MarketingObjectsPage] {
			return client.ListAdSets()
		}},
		{"campaigns", "campaigns", func(client *Client) sdkgo.Query[ListMarketingObjectsInput, MarketingObjectsPage] {
			return client.ListCampaigns()
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "/act_123456789012345/"+testCase.collection, request.URL.Path)
				require.Equal(t, "id,name,status,effective_status", request.URL.Query().Get("fields"))
				require.Equal(t, "25", request.URL.Query().Get("limit"))
				require.Equal(t, "cursorABC", request.URL.Query().Get("after"))
				require.Equal(t, "Bearer synthetic-marketing-token", request.Header.Get("Authorization"))
				require.Empty(t, request.URL.Query().Get("access_token"))
				_, err := writer.Write([]byte(`{"data":[{"id":"223456789012345","name":"Spring","status":"ACTIVE","effective_status":"ACTIVE"}],"paging":{"next":"https://graph.facebook.com/next","cursors":{"after":"cursorDEF"}}}`))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, testCase.invoke(newTestClient(t, server.URL)), testConnection, ListMarketingObjectsInput{AccountID: "123456789012345", Limit: 25, After: "cursorABC"})
			require.NoError(t, err)
			require.Len(t, result.Value.Objects, 1)
			require.Equal(t, "223456789012345", result.Value.Objects[0].ID)
			require.Equal(t, "cursorDEF", result.Value.NextCursor)
		})
	}
}

func TestListAdsClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{404, `{}`, ListAdsBranchNotFound, false},
		{403, `{}`, ListAdsBranchProviderRejected, false},
		{200, `{}`, ListAdsBranchInvalidResponse, false},
		{429, `{}`, "", true},
		{503, `{}`, "", true},
	} {
		t.Run(http.StatusText(testCase.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListAds(), testConnection, ListMarketingObjectsInput{AccountID: "123456789012345"})
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

func TestListAdsRejectsInvalidAccountID(t *testing.T) {
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, "https://graph.facebook.com/v25.0").ListAds(), testConnection, ListMarketingObjectsInput{AccountID: "act_123"})
	require.NoError(t, err)
	require.Equal(t, ListAdsBranchDefect, result.Branch)
}
