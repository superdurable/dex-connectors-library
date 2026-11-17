// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func runDelete(t *testing.T, client *salesforce.Client, input salesforce.DeleteRecordInput) (sdkgo.MutationResult[salesforce.DeleteRecordOutput], error) {
	t.Helper()
	return sdkgo.RunMutation(newDexContext("delete"), client.DeleteRecord(), salesforceConnection, input)
}

func deleteContact() salesforce.DeleteRecordInput {
	return salesforce.DeleteRecordInput{SObjectType: "Contact", RecordID: "003RM000001AbcdYAC"}
}

func TestDeleteRecordRemovesTheNamedRecord(t *testing.T) {
	fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		response.WriteHeader(http.StatusNoContent)
	})
	result, err := runDelete(t, newSalesforceClient(t, fake.URL), deleteContact())
	require.NoError(t, err)
	require.Equal(t, salesforce.DeleteRecordBranchDeleted, result.Branch)
	require.Equal(t, deleteContact().RecordID, result.Value.ID)
	requests := fake.recorded()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodDelete, requests[0].method)
	require.Equal(t, "/services/data/v62.0/sobjects/Contact/003RM000001AbcdYAC", requests[0].path)
}

func TestDeleteRecordClassifiesProviderResponses(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"missing", 404, `[{"errorCode":"NOT_FOUND","message":"` + providerMessageSentinel + `"}]`, salesforce.DeleteRecordBranchNotFound},
		{"constraint", 400, `[{"errorCode":"DELETE_FAILED","message":"` + providerMessageSentinel + `"}]`, salesforce.DeleteRecordBranchRecordRejected},
		{"denied", 403, `[{"errorCode":"INSUFFICIENT_ACCESS","message":"` + providerMessageSentinel + `"}]`, salesforce.DeleteRecordBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := runDelete(t, newSalesforceClient(t, fake.URL), deleteContact())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), providerMessageSentinel)
		})
	}
}

func TestDeleteRecordRetriesUnknownAndRateLimitedOutcomes(t *testing.T) {
	for _, status := range []int{429, 500, 503} {
		fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(t, response, status, `[{"errorCode":"SERVER_UNAVAILABLE"}]`)
		})
		_, err := runDelete(t, newSalesforceClient(t, fake.URL), deleteContact())
		var retryErr *sdkgo.RetryError
		require.ErrorAs(t, err, &retryErr)
	}
}

func TestDeleteRecordRejectsInvalidInputWithoutARequest(t *testing.T) {
	fake := newFakeSalesforce(t, func(http.ResponseWriter, *http.Request, []byte) {})
	result, err := runDelete(t, newSalesforceClient(t, fake.URL), salesforce.DeleteRecordInput{SObjectType: "Contact", RecordID: "bad"})
	require.NoError(t, err)
	require.Equal(t, salesforce.DeleteRecordBranchDefect, result.Branch)
	require.Empty(t, fake.recorded())
}
