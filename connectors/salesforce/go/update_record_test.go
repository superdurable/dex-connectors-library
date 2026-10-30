// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func runUpdate(t *testing.T, client *salesforce.Client, input salesforce.UpdateRecordInput) (sdkgo.MutationResult[salesforce.UpdateRecordOutput], error) {
	t.Helper()
	return sdkgo.RunMutation(newDexContext("update"), client.UpdateRecord(), salesforceConnection, input)
}

func closeOpportunity() salesforce.UpdateRecordInput {
	return salesforce.UpdateRecordInput{
		SObjectType: "Opportunity", RecordID: "006RM000001AbcdYAC",
		Fields: map[string]json.RawMessage{"StageName": json.RawMessage(`"Closed Won"`), "NextStep": json.RawMessage(`null`)},
	}
}

func TestUpdateRecordPatchesOnlyTheNamedFields(t *testing.T) {
	fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		response.WriteHeader(http.StatusNoContent)
	})
	result, err := runUpdate(t, newSalesforceClient(t, fake.URL), closeOpportunity())
	require.NoError(t, err)
	require.Equal(t, salesforce.UpdateRecordBranchUpdated, result.Branch)
	require.Equal(t, salesforce.UpdateRecordOutput{ID: "006RM000001AbcdYAC"}, result.Value)
	require.Equal(t, "006RM000001AbcdYAC", result.Receipt.ProviderObjectID)

	requests := fake.recorded()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPatch, requests[0].method)
	require.Equal(t, "/services/data/v62.0/sobjects/Opportunity/006RM000001AbcdYAC", requests[0].path)
	require.JSONEq(t, `{"StageName":"Closed Won","NextStep":null}`, string(requests[0].body))
}

func TestUpdateRecordRoutesSalesforceErrors(t *testing.T) {
	for _, test := range []struct {
		name           string
		status         int
		body           string
		branch         sdkgo.BranchID
		providerErrors []salesforce.ProviderError
	}{
		{name: "deleted record", status: http.StatusNotFound, body: `[{"message":"entity is deleted","errorCode":"ENTITY_IS_DELETED"}]`,
			branch: salesforce.UpdateRecordBranchNotFound, providerErrors: []salesforce.ProviderError{{ErrorCode: "ENTITY_IS_DELETED"}}},
		{name: "validation rule", status: http.StatusBadRequest, body: `[{"message":"` + providerMessageSentinel + `","errorCode":"FIELD_CUSTOM_VALIDATION_EXCEPTION","fields":["CloseDate"]}]`,
			branch: salesforce.UpdateRecordBranchRecordRejected, providerErrors: []salesforce.ProviderError{{ErrorCode: "FIELD_CUSTOM_VALIDATION_EXCEPTION", Fields: []string{"CloseDate"}}}},
		{name: "unique field duplicate", status: http.StatusBadRequest, body: `[{"message":"duplicate value found","errorCode":"DUPLICATE_VALUE","fields":[]}]`,
			branch: salesforce.UpdateRecordBranchRecordRejected, providerErrors: []salesforce.ProviderError{{ErrorCode: "DUPLICATE_VALUE"}}},
		{name: "approval lock", status: http.StatusBadRequest, body: `[{"message":"locked","errorCode":"ENTITY_IS_LOCKED"}]`,
			branch: salesforce.UpdateRecordBranchRecordRejected, providerErrors: []salesforce.ProviderError{{ErrorCode: "ENTITY_IS_LOCKED"}}},
		{name: "revoked session", status: http.StatusUnauthorized, body: `[{"message":"Session expired or invalid","errorCode":"INVALID_SESSION_ID"}]`,
			branch: salesforce.UpdateRecordBranchProviderRejected, providerErrors: []salesforce.ProviderError{{ErrorCode: "INVALID_SESSION_ID"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := runUpdate(t, newSalesforceClient(t, fake.URL), closeOpportunity())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.providerErrors, result.Value.ProviderErrors)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "SENTINEL")
		})
	}
}

func TestUpdateRecordRetriesRowLocksAndOutages(t *testing.T) {
	for name, status := range map[string]int{"UNABLE_TO_LOCK_ROW": http.StatusBadRequest, "SERVER_UNAVAILABLE": http.StatusServiceUnavailable} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(t, response, status, `[{"message":"try again","errorCode":"`+name+`"}]`)
			})
			_, err := runUpdate(t, newSalesforceClient(t, fake.URL), closeOpportunity())
			require.Error(t, err)
			var retryErr *sdkgo.RetryError
			require.ErrorAs(t, err, &retryErr)
		})
	}
}

func TestUpdateRecordValidatesInputWithoutAProviderRequest(t *testing.T) {
	fake := newFakeSalesforce(t, func(http.ResponseWriter, *http.Request, []byte) {})
	client := newSalesforceClient(t, fake.URL)
	for name, input := range map[string]salesforce.UpdateRecordInput{
		"no fields":  {SObjectType: "Opportunity", RecordID: "006RM000001AbcdYAC"},
		"bad ID":     {SObjectType: "Opportunity", RecordID: "006/../001", Fields: closeOpportunity().Fields},
		"sets Id":    {SObjectType: "Opportunity", RecordID: "006RM000001AbcdYAC", Fields: map[string]json.RawMessage{"ID": json.RawMessage(`"x"`)}},
		"bad object": {SObjectType: "", RecordID: "006RM000001AbcdYAC", Fields: closeOpportunity().Fields},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := runUpdate(t, client, input)
			require.NoError(t, err)
			require.Equal(t, salesforce.UpdateRecordBranchDefect, result.Branch)
		})
	}
	require.Empty(t, fake.recorded())
}
