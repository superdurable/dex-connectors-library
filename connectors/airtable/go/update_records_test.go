// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateRecordsPatchesNamedFieldsByRecordID(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"records":[
			{"id":"recPolicyStandard","createdTime":"2026-09-12T21:03:48.000Z","fields":{"Policy Key":"refund-standard","Last Decided Case":"case-1"}}
		]}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	result, err := sdkgo.RunMutation(newStepDexContext("stamp-policy"), client.UpdateRecords(), airtableConnection, airtable.UpdateRecordsInput{
		BaseID: testBaseID, TableIDOrName: testTableID,
		Records: []airtable.RecordUpdate{{ID: "recPolicyStandard", Fields: map[string]airtable.CellValue{
			"Last Decided Case": airtable.TextCellValue("case-1"),
		}}},
	})
	require.NoError(t, err)
	require.Equal(t, airtable.UpdateRecordsBranchUpdated, result.Branch)
	request := provider.recordedRequests()[0]
	require.Equal(t, http.MethodPatch, request.method)
	require.Equal(t, "/v0/"+testBaseID+"/"+testTableID, request.path)
	require.JSONEq(t, `{"records":[{"id":"recPolicyStandard","fields":{"Last Decided Case":"case-1"}}]}`, string(request.body),
		"an update never sends performUpsert, so an unknown record ID is never created")
	stamped, _ := result.Value.Records[0].Fields["Last Decided Case"].Text()
	require.Equal(t, "case-1", stamped)
	require.Empty(t, result.Value.PartialSuccessReasons)
}

func TestUpdateRecordsSelectsNotFoundForAMissingRecord(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusNotFound, `{"error":{"type":"NOT_FOUND","message":"`+providerMessageSentinel+`"}}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	result, err := sdkgo.RunMutation(newStepDexContext("stamp-missing"), client.UpdateRecords(), airtableConnection, airtable.UpdateRecordsInput{
		BaseID: testBaseID, TableIDOrName: testTableID,
		Records: []airtable.RecordUpdate{{ID: "recPolicyStandard", Fields: map[string]airtable.CellValue{"Status": airtable.TextCellValue("done")}}},
	})
	require.NoError(t, err)
	require.Equal(t, airtable.UpdateRecordsBranchNotFound, result.Branch)
	requireSecretSafeFailure(t, result.Failure)
}

func TestUpdateRecordsRejectsUnsafeInputWithoutARequest(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	fields := map[string]airtable.CellValue{"Status": airtable.TextCellValue("done")}
	for name, records := range map[string][]airtable.RecordUpdate{
		"no records":         nil,
		"invalid record ID":  {{ID: "recPolicyStandard/../x", Fields: fields}},
		"repeated record ID": {{ID: "recPolicyStandard", Fields: fields}, {ID: "recPolicyStandard", Fields: fields}},
		"no field values":    {{ID: "recPolicyStandard"}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunMutation(newStepDexContext("update-invalid"), client.UpdateRecords(), airtableConnection, airtable.UpdateRecordsInput{
				BaseID: testBaseID, TableIDOrName: testTableID, Records: records,
			})
			require.NoError(t, err)
			require.Equal(t, airtable.UpdateRecordsBranchDefect, result.Branch)
		})
	}
	require.Empty(t, provider.recordedRequests())
}

func TestUpdateRecordsSelectsInvalidResponseForAnotherRecord(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"records":[{"id":"recSomeoneElse001","createdTime":"2026-09-12T21:03:48.000Z","fields":{}}]}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	result, err := sdkgo.RunMutation(newStepDexContext("update-other"), client.UpdateRecords(), airtableConnection, airtable.UpdateRecordsInput{
		BaseID: testBaseID, TableIDOrName: testTableID,
		Records: []airtable.RecordUpdate{{ID: "recPolicyStandard", Fields: map[string]airtable.CellValue{"Status": airtable.TextCellValue("done")}}},
	})
	require.NoError(t, err)
	require.Equal(t, airtable.UpdateRecordsBranchInvalidResponse, result.Branch)
}
