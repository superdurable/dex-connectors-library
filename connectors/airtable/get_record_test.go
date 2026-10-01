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

const testRecordID = "recDecisionLog001"

func TestGetRecordReadsOneRecordByID(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"id":"`+testRecordID+`","createdTime":"2026-09-30T10:00:00.000Z",
			"fields":{"Case ID":"case-4471","Amount USD":120,"Policy":["recPolicyStandard"]}}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	result, err := sdkgo.RunQuery(newStepDexContext("get-log"), client.GetRecord(), airtableConnection, airtable.GetRecordInput{
		BaseID: testBaseID, TableIDOrName: "Decision Log", RecordID: testRecordID,
	})
	require.NoError(t, err)
	require.Equal(t, airtable.GetRecordBranchFound, result.Branch)
	request := provider.recordedRequests()[0]
	require.Equal(t, http.MethodGet, request.method)
	require.Equal(t, "/v0/"+testBaseID+"/Decision%20Log/"+testRecordID, request.rawPath)
	require.Empty(t, request.body)
	require.Empty(t, request.contentType)
	require.Equal(t, testRecordID, result.Value.ID)
	caseID, _ := result.Value.Fields["Case ID"].Text()
	require.Equal(t, "case-4471", caseID)
	policies, _ := result.Value.Fields["Policy"].StringList()
	require.Equal(t, []string{"recPolicyStandard"}, policies)
	require.Equal(t, testRecordID, result.Receipt.ProviderObjectID)
}

func TestGetRecordSelectsNotFoundForAMissingRecord(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusNotFound, `{"error":"NOT_FOUND"}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	result, err := sdkgo.RunQuery(newStepDexContext("get-missing"), client.GetRecord(), airtableConnection, airtable.GetRecordInput{
		BaseID: testBaseID, TableIDOrName: testTableID, RecordID: testRecordID,
	})
	require.NoError(t, err)
	require.Equal(t, airtable.GetRecordBranchNotFound, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	requireSecretSafeFailure(t, result.Failure)
}

func TestGetRecordRejectsAnInvalidRecordIDAndADifferentRecord(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"id":"recSomeoneElse001","createdTime":"2026-09-30T10:00:00.000Z","fields":{}}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	result, err := sdkgo.RunQuery(newStepDexContext("get-invalid"), client.GetRecord(), airtableConnection, airtable.GetRecordInput{
		BaseID: testBaseID, TableIDOrName: testTableID, RecordID: "rec/../../meta",
	})
	require.NoError(t, err)
	require.Equal(t, airtable.GetRecordBranchDefect, result.Branch)
	require.Empty(t, provider.recordedRequests(), "a malformed record ID never builds a path")

	result, err = sdkgo.RunQuery(newStepDexContext("get-other"), client.GetRecord(), airtableConnection, airtable.GetRecordInput{
		BaseID: testBaseID, TableIDOrName: testTableID, RecordID: testRecordID,
	})
	require.NoError(t, err)
	require.Equal(t, airtable.GetRecordBranchInvalidResponse, result.Branch)
}
