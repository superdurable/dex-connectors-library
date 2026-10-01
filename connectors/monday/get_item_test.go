// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetItemReadsOneActiveItem(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"items": []any{itemJSON(testItemID, "Fire drill")}})
	})
	result, err := sdkgo.RunQuery(newMondayDexContext("get"), newMondayClient(t, provider.URL).GetItem(), mondayConnection, monday.GetItemInput{
		ItemID: " " + testItemID + " ", ColumnIDs: []string{"status"},
	})
	require.NoError(t, err)
	require.Equal(t, monday.GetItemBranchFound, result.Branch)
	require.Equal(t, testItemID, result.Value.ID)
	require.Equal(t, testBoardID, result.Value.BoardID)
	require.Equal(t, "48202303", result.Value.CreatorID)
	require.Equal(t, "https://acme.monday.com/boards/1234567890/pulses/9876543210", result.Value.URL)
	request := provider.request(0)
	require.Contains(t, request.query, "items(ids: $itemIds, exclude_nonactive: $excludeNonactive)")
	require.Equal(t, map[string]any{"itemIds": []any{testItemID}, "excludeNonactive": true, "columnIds": []any{"status"}}, request.variables)
}

func TestGetItemCanIncludeArchivedAndDeletedItems(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		item := itemJSON(testItemID, "Old drill")
		item["state"] = "archived"
		writeData(t, response, map[string]any{"items": []any{item}})
	})
	result, err := sdkgo.RunQuery(newMondayDexContext("inactive"), newMondayClient(t, provider.URL).GetItem(), mondayConnection, monday.GetItemInput{
		ItemID: testItemID, IncludesInactive: true,
	})
	require.NoError(t, err)
	require.Equal(t, monday.ItemStateArchived, result.Value.State)
	require.Equal(t, false, provider.request(0).variables["excludeNonactive"])
}

func TestGetItemReportsAnEmptyListAsNotFound(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"items": []any{}})
	})
	result, err := sdkgo.RunQuery(newMondayDexContext("missing"), newMondayClient(t, provider.URL).GetItem(), mondayConnection, monday.GetItemInput{ItemID: testItemID})
	require.NoError(t, err)
	require.Equal(t, monday.GetItemBranchNotFound, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
}

func TestGetItemRejectsAnotherItem(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"items": []any{itemJSON("1111111111", "Someone else's")}})
	})
	result, err := sdkgo.RunQuery(newMondayDexContext("other"), newMondayClient(t, provider.URL).GetItem(), mondayConnection, monday.GetItemInput{ItemID: testItemID})
	require.NoError(t, err)
	require.Equal(t, monday.GetItemBranchInvalidResponse, result.Branch)
}

func TestGetItemValidatesTheItemID(t *testing.T) {
	provider := newRecordingMonday(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request may be sent") })
	for _, itemID := range []string{"", "OPS-441", "0123", "12345678901234567890123"} {
		result, err := sdkgo.RunQuery(newMondayDexContext("invalid"), newMondayClient(t, provider.URL).GetItem(), mondayConnection, monday.GetItemInput{ItemID: itemID})
		require.NoError(t, err)
		require.Equal(t, monday.GetItemBranchDefect, result.Branch, itemID)
	}
}
