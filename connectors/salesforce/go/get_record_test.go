// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func runGetRecord(t *testing.T, client *salesforce.Client, input salesforce.GetRecordInput) (sdkgo.QueryResult[salesforce.Record], error) {
	t.Helper()
	return sdkgo.RunQuery(newDexContext("get"), client.GetRecord(), salesforceConnection, input)
}

func TestGetRecordReadsTheChosenFields(t *testing.T) {
	fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(t, response, http.StatusOK, `{"attributes":{"type":"Opportunity","url":"/services/data/v62.0/sobjects/Opportunity/006RM000001AbcdYAC"},"Id":"006RM000001AbcdYAC","StageName":"Negotiation","Amount":75000.0,"CloseDate":"2026-02-15"}`)
	})
	result, err := runGetRecord(t, newSalesforceClient(t, fake.URL), salesforce.GetRecordInput{
		SObjectType: "Opportunity", RecordID: "006RM000001Abcd", Fields: []string{"StageName", "Amount", "CloseDate"},
	})
	require.NoError(t, err)
	require.Equal(t, salesforce.GetRecordBranchFound, result.Branch)
	require.Equal(t, "006RM000001AbcdYAC", result.Value.ID)
	require.Equal(t, "006RM000001AbcdYAC", result.Receipt.ProviderObjectID)
	var amount float64
	isPresent, err := result.Value.DecodeField("Amount", &amount)
	require.NoError(t, err)
	require.True(t, isPresent)
	require.InDelta(t, 75000.0, amount, 0)
	isPresent, err = result.Value.DecodeField("Probability", &amount)
	require.NoError(t, err)
	require.False(t, isPresent)
	_, isString := result.Value.StringField("Amount")
	require.False(t, isString)

	requests := fake.recorded()
	require.Len(t, requests, 1)
	require.Equal(t, "/services/data/v62.0/sobjects/Opportunity/006RM000001Abcd", requests[0].path)
	require.Equal(t, "StageName,Amount,CloseDate", requests[0].query["fields"][0])
}

func TestGetRecordSelectsNotFoundForMissingAndDeletedRecords(t *testing.T) {
	for _, errorCode := range []string{"NOT_FOUND", "ENTITY_IS_DELETED"} {
		t.Run(errorCode, func(t *testing.T) {
			fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(t, response, http.StatusNotFound, `[{"message":"`+providerMessageSentinel+`","errorCode":"`+errorCode+`"}]`)
			})
			result, err := runGetRecord(t, newSalesforceClient(t, fake.URL), salesforce.GetRecordInput{
				SObjectType: "Contact", RecordID: "003RM000001AbcdYAC", Fields: []string{"Email"},
			})
			require.NoError(t, err)
			require.Equal(t, salesforce.GetRecordBranchNotFound, result.Branch)
			require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
		})
	}
}

func TestGetRecordRejectsUnknownFieldsAndInvalidRecords(t *testing.T) {
	fake := newFakeSalesforce(t, func(response http.ResponseWriter, request *http.Request, _ []byte) {
		if request.URL.Query().Get("fields") == "Bogus__c" {
			writeJSON(t, response, http.StatusBadRequest, `[{"message":"No such column 'Bogus__c'","errorCode":"INVALID_FIELD"}]`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"attributes":{"type":"Contact"},"Id":"003RM000009ZzzzYAC","Email":"other@example.com"}`)
	})
	client := newSalesforceClient(t, fake.URL)

	rejected, err := runGetRecord(t, client, salesforce.GetRecordInput{SObjectType: "Contact", RecordID: "003RM000001AbcdYAC", Fields: []string{"Bogus__c"}})
	require.NoError(t, err)
	require.Equal(t, salesforce.GetRecordBranchProviderRejected, rejected.Branch)
	require.Contains(t, rejected.Failure.Message, "INVALID_FIELD")
	require.NotContains(t, rejected.Failure.Message, "No such column")

	otherRecord, err := runGetRecord(t, client, salesforce.GetRecordInput{SObjectType: "Contact", RecordID: "003RM000001AbcdYAC", Fields: []string{"Email"}})
	require.NoError(t, err)
	require.Equal(t, salesforce.GetRecordBranchInvalidResponse, otherRecord.Branch)
	require.Equal(t, sdkgo.FailureProtocol, otherRecord.Failure.Kind)
}

func TestGetRecordValidatesInputWithoutAProviderRequest(t *testing.T) {
	fake := newFakeSalesforce(t, func(http.ResponseWriter, *http.Request, []byte) {})
	client := newSalesforceClient(t, fake.URL)
	for name, input := range map[string]salesforce.GetRecordInput{
		"object path":       {SObjectType: "Contact/003", RecordID: "003RM000001AbcdYAC", Fields: []string{"Email"}},
		"short ID":          {SObjectType: "Contact", RecordID: "003RM", Fields: []string{"Email"}},
		"no fields":         {SObjectType: "Contact", RecordID: "003RM000001AbcdYAC"},
		"relationship path": {SObjectType: "Contact", RecordID: "003RM000001AbcdYAC", Fields: []string{"Account.Name"}},
		"duplicate field":   {SObjectType: "Contact", RecordID: "003RM000001AbcdYAC", Fields: []string{"Email", "email"}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := runGetRecord(t, client, input)
			require.NoError(t, err)
			require.Equal(t, salesforce.GetRecordBranchDefect, result.Branch)
		})
	}
	require.Empty(t, fake.recorded())
}
