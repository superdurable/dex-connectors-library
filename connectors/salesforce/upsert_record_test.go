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

func runUpsert(t *testing.T, client *salesforce.Client, input salesforce.UpsertRecordByExternalIDInput) (sdkgo.MutationResult[salesforce.UpsertRecordByExternalIDOutput], error) {
	t.Helper()
	return sdkgo.RunMutation(newDexContext("upsert"), client.UpsertRecordByExternalID(), salesforceConnection, input)
}

func contactUpsert(externalID string) salesforce.UpsertRecordByExternalIDInput {
	return salesforce.UpsertRecordByExternalIDInput{
		SObjectType: "Contact", ExternalIDField: "ERP_Id__c", ExternalIDValue: externalID,
		Fields: map[string]json.RawMessage{"LastName": json.RawMessage(`"Raman"`), "Email": json.RawMessage(`"priya@meridian.example.com"`)},
	}
}

func TestUpsertRecordByExternalIDCreatesThenUpdatesTheSameRecord(t *testing.T) {
	responses := []struct {
		status int
		body   string
	}{
		{status: http.StatusCreated, body: `{"id":"003RM000001AbcdYAC","success":true,"errors":[],"created":true}`},
		{status: http.StatusOK, body: `{"id":"003RM000001AbcdYAC","success":true,"errors":[],"created":false}`},
	}
	fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(t, response, responses[0].status, responses[0].body)
		responses = responses[1:]
	})
	client := newSalesforceClient(t, fake.URL)

	created, err := runUpsert(t, client, contactUpsert("ERP/88 213"))
	require.NoError(t, err)
	require.Equal(t, salesforce.UpsertRecordByExternalIDBranchUpserted, created.Branch)
	require.Equal(t, salesforce.UpsertRecordByExternalIDOutput{ID: "003RM000001AbcdYAC", IsCreated: true}, created.Value)
	require.Equal(t, "003RM000001AbcdYAC", created.Receipt.ProviderObjectID)
	require.NotEmpty(t, created.Receipt.IdempotencyKey)

	updated, err := runUpsert(t, client, contactUpsert("ERP/88 213"))
	require.NoError(t, err)
	require.Equal(t, salesforce.UpsertRecordByExternalIDOutput{ID: "003RM000001AbcdYAC"}, updated.Value)

	requests := fake.recorded()
	require.Len(t, requests, 2)
	require.Equal(t, http.MethodPatch, requests[0].method)
	require.Equal(t, "/services/data/v62.0/sobjects/Contact/ERP_Id__c/ERP%2F88%20213", requests[0].rawPath)
	require.Equal(t, "application/json", requests[0].header.Get("Content-Type"))
	require.JSONEq(t, `{"LastName":"Raman","Email":"priya@meridian.example.com"}`, string(requests[0].body))
}

func TestUpsertRecordByExternalIDRoutesMultipleMatchesAndRejections(t *testing.T) {
	for _, test := range []struct {
		name           string
		status         int
		body           string
		branch         sdkgo.BranchID
		matchingIDs    []string
		providerErrors []salesforce.ProviderError
	}{
		{
			name: "multiple matches", status: http.StatusMultipleChoices,
			body:   `["/services/data/v62.0/sobjects/Contact/003RM000001AbcdYAC","/services/data/v62.0/sobjects/Contact/003RM000002AbcdYAC"]`,
			branch: salesforce.UpsertRecordByExternalIDBranchMultipleMatches, matchingIDs: []string{"003RM000001AbcdYAC", "003RM000002AbcdYAC"},
		},
		{
			name: "required field", status: http.StatusBadRequest,
			body:           `[{"message":"Required fields are missing: [LastName] ` + providerMessageSentinel + `","errorCode":"REQUIRED_FIELD_MISSING","fields":["LastName"]}]`,
			branch:         salesforce.UpsertRecordByExternalIDBranchRecordRejected,
			providerErrors: []salesforce.ProviderError{{ErrorCode: "REQUIRED_FIELD_MISSING", Fields: []string{"LastName"}}},
		},
		{
			name: "restricted picklist", status: http.StatusBadRequest,
			body:           `[{"message":"bad value for restricted picklist field: Won","errorCode":"INVALID_OR_NULL_FOR_RESTRICTED_PICKLIST","fields":["StageName","not a field!"]}]`,
			branch:         salesforce.UpsertRecordByExternalIDBranchRecordRejected,
			providerErrors: []salesforce.ProviderError{{ErrorCode: "INVALID_OR_NULL_FOR_RESTRICTED_PICKLIST", Fields: []string{"StageName"}}},
		},
		{
			name: "duplicate rule", status: http.StatusBadRequest,
			body:           `[{"message":"Use one of these records?","errorCode":"DUPLICATES_DETECTED","fields":[]}]`,
			branch:         salesforce.UpsertRecordByExternalIDBranchRecordRejected,
			providerErrors: []salesforce.ProviderError{{ErrorCode: "DUPLICATES_DETECTED"}},
		},
		{
			name: "unknown external ID field", status: http.StatusNotFound,
			body:           `[{"message":"Provided external ID field does not exist or is not accessible: ERP_Id__c","errorCode":"NOT_FOUND"}]`,
			branch:         salesforce.UpsertRecordByExternalIDBranchProviderRejected,
			providerErrors: []salesforce.ProviderError{{ErrorCode: "NOT_FOUND"}},
		},
		{
			name: "no create access", status: http.StatusBadRequest,
			body:           `[{"message":"insufficient access rights on object id","errorCode":"INSUFFICIENT_ACCESS_OR_READONLY"}]`,
			branch:         salesforce.UpsertRecordByExternalIDBranchProviderRejected,
			providerErrors: []salesforce.ProviderError{{ErrorCode: "INSUFFICIENT_ACCESS_OR_READONLY"}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := runUpsert(t, newSalesforceClient(t, fake.URL), contactUpsert("ERP-88213"))
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.matchingIDs, result.Value.MatchingRecordIDs)
			require.Equal(t, test.providerErrors, result.Value.ProviderErrors)
			require.Empty(t, result.Value.ID)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "SENTINEL")
			require.NotContains(t, string(encoded), "Required fields are missing")
			require.Len(t, fake.recorded(), 1)
		})
	}
}

func TestUpsertRecordByExternalIDRetriesEveryAmbiguousOutcome(t *testing.T) {
	for _, test := range []struct {
		name        string
		respond     func(t *testing.T, response http.ResponseWriter)
		failureKind sdkgo.FailureKind
	}{
		{name: "server error", failureKind: sdkgo.FailureAvailability, respond: func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusInternalServerError, `[{"message":"`+providerMessageSentinel+`","errorCode":"UNKNOWN_EXCEPTION"}]`)
		}},
		{name: "concurrent duplicate", failureKind: sdkgo.FailureConflict, respond: func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadRequest, `[{"message":"duplicate value found: ERP_Id__c","errorCode":"DUPLICATE_VALUE","fields":[]}]`)
		}},
		{name: "row lock", failureKind: sdkgo.FailureConflict, respond: func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadRequest, `[{"message":"unable to obtain exclusive access to this record","errorCode":"UNABLE_TO_LOCK_ROW"}]`)
		}},
		{name: "unreadable success", failureKind: sdkgo.FailureProtocol, respond: func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusCreated, `{"success":true}`)
		}},
		{name: "dropped connection", failureKind: sdkgo.FailureTransport, respond: func(t *testing.T, response http.ResponseWriter) {
			connection, _, err := response.(http.Hijacker).Hijack()
			require.NoError(t, err)
			require.NoError(t, connection.Close())
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeSalesforce(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
				test.respond(t, response)
			})
			_, err := runUpsert(t, newSalesforceClient(t, fake.URL), contactUpsert("ERP-88213"))
			requireRetry(t, err, test.failureKind)
		})
	}
}

func TestUpsertRecordByExternalIDValidatesInputWithoutAProviderRequest(t *testing.T) {
	fake := newFakeSalesforce(t, func(http.ResponseWriter, *http.Request, []byte) {})
	client := newSalesforceClient(t, fake.URL)
	withFields := func(fields map[string]json.RawMessage) salesforce.UpsertRecordByExternalIDInput {
		input := contactUpsert("ERP-88213")
		input.Fields = fields
		return input
	}
	for name, input := range map[string]salesforce.UpsertRecordByExternalIDInput{
		"blank value":            contactUpsert(""),
		"padded value":           contactUpsert(" ERP-1"),
		"control value":          contactUpsert("ERP\n1"),
		"dot segment value":      contactUpsert(".."),
		"Id as external ID":      {SObjectType: "Contact", ExternalIDField: "Id", ExternalIDValue: "003RM000001AbcdYAC"},
		"bad object":             {SObjectType: "Contact/ERP_Id__c", ExternalIDField: "ERP_Id__c", ExternalIDValue: "1"},
		"external ID in body":    withFields(map[string]json.RawMessage{"erp_id__c": json.RawMessage(`"ERP-88213"`)}),
		"Id in body":             withFields(map[string]json.RawMessage{"Id": json.RawMessage(`"003RM000001AbcdYAC"`)}),
		"invalid JSON value":     withFields(map[string]json.RawMessage{"LastName": json.RawMessage(`Raman`)}),
		"relationship field key": withFields(map[string]json.RawMessage{"Account.Name": json.RawMessage(`"Acme"`)}),
	} {
		t.Run(name, func(t *testing.T) {
			result, err := runUpsert(t, client, input)
			require.NoError(t, err)
			require.Equal(t, salesforce.UpsertRecordByExternalIDBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Empty(t, fake.recorded())
}
