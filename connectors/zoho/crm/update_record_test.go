// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func dealStageUpdateInput() crm.UpdateRecordInput {
	return crm.UpdateRecordInput{
		Module: crm.ModuleDeals, RecordID: testDealID,
		Fields: map[string]json.RawMessage{crm.FieldStage: crm.TextFieldValue("Negotiation/Review"), crm.FieldOwner: crm.LookupFieldValue(testOwnerID)},
	}
}

func TestUpdateRecordPutsTheFieldsOfOneRecord(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, writeSuccessJSON("", testDealID, nil))
	})
	result, err := sdkgo.RunMutation(newCRMDexContext("update"), newCRMClient(t, provider.URL).UpdateRecord(), crmConnection, dealStageUpdateInput())
	require.NoError(t, err)
	require.Equal(t, crm.UpdateRecordBranchUpdated, result.Branch)
	require.Equal(t, testDealID, result.Value.ID)
	require.False(t, result.Value.ModifiedAt.IsZero())
	request := provider.request(0)
	require.Equal(t, http.MethodPut, request.method)
	require.Equal(t, "/crm/v8/Deals/"+testDealID, request.path)
	require.JSONEq(t, `{"data":[{"Stage":"Negotiation/Review","Owner":{"id":"`+testOwnerID+`"}}]}`, request.body)
}

func TestUpdateRecordMapsMissingRecordsConflictsAndRejections(t *testing.T) {
	for name, scenario := range map[string]struct {
		status int
		body   any
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		"unknown record id":  {http.StatusBadRequest, recordErrorJSON("INVALID_DATA", "id", nil), crm.UpdateRecordBranchNotFound, sdkgo.FailureNotFound},
		"unique value taken": {http.StatusBadRequest, recordErrorJSON("DUPLICATE_DATA", "External_Deal_ID", map[string]any{"duplicate_record": map[string]any{"id": "4150868000003194099"}}), crm.UpdateRecordBranchConflict, sdkgo.FailureConflict},
		"invalid stage":      {http.StatusBadRequest, recordErrorJSON("INVALID_DATA", "Stage", nil), crm.UpdateRecordBranchRecordRejected, sdkgo.FailureValidation},
		"locked by approval": {http.StatusBadRequest, recordErrorJSON("RECORD_LOCKED", "Stage", nil), crm.UpdateRecordBranchRecordRejected, sdkgo.FailureValidation},
		"scope mismatch":     {http.StatusUnauthorized, map[string]any{"code": "OAUTH_SCOPE_MISMATCH", "details": map[string]any{}, "message": "SENTINEL", "status": "error"}, crm.UpdateRecordBranchProviderRejected, sdkgo.FailureAuthorization},
		"another record":     {http.StatusOK, writeSuccessJSON("", "4150868000003194099", nil), crm.UpdateRecordBranchInvalidResponse, sdkgo.FailureProtocol},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeValue(t, response, scenario.status, scenario.body)
			})
			result, err := sdkgo.RunMutation(newCRMDexContext("update"), newCRMClient(t, provider.URL).UpdateRecord(), crmConnection, dealStageUpdateInput())
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			require.Equal(t, scenario.kind, result.Failure.Kind)
			requireNoSentinel(t, result)
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestUpdateRecordRetriesALostResponseAndRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		hijacker, isHijacker := response.(http.Hijacker)
		require.True(t, isHijacker)
		connection, _, err := hijacker.Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	client := newCRMClient(t, provider.URL)
	_, err := sdkgo.RunMutation(newCRMDexContext("update"), client.UpdateRecord(), crmConnection, dealStageUpdateInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Equal(t, 1, provider.requestCount())

	for _, invalid := range []crm.UpdateRecordInput{
		{Module: crm.ModuleDeals, RecordID: "opp_123", Fields: dealStageUpdateInput().Fields},
		{Module: crm.ModuleDeals, RecordID: testDealID},
		{Module: crm.ModuleDeals, RecordID: testDealID, Fields: map[string]json.RawMessage{"Account_Name.Account_Name": crm.TextFieldValue("x")}},
	} {
		result, err := sdkgo.RunMutation(newCRMDexContext("update"), client.UpdateRecord(), crmConnection, invalid)
		require.NoError(t, err)
		require.Equal(t, crm.UpdateRecordBranchDefect, result.Branch)
	}
	require.Equal(t, 1, provider.requestCount())
}
