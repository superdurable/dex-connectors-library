// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func databaseJSON(dataSourceIDs ...string) string {
	references := make([]string, 0, len(dataSourceIDs))
	for index, id := range dataSourceIDs {
		references = append(references, fmt.Sprintf(`{"id":%q,"name":"Source %d"}`, id, index+1))
	}
	return fmt.Sprintf(`{"object":"database","id":%q,"title":[{"plain_text":"Form submissions"}],"data_sources":[%s],"in_trash":false}`,
		testDatabaseID, strings.Join(references, ","))
}

func TestQueryDatabaseSendsTheTypedFilterSortsAndReturnedProperties(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"object":"list","type":"page_or_data_source","page_or_data_source":{},"results":[`+
			pageJSON(testPageID, "Ada Lovelace")+`],"next_cursor":"cursor-2","has_more":true,"request_status":{"type":"complete"}}`)
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newNotionDexContext("query"), client.QueryDatabase(), notionConnection, notion.QueryDatabaseInput{
		DataSourceID: strings.ReplaceAll(testDataSourceID, "-", ""),
		Filter:       &notion.QueryFilter{Property: "Email", Type: notion.PropertyTypeEmail, Condition: notion.FilterEquals, Text: "ada@example.com"},
		Sorts:        []notion.QuerySort{{Timestamp: notion.TimestampCreatedTime, Direction: notion.SortDescending}},
		Properties:   []string{"Name", "Email"},
		PageSize:     1,
	})
	require.NoError(t, err)
	require.Equal(t, notion.QueryDatabaseBranchQueried, result.Branch)
	require.Equal(t, testDataSourceID, result.Value.DataSourceID)
	require.Equal(t, "cursor-2", result.Value.NextCursor)
	require.True(t, result.Value.HasMore)
	require.False(t, result.Value.IsResultLimitReached)
	require.Len(t, result.Value.Pages, 1)
	page := result.Value.Pages[0]
	require.Equal(t, testPageID, page.ID)
	require.Equal(t, "Ada Lovelace", page.Title)
	require.Equal(t, "ada@example.com", page.Properties["Email"].Email)
	require.Equal(t, "New", page.Properties["Status"].Option)
	require.Equal(t, notion.Parent{Type: notion.ParentTypeDataSource, ID: testDataSourceID, DatabaseID: testDatabaseID}, page.Parent)

	require.Equal(t, 1, provider.requestCount(), "a data source ID needs no database lookup")
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/v1/data_sources/"+testDataSourceID+"/query", request.path)
	query, err := url.ParseQuery(request.query)
	require.NoError(t, err)
	require.Equal(t, []string{"Name", "Email"}, query["filter_properties[]"])
	require.JSONEq(t, `{"filter":{"property":"Email","email":{"equals":"ada@example.com"}},`+
		`"sorts":[{"timestamp":"created_time","direction":"descending"}],"page_size":1,"result_type":"page"}`, request.body)
}

func TestQueryDatabaseResolvesTheOnlyDataSourceOfADatabase(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, databaseJSON(testDataSourceID))
			return
		}
		writeJSON(t, response, http.StatusOK, `{"object":"list","results":[],"next_cursor":null,"has_more":false,`+
			`"request_status":{"type":"incomplete","incomplete_reason":"query_result_limit_reached"}}`)
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newNotionDexContext("database"), client.QueryDatabase(), notionConnection, notion.QueryDatabaseInput{
		DatabaseID: "https://www.notion.so/acme/" + strings.ReplaceAll(testDatabaseID, "-", "") + "?v=0123456789abcdef0123456789abcdef",
	})
	require.NoError(t, err)
	require.Equal(t, notion.QueryDatabaseBranchQueried, result.Branch)
	require.Empty(t, result.Value.Pages, "an empty page of rows is a normal query result")
	require.True(t, result.Value.IsResultLimitReached)
	require.Equal(t, "/v1/databases/"+testDatabaseID, provider.request(0).path)
	require.Equal(t, "/v1/data_sources/"+testDataSourceID+"/query", provider.request(1).path)
	require.JSONEq(t, `{"page_size":50,"result_type":"page"}`, provider.request(1).body)
}

func TestQueryDatabaseNamesTheDataSourcesOfAnAmbiguousDatabase(t *testing.T) {
	const secondDataSourceID = "5d7f9213-4e6f-4081-92a3-445566778899"
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, databaseJSON(testDataSourceID, secondDataSourceID))
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newNotionDexContext("ambiguous"), client.QueryDatabase(), notionConnection, notion.QueryDatabaseInput{DatabaseID: testDatabaseID})
	require.NoError(t, err)
	require.Equal(t, notion.QueryDatabaseBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, testDataSourceID)
	require.Contains(t, result.Failure.Message, secondDataSourceID)
	require.NotContains(t, result.Failure.Message, "Source 1", "data source names are provider content")
	require.Equal(t, 1, provider.requestCount(), "nothing is queried while the data source is ambiguous")
}

func TestQueryDatabaseReportsAnUnsharedDatabaseAsNotFound(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, notionError(404, "object_not_found"))
	})
	client := newNotionClient(t, provider.URL)
	for _, input := range []notion.QueryDatabaseInput{{DatabaseID: testDatabaseID}, {DataSourceID: testDataSourceID}} {
		result, err := sdkgo.RunQuery(newNotionDexContext("missing"), client.QueryDatabase(), notionConnection, input)
		require.NoError(t, err)
		require.Equal(t, notion.QueryDatabaseBranchNotFound, result.Branch)
		require.Contains(t, result.Failure.Message, "Connections")
		requireNoSentinel(t, result)
	}
}

func TestQueryDatabaseRejectsAFilterNotionRefusesAndInvalidPages(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		switch index {
		case 0:
			writeJSON(t, response, http.StatusBadRequest, notionError(400, "validation_error"))
		case 1:
			writeJSON(t, response, http.StatusOK, `{"object":"list","results":[{"object":"page","id":"x"}],"has_more":false}`)
		default:
			writeJSON(t, response, http.StatusOK, `{"object":"list","results":[`+pageJSON(testPageID, "a")+`,`+pageJSON(testPageID, "b")+`],"has_more":false}`)
		}
	})
	client := newNotionClient(t, provider.URL)
	input := notion.QueryDatabaseInput{DataSourceID: testDataSourceID, PageSize: 1}
	rejected, err := sdkgo.RunQuery(newNotionDexContext("rejected"), client.QueryDatabase(), notionConnection, input)
	require.NoError(t, err)
	require.Equal(t, notion.QueryDatabaseBranchProviderRejected, rejected.Branch)
	require.Contains(t, rejected.Failure.Message, "validation_error")
	for _, step := range []string{"invalid-row", "oversized-page"} {
		invalid, err := sdkgo.RunQuery(newNotionDexContext(step), client.QueryDatabase(), notionConnection, input)
		require.NoError(t, err)
		require.Equal(t, notion.QueryDatabaseBranchInvalidResponse, invalid.Branch, step)
	}
}

func TestQueryDatabaseRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingNotion(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newNotionClient(t, provider.URL)
	for name, input := range map[string]notion.QueryDatabaseInput{
		"neither ID":       {},
		"both IDs":         {DataSourceID: testDataSourceID, DatabaseID: testDatabaseID},
		"an invalid ID":    {DataSourceID: "Form submissions"},
		"an invalid sort":  {DataSourceID: testDataSourceID, Sorts: []notion.QuerySort{{}}},
		"an invalid size":  {DataSourceID: testDataSourceID, PageSize: 101},
		"a blank property": {DataSourceID: testDataSourceID, Properties: []string{" "}},
		"an invalid filter": {DataSourceID: testDataSourceID, Filter: &notion.QueryFilter{
			Property: "Score", Type: notion.PropertyTypeNumber, Condition: notion.FilterStartsWith, Text: "1",
		}},
	} {
		result, err := sdkgo.RunQuery(newNotionDexContext("invalid"), client.QueryDatabase(), notionConnection, input)
		require.NoError(t, err)
		require.Equal(t, notion.QueryDatabaseBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.requestCount())
}
