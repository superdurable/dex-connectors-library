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

func createContact() salesforce.CreateRecordInput {
	return salesforce.CreateRecordInput{
		SObjectType: "Contact",
		Fields:      map[string]json.RawMessage{"LastName": json.RawMessage(`"Raman"`)},
	}
}

func runCreate(t *testing.T, client *salesforce.Client, input salesforce.CreateRecordInput) (sdkgo.MutationResult[salesforce.CreateRecordOutput], error) {
	t.Helper()
	return sdkgo.RunMutation(newDexContext("create"), client.CreateRecord(), salesforceConnection, input)
}

func TestCreateRecordPostsOneValidatedRecord(t *testing.T) {
	fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(t, response, http.StatusCreated, `{"id":"003RM000001AbcdYAC","success":true,"errors":[]}`)
	})
	result, err := runCreate(t, newSalesforceClient(t, fake.URL), createContact())
	require.NoError(t, err)
	require.Equal(t, salesforce.CreateRecordBranchCreated, result.Branch)
	require.Equal(t, "003RM000001AbcdYAC", result.Value.ID)
	requests := fake.recorded()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].method)
	require.Equal(t, "/services/data/v62.0/sobjects/Contact", requests[0].path)
	require.JSONEq(t, `{"LastName":"Raman"}`, string(requests[0].body))
}

func TestCreateRecordClassifiesRejectedValuesAndAccess(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"required field", 400, `[{"errorCode":"REQUIRED_FIELD_MISSING","message":"` + providerMessageSentinel + `"}]`, salesforce.CreateRecordBranchRecordRejected},
		{"access", 403, `[{"errorCode":"INSUFFICIENT_ACCESS","message":"` + providerMessageSentinel + `"}]`, salesforce.CreateRecordBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := runCreate(t, newSalesforceClient(t, fake.URL), createContact())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), providerMessageSentinel)
		})
	}
}

func TestCreateRecordRetriesRateLimitsAndUnknownOutcomes(t *testing.T) {
	for _, status := range []int{429, 500, 503} {
		fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
			writeJSON(t, response, status, `[{"errorCode":"SERVER_UNAVAILABLE"}]`)
		})
		_, err := runCreate(t, newSalesforceClient(t, fake.URL), createContact())
		var retryErr *sdkgo.RetryError
		require.ErrorAs(t, err, &retryErr)
	}
}

func TestCreateRecordRejectsInvalidValuesWithoutSending(t *testing.T) {
	fake := newFakeSalesforce(t, func(http.ResponseWriter, *http.Request, []byte) {})
	input := createContact()
	input.Fields = map[string]json.RawMessage{"Id": json.RawMessage(`"003RM000001AbcdYAC"`)}
	result, err := runCreate(t, newSalesforceClient(t, fake.URL), input)
	require.NoError(t, err)
	require.Equal(t, salesforce.CreateRecordBranchDefect, result.Branch)
	require.Empty(t, fake.recorded())
}
