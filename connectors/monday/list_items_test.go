// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListItemsSendsATypedFilterAndDecodesOnePage(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"boards": []any{map[string]any{"id": testBoardID, "items_page": map[string]any{
			"cursor": "MSw5NzI4MDA5MDAsaV9YcmxJb0p1VEdYc1VWeGlxeF9k",
			"items":  []any{itemJSON("5550001", "Monthly Fire Drill Checklist - February")},
		}}}})
	})
	result, err := sdkgo.RunQuery(newMondayDexContext("list"), newMondayClient(t, provider.URL).ListItems(), mondayConnection, monday.ListItemsInput{
		BoardID: testBoardID, Limit: 10, ColumnIDs: []string{"status", "date4", "status"},
		Filter: monday.ItemFilter{Combination: monday.ItemFilterAllRules, Rules: []monday.ItemFilterRule{
			{ColumnID: "name", Values: []string{"Monthly Fire Drill Checklist - February"}},
			{ColumnID: "status", Operator: monday.ItemFilterNotAnyOf, Numbers: []float64{1}},
			{ColumnID: "date4", Operator: monday.ItemFilterBetween, Values: []string{"2026-02-01", "2026-02-28"}},
			{ColumnID: "text", Operator: monday.ItemFilterContainsText, Values: []string{"drill"}},
			{ColumnID: "person", Operator: monday.ItemFilterIsEmpty},
		}},
		Order: &monday.ItemOrder{ColumnID: monday.ItemOrderCreationTime, IsDescending: true},
	})
	require.NoError(t, err)
	require.Equal(t, monday.ListItemsBranchListed, result.Branch)
	request := provider.request(0)
	require.Contains(t, request.query, "boards(ids: $boardIds) { id items_page(limit: $limit, query_params: $queryParams)")
	variables, err := json.Marshal(request.variables)
	require.NoError(t, err)
	require.JSONEq(t, `{"boardIds":["1234567890"],"limit":10,"columnIds":["status","date4"],"queryParams":{
		"operator":"and","order_by":[{"column_id":"__creation_log__","direction":"desc"}],"rules":[
		{"column_id":"name","operator":"any_of","compare_value":["Monthly Fire Drill Checklist - February"]},
		{"column_id":"status","operator":"not_any_of","compare_value":[1]},
		{"column_id":"date4","operator":"between","compare_value":["2026-02-01","2026-02-28"]},
		{"column_id":"text","operator":"contains_text","compare_value":"drill"},
		{"column_id":"person","operator":"is_empty","compare_value":[]}]}}`, string(variables))

	require.Equal(t, testBoardID, result.Value.BoardID)
	require.Equal(t, "MSw5NzI4MDA5MDAsaV9YcmxJb0p1VEdYc1VWeGlxeF9k", result.Value.NextCursor)
	require.Len(t, result.Value.Items, 1)
	item := result.Value.Items[0]
	require.Equal(t, "5550001", item.ID)
	require.Equal(t, monday.ItemStateActive, item.State)
	require.Equal(t, &monday.ItemGroup{ID: "topics", Title: "February"}, item.Group)
	require.Equal(t, time.Date(2026, 1, 28, 10, 5, 0, 0, time.UTC), item.UpdatedAt)
	status, isFound := item.ColumnValueByID("status")
	require.True(t, isFound)
	require.Equal(t, monday.ColumnTypeStatus, status.Type)
	require.Equal(t, "Working on it", status.Text)
	require.JSONEq(t, `{"index":0,"post_id":null,"changed_at":"2026-01-28T10:00:00Z"}`, string(status.Value), "the JSON-encoded value string is decoded")
	empty, isFound := item.ColumnValueByID("text")
	require.True(t, isFound)
	require.Empty(t, empty.Text)
	require.Nil(t, empty.Value)
}

func TestListItemsContinuesWithTheCursorAlone(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"next_items_page": map[string]any{"cursor": nil, "items": []any{itemJSON("5550002", "Second")}}})
	})
	result, err := sdkgo.RunQuery(newMondayDexContext("next"), newMondayClient(t, provider.URL).ListItems(), mondayConnection, monday.ListItemsInput{
		BoardID: testBoardID, Cursor: "MSw5NzI4MDA5MDAs",
	})
	require.NoError(t, err)
	require.Equal(t, monday.ListItemsBranchListed, result.Branch)
	require.Empty(t, result.Value.NextCursor, "a null cursor means the last page")
	require.Len(t, result.Value.Items, 1)
	request := provider.request(0)
	require.Contains(t, request.query, "next_items_page(cursor: $cursor, limit: $limit)")
	require.Equal(t, map[string]any{"cursor": "MSw5NzI4MDA5MDAs", "limit": float64(monday.DefaultListItemsLimit), "columnIds": nil}, request.variables)
}

func TestListItemsReportsAMissingBoardAsNotFound(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"boards": []any{}})
	})
	result, err := sdkgo.RunQuery(newMondayDexContext("missing-board"), newMondayClient(t, provider.URL).ListItems(), mondayConnection, monday.ListItemsInput{BoardID: testBoardID})
	require.NoError(t, err)
	require.Equal(t, monday.ListItemsBranchNotFound, result.Branch)
	require.Equal(t, testBoardID, result.Value.BoardID)
	require.Empty(t, result.Value.Items)
}

func TestListItemsRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingMonday(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request may be sent") })
	client := newMondayClient(t, provider.URL)
	tooManyRules := make([]monday.ItemFilterRule, monday.MaxItemFilterRules+1)
	for index := range tooManyRules {
		tooManyRules[index] = monday.ItemFilterRule{ColumnID: "status", Numbers: []float64{1}}
	}
	for name, input := range map[string]monday.ListItemsInput{
		"board name instead of ID":  {BoardID: "Facilities"},
		"limit above the bound":     {BoardID: testBoardID, Limit: monday.MaxListItemsLimit + 1},
		"negative limit":            {BoardID: testBoardID, Limit: -1},
		"cursor with a filter":      {BoardID: testBoardID, Cursor: "abc", Filter: monday.ItemFilter{Rules: []monday.ItemFilterRule{{ColumnID: "name", Values: []string{"x"}}}}},
		"cursor with spaces":        {BoardID: testBoardID, Cursor: "not a cursor"},
		"too many rules":            {BoardID: testBoardID, Filter: monday.ItemFilter{Rules: tooManyRules}},
		"unknown operator":          {BoardID: testBoardID, Filter: monday.ItemFilter{Rules: []monday.ItemFilterRule{{ColumnID: "name", Operator: "equals", Values: []string{"x"}}}}},
		"is_empty with a value":     {BoardID: testBoardID, Filter: monday.ItemFilter{Rules: []monday.ItemFilterRule{{ColumnID: "status", Operator: monday.ItemFilterIsEmpty, Values: []string{"x"}}}}},
		"contains_text with two":    {BoardID: testBoardID, Filter: monday.ItemFilter{Rules: []monday.ItemFilterRule{{ColumnID: "text", Operator: monday.ItemFilterContainsText, Values: []string{"a", "b"}}}}},
		"between with one":          {BoardID: testBoardID, Filter: monday.ItemFilter{Rules: []monday.ItemFilterRule{{ColumnID: "date4", Operator: monday.ItemFilterBetween, Values: []string{"2026-01-01"}}}}},
		"values and numbers":        {BoardID: testBoardID, Filter: monday.ItemFilter{Rules: []monday.ItemFilterRule{{ColumnID: "status", Values: []string{"Done"}, Numbers: []float64{1}}}}},
		"any_of without a value":    {BoardID: testBoardID, Filter: monday.ItemFilter{Rules: []monday.ItemFilterRule{{ColumnID: "status"}}}},
		"column ID with a quote":    {BoardID: testBoardID, Filter: monday.ItemFilter{Rules: []monday.ItemFilterRule{{ColumnID: `status"`, Values: []string{"x"}}}}},
		"combination without rules": {BoardID: testBoardID, Filter: monday.ItemFilter{Combination: monday.ItemFilterAnyRule}},
		"unknown combination":       {BoardID: testBoardID, Filter: monday.ItemFilter{Combination: "xor", Rules: []monday.ItemFilterRule{{ColumnID: "name", Values: []string{"x"}}}}},
		"too many columns":          {BoardID: testBoardID, ColumnIDs: strings.Split(strings.Repeat("c,", monday.MaxRequestedColumns+1), ",")},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(newMondayDexContext("invalid"), client.ListItems(), mondayConnection, input)
			require.NoError(t, err)
			require.Equal(t, monday.ListItemsBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
}

func TestListItemsTreatsAnUnusableItemAsInvalidResponse(t *testing.T) {
	for name, item := range map[string]map[string]any{
		"non-numeric ID":     {"id": "abc", "name": "x"},
		"invalid state":      {"id": "1", "name": "x", "state": "frozen"},
		"value is not JSON":  {"id": "1", "name": "x", "column_values": []any{map[string]any{"id": "status", "type": "status", "value": "{oops"}}},
		"column type tokens": {"id": "1", "name": "x", "column_values": []any{map[string]any{"id": "status", "type": "Status Column SENTINEL"}}},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
				writeData(t, response, map[string]any{"boards": []any{map[string]any{"id": testBoardID, "items_page": map[string]any{"cursor": nil, "items": []any{item}}}}})
			})
			result, err := sdkgo.RunQuery(newMondayDexContext("unusable"), newMondayClient(t, provider.URL).ListItems(), mondayConnection, monday.ListItemsInput{BoardID: testBoardID})
			require.NoError(t, err)
			require.Equal(t, monday.ListItemsBranchInvalidResponse, result.Branch)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
		})
	}
}

func TestColumnValuesAreBounded(t *testing.T) {
	longText := strings.Repeat("é", monday.MaxColumnTextBytes)
	largeValue := `"{\"text\":\"` + strings.Repeat("y", monday.MaxColumnValueBytes) + `\"}"`
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":{"items":[{"id":"`+testItemID+`","name":"Long","column_values":[
			{"id":"long_text","type":"long_text","text":"`+longText+`","value":`+largeValue+`}]}]}}`)
	})
	result, err := sdkgo.RunQuery(newMondayDexContext("bounded"), newMondayClient(t, provider.URL).GetItem(), mondayConnection, monday.GetItemInput{ItemID: testItemID})
	require.NoError(t, err)
	require.Equal(t, monday.GetItemBranchFound, result.Branch)
	column := result.Value.ColumnValues[0]
	require.True(t, column.IsTextTruncated)
	require.LessOrEqual(t, len(column.Text), monday.MaxColumnTextBytes)
	require.True(t, strings.HasSuffix(column.Text, "é"), "the cut is on a rune boundary")
	require.True(t, column.IsValueOmitted)
	require.Nil(t, column.Value)
}
