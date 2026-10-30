// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func dataSourceSearchJSON(id string, title string) string {
	return fmt.Sprintf(`{"object":"data_source","id":%q,"title":[{"type":"text","plain_text":%q}],`+
		`"parent":{"type":"database_id","database_id":%q},"url":"https://app.notion.com/p/%s","last_edited_time":"2026-09-29T10:00:00.000Z","in_trash":false}`,
		id, title, testDatabaseID, id)
}

func TestSearchSendsAFilteredBoundedRequestAndReturnsMatches(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"object":"list","type":"page_or_data_source","page_or_data_source":{},`+
			`"results":[`+dataSourceSearchJSON(testDataSourceID, "Form submissions")+`,`+pageJSON(testPageID, "Form submissions guide")+
			`,{"object":"agent","id":"4c6f8102-3d5e-4f70-8192-334455667788"}],"next_cursor":"cursor-2","has_more":true}`)
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newNotionDexContext("search"), client.Search(), notionConnection, notion.SearchInput{
		Query: "  Form submissions ", ObjectType: notion.SearchObjectDataSource, SortDirection: notion.SortDescending, PageSize: 10,
	})
	require.NoError(t, err)
	require.Equal(t, notion.SearchBranchSearched, result.Branch)
	require.Equal(t, notion.SearchOutput{
		Matches: []notion.SearchMatch{
			{
				ObjectType: notion.SearchObjectDataSource, ID: testDataSourceID, Title: "Form submissions",
				URL: "https://app.notion.com/p/" + testDataSourceID, Parent: notion.Parent{Type: notion.ParentTypeDatabase, ID: testDatabaseID},
				LastEditedTime: "2026-09-29T10:00:00.000Z",
			},
			{
				ObjectType: notion.SearchObjectPage, ID: testPageID, Title: "Form submissions guide", URL: "https://app.notion.com/p/" + testPageID,
				Parent:         notion.Parent{Type: notion.ParentTypeDataSource, ID: testDataSourceID, DatabaseID: testDatabaseID},
				LastEditedTime: "2026-09-30T16:05:00.000Z",
			},
		},
		NextCursor: "cursor-2", HasMore: true,
	}, result.Value, "an object type Notion adds later is skipped")
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/v1/search", request.path)
	require.JSONEq(t, `{"query":"Form submissions","filter":{"property":"object","value":"data_source"},`+
		`"sort":{"timestamp":"last_edited_time","direction":"descending"},"page_size":10}`, request.body)
}

func TestSearchSelectsNoMatchOnlyForAnEmptyFirstPage(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"object":"list","results":[],"next_cursor":null,"has_more":false}`)
	})
	client := newNotionClient(t, provider.URL)
	first, err := sdkgo.RunQuery(newNotionDexContext("first"), client.Search(), notionConnection, notion.SearchInput{Query: "Acount Hierarchy"})
	require.NoError(t, err)
	require.Equal(t, notion.SearchBranchNoMatch, first.Branch)
	require.Equal(t, sdkgo.FailureNotFound, first.Failure.Kind)
	require.Empty(t, first.Value.Matches)
	require.JSONEq(t, `{"query":"Acount Hierarchy","page_size":20}`, provider.request(0).body)

	later, err := sdkgo.RunQuery(newNotionDexContext("later"), client.Search(), notionConnection, notion.SearchInput{Query: "Acount", StartCursor: "cursor-2"})
	require.NoError(t, err)
	require.Equal(t, notion.SearchBranchSearched, later.Branch, "an empty continuation page is the end of results")
	require.JSONEq(t, `{"query":"Acount","start_cursor":"cursor-2","page_size":20}`, provider.request(1).body)
}

func TestSearchRetriesRateLimitsAfterRetryAfterButNotABlockedConnection(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"429", http.StatusTooManyRequests, `{"object":"error","status":429,"code":"rate_limited","message":"SENTINEL","additional_data":{"rate_limit_reason":"public_api_request_rate_limit","retry_after":"7"}}`},
		{"529", 529, `{"object":"error","status":529,"code":"service_overload","message":"SENTINEL"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				response.Header().Set("Retry-After", "7")
				writeJSON(t, response, test.status, test.body)
			})
			client := newNotionClient(t, provider.URL)
			_, err := sdkgo.RunQuery(newNotionDexContext("rate"), client.Search(), notionConnection, notion.SearchInput{Query: "Form"})
			requireRetry(t, err, sdkgo.FailureRateLimit)
			require.Equal(t, 7*time.Second, retryDelay(err))
		})
	}
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusTooManyRequests,
			`{"object":"error","status":429,"code":"rate_limited","message":"SENTINEL","additional_data":{"rate_limit_reason":"public_api_request_blocked"}}`)
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newNotionDexContext("blocked"), client.Search(), notionConnection, notion.SearchInput{Query: "Form"})
	require.NoError(t, err)
	require.Equal(t, notion.SearchBranchProviderRejected, result.Branch)
	require.Contains(t, result.Failure.Message, "public_api_request_blocked")
	requireNoSentinel(t, result)
}

func TestSearchRetriesServerErrorsAndRejectsInvalidPages(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		switch index {
		case 0:
			writeJSON(t, response, http.StatusBadGateway, notionError(502, "bad_gateway"))
		default:
			writeJSON(t, response, http.StatusOK, `{"object":"list","results":[{"object":"page","id":"not-an-id","properties":{}}],"has_more":false}`)
		}
	})
	client := newNotionClient(t, provider.URL)
	_, err := sdkgo.RunQuery(newNotionDexContext("server"), client.Search(), notionConnection, notion.SearchInput{})
	retry := requireRetry(t, err, sdkgo.FailureAvailability)
	require.Contains(t, retry.Failure.Message, "bad_gateway")
	result, err := sdkgo.RunQuery(newNotionDexContext("invalid"), client.Search(), notionConnection, notion.SearchInput{})
	require.NoError(t, err)
	require.Equal(t, notion.SearchBranchInvalidResponse, result.Branch)
}

func TestSearchRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingNotion(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newNotionClient(t, provider.URL)
	for _, input := range []notion.SearchInput{
		{ObjectType: "database"},
		{SortDirection: "newest"},
		{PageSize: 101},
		{PageSize: -1},
		{StartCursor: "cursor with spaces"},
	} {
		result, err := sdkgo.RunQuery(newNotionDexContext("invalid-input"), client.Search(), notionConnection, input)
		require.NoError(t, err)
		require.Equal(t, notion.SearchBranchDefect, result.Branch)
	}
	require.Zero(t, provider.requestCount())
}
