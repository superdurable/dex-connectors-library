// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetRecordReadsTheChosenFieldsOfOneRecord(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"data": []any{contactJSON(testContactID, "jane@acme.example.com", "2026-01-26T14:00:00+05:30")}})
	})
	result, err := sdkgo.RunQuery(newCRMDexContext("get"), newCRMClient(t, provider.URL).GetRecord(), crmConnection,
		crm.GetRecordInput{Module: crm.ModuleContacts, RecordID: testContactID, Fields: []string{"Email", "Account_Name"}})
	require.NoError(t, err)
	require.Equal(t, crm.GetRecordBranchFound, result.Branch)
	require.Equal(t, testContactID, result.Value.ID)
	require.Equal(t, crm.ModuleContacts, result.Value.Module)
	ownerID, isFound := result.Value.LookupID("Owner")
	require.True(t, isFound)
	require.Equal(t, testOwnerID, ownerID)
	require.Equal(t, testContactID, result.Receipt.ProviderObjectID)
	request := provider.request(0)
	require.Equal(t, http.MethodGet, request.method)
	require.Equal(t, "/crm/v8/Contacts/"+testContactID, request.path)
	require.Equal(t, url.Values{"fields": {"Email,Account_Name"}}, request.query)
}

func TestGetRecordSelectsNotFoundForAMissingRecord(t *testing.T) {
	for name, reply := range map[string]func(http.ResponseWriter){
		"204": func(response http.ResponseWriter) { response.WriteHeader(http.StatusNoContent) },
		"invalid id": func(response http.ResponseWriter) {
			writeValue(t, response, http.StatusBadRequest, recordErrorJSON("INVALID_DATA", "id", nil))
		},
		"details id": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadRequest, `{"code":"INVALID_DATA","details":{"id":"`+testDealID+`"},"message":"SENTINEL","status":"error"}`)
		},
		"empty record": func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, `{"data":[]}`) },
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) { reply(response) })
			result, err := sdkgo.RunQuery(newCRMDexContext("get"), newCRMClient(t, provider.URL).GetRecord(), crmConnection,
				crm.GetRecordInput{Module: crm.ModuleDeals, RecordID: testDealID})
			require.NoError(t, err)
			require.Equal(t, crm.GetRecordBranchNotFound, result.Branch)
			require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
			requireNoSentinel(t, result)
			require.Empty(t, provider.request(0).query, "blank fields return every field")
		})
	}
}

func TestGetRecordRejectsABadLookupAnotherRecordAndInvalidInput(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeValue(t, response, http.StatusBadRequest, recordErrorJSON("INVALID_DATA", "Owner", map[string]any{"id": testOwnerID}))
			return
		}
		writeValue(t, response, http.StatusOK, map[string]any{"data": []any{dealJSON("4150868000003194099", "Won", "2026-01-28T18:30:00+05:30")}})
	})
	client := newCRMClient(t, provider.URL)
	input := crm.GetRecordInput{Module: crm.ModuleDeals, RecordID: testDealID}
	result, err := sdkgo.RunQuery(newCRMDexContext("get"), client.GetRecord(), crmConnection, input)
	require.NoError(t, err)
	require.Equal(t, crm.GetRecordBranchProviderRejected, result.Branch, "INVALID_DATA about another field is not a missing record")
	require.Contains(t, result.Failure.Message, "field: Owner")
	result, err = sdkgo.RunQuery(newCRMDexContext("get"), client.GetRecord(), crmConnection, input)
	require.NoError(t, err)
	require.Equal(t, crm.GetRecordBranchInvalidResponse, result.Branch)

	for _, invalid := range []crm.GetRecordInput{
		{Module: crm.ModuleDeals, RecordID: "opp_123"},
		{Module: "Deals/../Leads", RecordID: testDealID},
		{Module: crm.ModuleDeals, RecordID: testDealID, Fields: []string{"Account_Name.Account_Name"}},
	} {
		result, err := sdkgo.RunQuery(newCRMDexContext("get"), client.GetRecord(), crmConnection, invalid)
		require.NoError(t, err)
		require.Equal(t, crm.GetRecordBranchDefect, result.Branch)
	}
	require.Equal(t, 2, provider.requestCount())
}
