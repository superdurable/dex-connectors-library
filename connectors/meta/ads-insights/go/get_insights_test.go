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

func TestGetInsightsReadsCampaignMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/987654321012345/insights", request.URL.Path)
		require.Equal(t, "last_7d", request.URL.Query().Get("date_preset"))
		require.Equal(t, "25", request.URL.Query().Get("limit"))
		require.Equal(t, "cursorABC", request.URL.Query().Get("after"))
		require.Equal(t, "Bearer synthetic-marketing-token", request.Header.Get("Authorization"))
		require.Empty(t, request.URL.Query().Get("access_token"))
		_, err := writer.Write([]byte(`{"data":[{"account_id":"123456789012345","campaign_id":"987654321012345","date_start":"2026-11-01","date_stop":"2026-11-07","impressions":"361324","clicks":"7346","spend":"5339.5"}],"paging":{"next":"https://graph.facebook.com/next","cursors":{"after":"cursorDEF"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetInsights(), testConnection, GetInsightsInput{ObjectID: "987654321012345", DatePreset: "last_7d", Limit: 25, After: "cursorABC"})
	require.NoError(t, err)
	require.Equal(t, GetInsightsBranchListed, result.Branch)
	require.Equal(t, "cursorDEF", result.Value.NextCursor)
	require.Equal(t, "361324", result.Value.Rows[0].Impressions)
}

func TestGetInsightsClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{{404, `{}`, GetInsightsBranchNotFound, false}, {403, `{}`, GetInsightsBranchProviderRejected, false}, {200, `{}`, GetInsightsBranchInvalidResponse, false}, {429, `{}`, "", true}, {503, `{}`, "", true}} {
		t.Run(http.StatusText(testCase.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetInsights(), testConnection, GetInsightsInput{ObjectID: "987654321012345", DatePreset: "last_7d", Limit: 25, After: "cursorABC"})
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

func TestGetInsightsRejectsInvalidObjectID(t *testing.T) {
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, "https://graph.facebook.com/v25.0").GetInsights(), testConnection, GetInsightsInput{ObjectID: "../other"})
	require.NoError(t, err)
	require.Equal(t, GetInsightsBranchDefect, result.Branch)
}

func TestGetInsightsRejectsInvalidMetrics(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		impressions string
		clicks      string
		spend       string
	}{
		{"nonnumeric impressions", "many", "3", "1.25"},
		{"negative impressions", "-1", "3", "1.25"},
		{"nonnumeric clicks", "4", "three", "1.25"},
		{"negative clicks", "4", "-3", "1.25"},
		{"nonnumeric spend", "4", "3", "USD 1.25"},
		{"negative spend", "4", "3", "-1.25"},
		{"incomplete decimal spend", "4", "3", "1."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				response := `{"data":[{"account_id":"123","date_start":"2026-11-01","date_stop":"2026-11-07","impressions":"` + testCase.impressions + `","clicks":"` + testCase.clicks + `","spend":"` + testCase.spend + `"}]}`
				_, err := writer.Write([]byte(response))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetInsights(), testConnection, GetInsightsInput{ObjectID: "987654321012345"})
			require.NoError(t, err)
			require.Equal(t, GetInsightsBranchInvalidResponse, result.Branch)
		})
	}
}
