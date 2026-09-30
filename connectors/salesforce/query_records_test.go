// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const firstPageCursor = "/services/data/v62.0/query/01gRM000001Bxy1YAC-200"

func runQueryRecords(t *testing.T, client *salesforce.Client, input salesforce.QueryRecordsInput) (sdkgo.QueryResult[salesforce.QueryRecordsOutput], error) {
	t.Helper()
	return sdkgo.RunQuery(newDexContext("query"), client.QueryRecords(), salesforceConnection, input)
}

func TestQueryRecordsSendsTheBoundStatementAndReturnsTheCursor(t *testing.T) {
	fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(t, response, http.StatusOK, `{"totalSize":450,"done":false,"nextRecordsUrl":"`+firstPageCursor+`","records":[
			{"attributes":{"type":"Contact","url":"/services/data/v62.0/sobjects/Contact/003RM000001AbcdYAC"},"Id":"003RM000001AbcdYAC","Email":"o'brien@example.com","Account":{"attributes":{"type":"Account"},"Name":"Acme"},"Amount__c":75000.50}]}`)
	})
	client := newSalesforceClient(t, fake.URL)

	result, err := runQueryRecords(t, client, salesforce.QueryRecordsInput{
		SOQL:     "SELECT Id, Email, Account.Name, Amount__c FROM :object WHERE Email = :email AND Name != 'a :literal' LIMIT 10",
		Bindings: map[string]salesforce.SOQLValue{"object": salesforce.SOQLIdentifier("Contact"), "email": salesforce.SOQLString("o'brien@example.com")},
	})
	require.NoError(t, err)
	require.Equal(t, salesforce.QueryRecordsBranchFound, result.Branch)
	require.Equal(t, int64(450), result.Value.TotalSize)
	require.False(t, result.Value.IsDone)
	require.Equal(t, firstPageCursor, result.Value.NextRecordsCursor)
	require.Len(t, result.Value.Records, 1)
	record := result.Value.Records[0]
	require.Equal(t, "Contact", record.Type)
	require.Equal(t, "003RM000001AbcdYAC", record.ID)
	email, isString := record.StringField("email")
	require.True(t, isString)
	require.Equal(t, "o'brien@example.com", email)
	require.JSONEq(t, `75000.50`, string(record.Fields["Amount__c"]))
	require.NotContains(t, record.Fields, "attributes")
	require.Equal(t, "18/15000", result.Receipt.Metadata["apiUsage"])

	requests := fake.recorded()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodGet, requests[0].method)
	require.Equal(t, "/services/data/v62.0/query", requests[0].path)
	require.Equal(t, `SELECT Id, Email, Account.Name, Amount__c FROM Contact WHERE Email = 'o\'brien@example.com' AND Name != 'a :literal' LIMIT 10`, requests[0].query["q"][0])
	require.Equal(t, "batchSize=200", requests[0].header.Get("Sforce-Query-Options"))
	require.Equal(t, "Bearer "+salesforceTestToken, requests[0].header.Get("Authorization"))
}

func TestQueryRecordsFollowsTheCursorAndSelectsNotFoundOnlyForAnEmptyResult(t *testing.T) {
	responses := []string{
		`{"totalSize":450,"done":true,"records":[{"attributes":{"type":"Contact"},"Id":"003RM000001AbcdYAC"}]}`,
		`{"totalSize":0,"done":true,"records":[]}`,
		`{"totalSize":37,"done":true,"records":[]}`,
	}
	fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(t, response, http.StatusOK, responses[0])
		responses = responses[1:]
	})
	client := newSalesforceClient(t, fake.URL, salesforce.Config{QueryBatchSize: 500})

	lastPage, err := runQueryRecords(t, client, salesforce.QueryRecordsInput{NextRecordsCursor: firstPageCursor, BatchSize: 1000})
	require.NoError(t, err)
	require.Equal(t, salesforce.QueryRecordsBranchFound, lastPage.Branch)
	require.True(t, lastPage.Value.IsDone)
	require.Empty(t, lastPage.Value.NextRecordsCursor)

	empty, err := runQueryRecords(t, client, salesforce.QueryRecordsInput{SOQL: "SELECT Id FROM Lead WHERE IsConverted = :converted", Bindings: map[string]salesforce.SOQLValue{"converted": salesforce.SOQLBoolean(false)}})
	require.NoError(t, err)
	require.Equal(t, salesforce.QueryRecordsBranchNotFound, empty.Branch)
	require.Equal(t, sdkgo.FailureNotFound, empty.Failure.Kind)

	count, err := runQueryRecords(t, client, salesforce.QueryRecordsInput{SOQL: "SELECT COUNT() FROM Contact"})
	require.NoError(t, err)
	require.Equal(t, salesforce.QueryRecordsBranchFound, count.Branch)
	require.Equal(t, int64(37), count.Value.TotalSize)

	requests := fake.recorded()
	require.Equal(t, "/services/data/v62.0/query/01gRM000001Bxy1YAC-200", requests[0].path)
	require.Empty(t, requests[0].query)
	require.Equal(t, "batchSize=1000", requests[0].header.Get("Sforce-Query-Options"))
	require.Equal(t, "SELECT Id FROM Lead WHERE IsConverted = FALSE", requests[1].query["q"][0])
	require.Equal(t, "batchSize=500", requests[1].header.Get("Sforce-Query-Options"))
}

func TestQueryRecordsRejectsInvalidInputWithoutAProviderRequest(t *testing.T) {
	fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(t, response, http.StatusOK, `{"totalSize":0,"done":true,"records":[]}`)
	})
	client := newSalesforceClient(t, fake.URL)
	for name, input := range map[string]salesforce.QueryRecordsInput{
		"blank":                 {},
		"soql and cursor":       {SOQL: "SELECT Id FROM Contact", NextRecordsCursor: firstPageCursor},
		"cursor with bindings":  {NextRecordsCursor: firstPageCursor, Bindings: map[string]salesforce.SOQLValue{"a": salesforce.SOQLNull()}},
		"foreign cursor":        {NextRecordsCursor: "https://attacker.example/services/data/v62.0/query/01gRM000001Bxy1YAC-200"},
		"other version cursor":  {NextRecordsCursor: "/services/data/v61.0/query/01gRM000001Bxy1YAC-200"},
		"missing binding":       {SOQL: "SELECT Id FROM Contact WHERE Email = :email"},
		"unused binding":        {SOQL: "SELECT Id FROM Contact", Bindings: map[string]salesforce.SOQLValue{"email": salesforce.SOQLString("x")}},
		"identifier expression": {SOQL: "SELECT Id FROM :object", Bindings: map[string]salesforce.SOQLValue{"object": salesforce.SOQLIdentifier("Contact WHERE Name != null")}},
		"small batch":           {SOQL: "SELECT Id FROM Contact", BatchSize: 199},
		"large batch":           {SOQL: "SELECT Id FROM Contact", BatchSize: 2001},
		"too long":              {SOQL: "SELECT Id FROM Contact WHERE Id IN :ids", Bindings: map[string]salesforce.SOQLValue{"ids": salesforce.SOQLStringList(repeatedIDs(900))}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := runQueryRecords(t, client, input)
			require.NoError(t, err)
			require.Equal(t, salesforce.QueryRecordsBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "Name != null")
		})
	}
	require.Empty(t, fake.recorded())
}

func TestQueryRecordsMapsSalesforceErrorsWithoutProviderMessages(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int
		errorCode   string
		branch      sdkgo.BranchID
		failureKind sdkgo.FailureKind
	}{
		{name: "malformed query", status: http.StatusBadRequest, errorCode: "MALFORMED_QUERY", branch: salesforce.QueryRecordsBranchQueryRejected, failureKind: sdkgo.FailureValidation},
		{name: "invalid field", status: http.StatusBadRequest, errorCode: "INVALID_FIELD", branch: salesforce.QueryRecordsBranchQueryRejected, failureKind: sdkgo.FailureValidation},
		{name: "expired cursor", status: http.StatusBadRequest, errorCode: "INVALID_QUERY_LOCATOR", branch: salesforce.QueryRecordsBranchQueryRejected, failureKind: sdkgo.FailureValidation},
		{name: "insufficient access", status: http.StatusBadRequest, errorCode: "INSUFFICIENT_ACCESS", branch: salesforce.QueryRecordsBranchProviderRejected, failureKind: sdkgo.FailureAuthorization},
		{name: "api disabled", status: http.StatusForbidden, errorCode: "API_DISABLED_FOR_ORG", branch: salesforce.QueryRecordsBranchProviderRejected, failureKind: sdkgo.FailureAuthorization},
		{name: "expired session", status: http.StatusUnauthorized, errorCode: "INVALID_SESSION_ID", branch: salesforce.QueryRecordsBranchProviderRejected, failureKind: sdkgo.FailureAuthentication},
		{name: "unknown version path", status: http.StatusNotFound, errorCode: "NOT_FOUND", branch: salesforce.QueryRecordsBranchProviderRejected, failureKind: sdkgo.FailureNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(t, response, test.status, `[{"message":"`+providerMessageSentinel+`","errorCode":"`+test.errorCode+`"}]`)
			})
			result, err := runQueryRecords(t, newSalesforceClient(t, fake.URL), salesforce.QueryRecordsInput{SOQL: "SELECT Id FROM Contact"})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.failureKind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, test.errorCode)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
			require.Len(t, fake.recorded(), 1)
		})
	}
}

func TestQueryRecordsRetriesLimitsAndOutages(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int
		body        string
		failureKind sdkgo.FailureKind
	}{
		{name: "api request limit", status: http.StatusForbidden, body: `[{"message":"TotalRequests Limit exceeded.","errorCode":"REQUEST_LIMIT_EXCEEDED"}]`, failureKind: sdkgo.FailureRateLimit},
		{name: "server unavailable", status: http.StatusServiceUnavailable, body: `[{"message":"maintenance","errorCode":"SERVER_UNAVAILABLE"}]`, failureKind: sdkgo.FailureAvailability},
		{name: "gateway error page", status: http.StatusBadGateway, body: `<html>` + providerMessageSentinel + `</html>`, failureKind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(t, response, test.status, test.body)
			})
			_, err := runQueryRecords(t, newSalesforceClient(t, fake.URL), salesforce.QueryRecordsInput{SOQL: "SELECT Id FROM Contact"})
			requireRetry(t, err, test.failureKind)
		})
	}
}

func TestQueryRecordsBoundsAndValidatesThePage(t *testing.T) {
	for name, body := range map[string]string{
		"oversized":            `{"totalSize":1,"done":true,"records":[{"attributes":{"type":"Contact"},"Description":"` + strings.Repeat("x", 2048) + `"}]}`,
		"missing done":         `{"totalSize":1,"records":[]}`,
		"unfinished no cursor": `{"totalSize":900,"done":false,"records":[]}`,
		"foreign cursor":       `{"totalSize":900,"done":false,"nextRecordsUrl":"https://attacker.example/next","records":[]}`,
		"record without type":  `{"totalSize":1,"done":true,"records":[{"Id":"003RM000001AbcdYAC"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(t, response, http.StatusOK, body)
			})
			result, err := runQueryRecords(t, newSalesforceClient(t, fake.URL, salesforce.Config{MaxResponseBytes: 1024}), salesforce.QueryRecordsInput{SOQL: "SELECT Id FROM Contact"})
			require.NoError(t, err)
			require.Equal(t, salesforce.QueryRecordsBranchInvalidResponse, result.Branch)
		})
	}
}

func repeatedIDs(count int) []string {
	ids := make([]string, count)
	for index := range ids {
		ids[index] = "003RM000001AbcdYAC"
	}
	return ids
}
