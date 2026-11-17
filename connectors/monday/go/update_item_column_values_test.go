// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateItemColumnValuesSetsAbsoluteValuesAndReadsThemBack(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"change_multiple_column_values": itemJSON(testItemID, "Fire drill")})
	})
	result, err := sdkgo.RunMutation(newMondayDexContext("update"), newMondayClient(t, provider.URL).UpdateItemColumnValues(), mondayConnection, monday.UpdateItemColumnValuesInput{
		BoardID: testBoardID, ItemID: testItemID, ColumnValues: map[string]monday.ColumnValue{
			"status": monday.StatusIndexValue(1), "date4": monday.DateTimeValue("2026-02-18", "09:00:00"), "text": monday.ClearedValue(monday.ColumnTypeText),
		},
	})
	require.NoError(t, err)
	require.Equal(t, monday.UpdateItemColumnValuesBranchUpdated, result.Branch)
	require.Equal(t, testItemID, result.Value.Item.ID)
	require.Equal(t, testItemID, result.Receipt.ProviderObjectID)
	request := provider.request(0)
	require.Contains(t, request.query, "change_multiple_column_values(board_id: $boardId, item_id: $itemId, column_values: $columnValues")
	require.NotEmpty(t, request.header.Get("Idempotency-Key"))
	require.JSONEq(t, `{"date4":{"date":"2026-02-18","time":"09:00:00"},"status":{"index":1},"text":null}`, request.variables["columnValues"].(string))
	require.Equal(t, []any{"date4", "status", "text"}, request.variables["columnIds"], "the result carries the changed columns only")
}

func TestUpdateItemColumnValuesRetriesALostResponseBecauseRepeatingIsSafe(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { dropConnection(t, response) })
	_, err := sdkgo.RunMutation(newMondayDexContext("update-lost"), newMondayClient(t, provider.URL).UpdateItemColumnValues(), mondayConnection, monday.UpdateItemColumnValuesInput{
		BoardID: testBoardID, ItemID: testItemID, ColumnValues: map[string]monday.ColumnValue{"status": monday.StatusLabelValue("Done")},
	})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}

func TestUpdateItemColumnValuesRetriesEveryUnknownOutcome(t *testing.T) {
	for name, reply := range map[string]func(http.ResponseWriter){
		"malformed item": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"data":{"change_multiple_column_values":{"id":"x"}}}`)
		},
		"another item": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"data":{"change_multiple_column_values":{"id":"1111111111","name":"x"}}}`)
		},
		"oversized item": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"data":{"change_multiple_column_values":{"id":"`+testItemID+`","name":"`+strings.Repeat("x", 2048)+`"}}}`)
		},
		"unlisted status":     func(response http.ResponseWriter) { writeJSON(t, response, http.StatusTeapot, `{"errors":[{"message":"SENTINEL"}]}`) },
		"unknown error code":  func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, `{"errors":[{"extensions":{"code":"SomethingNew"}}]}`) },
		"redirect":            func(response http.ResponseWriter) { response.Header().Set("Location", "https://example.invalid/"); response.WriteHeader(http.StatusFound) },
		"refusal beside item": func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, `{"data":{"change_multiple_column_values":{"id":"x"}},"errors":[{"extensions":{"code":"ColumnValueException"}}]}`) },
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { reply(response) })
			client, err := monday.New(monday.Config{MaxResponseBytes: 1024}, testCredentialProvider(), monday.WithAPIURL(provider.URL+"/v2"))
			require.NoError(t, err)
			_, err = sdkgo.RunMutation(newMondayDexContext("update-"+name), client.UpdateItemColumnValues(), mondayConnection, monday.UpdateItemColumnValuesInput{
				BoardID: testBoardID, ItemID: testItemID, ColumnValues: map[string]monday.ColumnValue{"status": monday.StatusLabelValue("Done")},
			})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "repeating an absolute update is safe")
		})
	}
}

func TestUpdateItemColumnValuesDiscardsAResponseThatEchoesTheCredential(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":{"change_multiple_column_values":{"id":"`+testItemID+`","name":"`+testAPIToken+`"}}}`)
	})
	result, err := sdkgo.RunMutation(newMondayDexContext("update-reflected"), newMondayClient(t, provider.URL).UpdateItemColumnValues(), mondayConnection, monday.UpdateItemColumnValuesInput{
		BoardID: testBoardID, ItemID: testItemID, ColumnValues: map[string]monday.ColumnValue{"status": monday.StatusLabelValue("Done")},
	})
	require.NoError(t, err)
	require.Equal(t, monday.UpdateItemColumnValuesBranchInvalidResponse, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestUpdateItemColumnValuesMapsAMissingItemToNotFound(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":{"change_multiple_column_values":null},"errors":[{"message":"SENTINEL","extensions":{"code":"ResourceNotFoundException","status_code":404}}]}`)
	})
	result, err := sdkgo.RunMutation(newMondayDexContext("update-missing"), newMondayClient(t, provider.URL).UpdateItemColumnValues(), mondayConnection, monday.UpdateItemColumnValuesInput{
		BoardID: testBoardID, ItemID: testItemID, ColumnValues: map[string]monday.ColumnValue{"status": monday.StatusLabelValue("Done")},
	})
	require.NoError(t, err)
	require.Equal(t, monday.UpdateItemColumnValuesBranchNotFound, result.Branch)
	require.Equal(t, monday.UpdateItemColumnValuesOutput{BoardID: testBoardID, ItemID: testItemID}, result.Value)
}

func TestUpdateItemColumnValuesNeedsAtLeastOneColumn(t *testing.T) {
	provider := newRecordingMonday(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request may be sent") })
	result, err := sdkgo.RunMutation(newMondayDexContext("update-empty"), newMondayClient(t, provider.URL).UpdateItemColumnValues(), mondayConnection, monday.UpdateItemColumnValuesInput{
		BoardID: testBoardID, ItemID: testItemID,
	})
	require.NoError(t, err)
	require.Equal(t, monday.UpdateItemColumnValuesBranchDefect, result.Branch)
}
