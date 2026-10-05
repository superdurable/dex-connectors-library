// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func contactUpsertInput() crm.UpsertRecordInput {
	return crm.UpsertRecordInput{
		Module: crm.ModuleContacts, DuplicateCheckFields: []string{crm.FieldEmail},
		Fields: map[string]json.RawMessage{
			crm.FieldEmail: crm.TextFieldValue("jane@acme.example.com"), crm.FieldLastName: crm.TextFieldValue("Smith"),
			crm.FieldAccountName: crm.LookupFieldValue(testAccountID),
		},
	}
}

func TestUpsertRecordSendsOneRecordWithItsDuplicateCheckFieldsAndReportsTheAction(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeValue(t, response, http.StatusCreated, writeSuccessJSON("insert", testContactID, nil))
			return
		}
		writeValue(t, response, http.StatusOK, writeSuccessJSON("update", testContactID, "Email"))
	})
	client := newCRMClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newCRMDexContext("upsert"), client.UpsertRecord(), crmConnection, contactUpsertInput())
	require.NoError(t, err)
	require.Equal(t, crm.UpsertRecordBranchUpserted, result.Branch)
	require.Equal(t, crm.ModuleContacts, result.Value.Module)
	require.Equal(t, testContactID, result.Value.ID)
	require.True(t, result.Value.IsCreated)
	require.Empty(t, result.Value.DuplicateField)
	require.True(t, result.Value.CreatedAt.Equal(time.Date(2026, 1, 28, 7, 30, 5, 0, time.UTC)))
	require.True(t, result.Value.ModifiedAt.Equal(time.Date(2026, 1, 28, 7, 30, 5, 0, time.UTC)))
	require.Equal(t, testContactID, result.Receipt.ProviderObjectID)
	requireNoSentinel(t, result)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/crm/v8/Contacts/upsert", request.path)
	require.JSONEq(t, `{"data":[{"Email":"jane@acme.example.com","Last_Name":"Smith","Account_Name":{"id":"`+testAccountID+`"}}],
		"duplicate_check_fields":["Email"]}`, request.body, "no trigger key keeps Zoho's default automations")

	result, err = sdkgo.RunMutation(newCRMDexContext("upsert-again"), client.UpsertRecord(), crmConnection, contactUpsertInput())
	require.NoError(t, err)
	require.False(t, result.Value.IsCreated)
	require.Equal(t, "Email", result.Value.DuplicateField)
}

func TestUpsertRecordResendsOnceAfterDuplicateDataAndThenSelectsConflict(t *testing.T) {
	duplicate := recordErrorJSON("DUPLICATE_DATA", "Email", map[string]any{
		"duplicate_record": map[string]any{"id": testContactID, "module": map[string]any{"api_name": "Contacts"}, "Owner": map[string]any{"name": "SENTINEL"}},
	})
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 1 {
			writeValue(t, response, http.StatusOK, writeSuccessJSON("update", testContactID, "Email"))
			return
		}
		writeValue(t, response, http.StatusBadRequest, duplicate)
	})
	client := newCRMClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newCRMDexContext("upsert"), client.UpsertRecord(), crmConnection, contactUpsertInput())
	require.NoError(t, err)
	require.Equal(t, crm.UpsertRecordBranchUpserted, result.Branch, "a concurrent attempt inserted first; the resend updated it")
	require.Equal(t, 2, provider.requestCount())

	result, err = sdkgo.RunMutation(newCRMDexContext("upsert-conflict"), client.UpsertRecord(), crmConnection, contactUpsertInput())
	require.NoError(t, err)
	require.Equal(t, crm.UpsertRecordBranchConflict, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, []crm.ProviderError{{Code: "DUPLICATE_DATA", Field: "Email", DuplicateRecordID: testContactID}}, result.Value.ProviderErrors)
	require.Equal(t, 4, provider.requestCount(), "one resend, then conflict")
	requireNoSentinel(t, result)
}

func TestUpsertRecordMapsRecordAndRequestRejections(t *testing.T) {
	for name, scenario := range map[string]struct {
		status int
		body   any
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		"mandatory field":       {http.StatusBadRequest, recordErrorJSON("MANDATORY_NOT_FOUND", "Last_Name", nil), crm.UpsertRecordBranchRecordRejected, sdkgo.FailureValidation},
		"multi-status record":   {http.StatusMultiStatus, recordErrorJSON("INVALID_DATA", "Amount", nil), crm.UpsertRecordBranchRecordRejected, sdkgo.FailureValidation},
		"locked":                {http.StatusBadRequest, recordErrorJSON("RECORD_LOCKED", "id", nil), crm.UpsertRecordBranchRecordRejected, sdkgo.FailureValidation},
		"no permission":         {http.StatusForbidden, map[string]any{"code": "NO_PERMISSION", "details": map[string]any{}, "message": "SENTINEL", "status": "error"}, crm.UpsertRecordBranchProviderRejected, sdkgo.FailureAuthorization},
		"authorization failed":  {http.StatusBadRequest, recordErrorJSON("AUTHORIZATION_FAILED", "Email", nil), crm.UpsertRecordBranchProviderRejected, sdkgo.FailureAuthorization},
		"invalid module":        {http.StatusBadRequest, map[string]any{"code": "INVALID_MODULE", "details": map[string]any{}, "message": "SENTINEL", "status": "error"}, crm.UpsertRecordBranchProviderRejected, sdkgo.FailureValidation},
		"success without an id": {http.StatusOK, map[string]any{"data": []any{map[string]any{"code": "SUCCESS", "status": "success", "details": map[string]any{}}}}, crm.UpsertRecordBranchInvalidResponse, sdkgo.FailureProtocol},
		"two results for one":   {http.StatusOK, map[string]any{"data": []any{map[string]any{}, map[string]any{}}}, crm.UpsertRecordBranchInvalidResponse, sdkgo.FailureProtocol},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeValue(t, response, scenario.status, scenario.body)
			})
			result, err := sdkgo.RunMutation(newCRMDexContext("upsert"), newCRMClient(t, provider.URL).UpsertRecord(), crmConnection, contactUpsertInput())
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			require.Equal(t, scenario.kind, result.Failure.Kind)
			requireNoSentinel(t, result)
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestUpsertRecordRetriesEveryUnconfirmedOutcome(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusInternalServerError, `{"code":"INTERNAL_ERROR","message":"SENTINEL","status":"error"}`)
	})
	_, err := sdkgo.RunMutation(newCRMDexContext("upsert"), newCRMClient(t, provider.URL).UpsertRecord(), crmConnection, contactUpsertInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
}

func TestUpsertRecordRequiresTheDuplicateCheckValueBeforeAnyRequest(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	client := newCRMClient(t, provider.URL)
	for name, change := range map[string]func(*crm.UpsertRecordInput){
		"missing value":  func(input *crm.UpsertRecordInput) { delete(input.Fields, crm.FieldEmail) },
		"null value":     func(input *crm.UpsertRecordInput) { input.Fields[crm.FieldEmail] = json.RawMessage(`null`) },
		"blank value":    func(input *crm.UpsertRecordInput) { input.Fields[crm.FieldEmail] = crm.TextFieldValue("  ") },
		"no check field": func(input *crm.UpsertRecordInput) { input.DuplicateCheckFields = nil },
		"record id":      func(input *crm.UpsertRecordInput) { input.Fields["id"] = crm.TextFieldValue(testContactID) },
		"dollar key":     func(input *crm.UpsertRecordInput) { input.Fields["$append_values"] = json.RawMessage(`{}`) },
		"invalid json":   func(input *crm.UpsertRecordInput) { input.Fields[crm.FieldLastName] = json.RawMessage(`{`) },
		"skip and triggers": func(input *crm.UpsertRecordInput) {
			input.ShouldSkipAutomation, input.Triggers = true, []crm.RecordTrigger{crm.RecordTriggerWorkflow}
		},
		"unknown trigger":      func(input *crm.UpsertRecordInput) { input.Triggers = []crm.RecordTrigger{"cadences"} },
		"repeated check field": func(input *crm.UpsertRecordInput) { input.DuplicateCheckFields = []string{"Email", "Email"} },
	} {
		input := contactUpsertInput()
		change(&input)
		result, err := sdkgo.RunMutation(newCRMDexContext("upsert"), client.UpsertRecord(), crmConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, crm.UpsertRecordBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.requestCount())
}

func TestUpsertRecordSendsAnEmptyTriggerListToSkipAutomation(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusCreated, writeSuccessJSON("insert", testContactID, nil))
	})
	client := newCRMClient(t, provider.URL)
	input := contactUpsertInput()
	input.ShouldSkipAutomation = true
	_, err := sdkgo.RunMutation(newCRMDexContext("upsert"), client.UpsertRecord(), crmConnection, input)
	require.NoError(t, err)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(provider.request(0).body), &body))
	require.JSONEq(t, `[]`, string(body["trigger"]))

	input = contactUpsertInput()
	input.Triggers = []crm.RecordTrigger{crm.RecordTriggerWorkflow}
	_, err = sdkgo.RunMutation(newCRMDexContext("upsert-workflow"), client.UpsertRecord(), crmConnection, input)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(provider.request(1).body), &body))
	require.JSONEq(t, `["workflow"]`, string(body["trigger"]))
}
