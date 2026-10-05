// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Custom field keys are built at run time, so no token-shaped 40-character literal is checked in.
var (
	sourceFieldKey = strings.Repeat("ab", 20)
	tierFieldKey   = strings.Repeat("cd", 20)
)

func dealInput() pipedrive.CreateObjectInput {
	return pipedrive.CreateObjectInput{
		ObjectType: pipedrive.ObjectTypeDeals,
		Fields: map[string]json.RawMessage{
			"title": pipedrive.StringValue("Acme renewal"), "person_id": pipedrive.IDValue("501"), "stage_id": pipedrive.IDValue("3"),
		},
		CustomFields: map[string]json.RawMessage{sourceFieldKey: pipedrive.StringValue("webinar")},
	}
}

func TestCreateObjectRecordsTheCheckpointThenSendsOneCreate(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) { writeRecord(t, response, dealRecord) })
	client := newAPITokenClient(t, provider.URL)
	ctx := newStepDexContext("create-deal")
	result, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, pipedrive.CreateObjectBranchCreated, result.Branch)
	require.Equal(t, "42", result.Value.ID)
	require.Equal(t, "42", result.Receipt.ProviderObjectID)
	require.True(t, ctx.hasHeartbeat(), "the dispatch checkpoint stays recorded after a create")
	requests := provider.recordedRequests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].method)
	require.Equal(t, "/api/v2/deals", requests[0].path)
	require.Equal(t, "application/json", requests[0].contentType)
	require.JSONEq(t, `{"title":"Acme renewal","person_id":501,"stage_id":3,"custom_fields":{"`+sourceFieldKey+`":"webinar"}}`, string(requests[0].body))

	again, err := sdkgo.RunMutation(ctx, client.CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, again.Branch, "a later attempt of the same Step execution never sends the create again")
	require.Len(t, provider.recordedRequests(), 1)
}

func TestCreateObjectSelectsUncertainWhenTheOutcomeIsUnknown(t *testing.T) {
	for name, status := range map[string]int{"server error": http.StatusInternalServerError, "gateway timeout": http.StatusGatewayTimeout} {
		provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) { writeError(t, response, status) })
		client := newAPITokenClient(t, provider.URL)
		result, err := sdkgo.RunMutation(newStepDexContext("create-unknown"), client.CreateObject(), pipedriveConnection, dealInput())
		require.NoError(t, err, name)
		require.Equal(t, sdkgo.UncertainBranchID, result.Branch, name)
		require.Equal(t, sdkgo.FailureAvailability, result.Failure.Kind, name)
		require.NotContains(t, result.Failure.Message, providerMessageSentinel)
	}
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"id":"x"}}`)
	})
	result, err := sdkgo.RunMutation(newStepDexContext("create-invalid"), newAPITokenClient(t, provider.URL).CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch, "an unreadable success may still have created the deal")
}

func TestCreateObjectRetriesABurstLimitAndClearsTheCheckpoint(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		response.Header().Set("X-Ratelimit-Reset", "1")
		response.Header().Set("X-Daily-Ratelimit-Token-Remaining", "29000")
		writeError(t, response, http.StatusTooManyRequests)
	})
	ctx := newStepDexContext("create-burst")
	_, err := sdkgo.RunMutation(ctx, newAPITokenClient(t, provider.URL).CreateObject(), pipedriveConnection, dealInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
	require.False(t, ctx.hasHeartbeat(), "Pipedrive refused the create, so the next attempt may send it")
}

func TestCreateObjectRejectsAnInvalidDealWithoutSendingIt(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) { writeError(t, response, http.StatusBadRequest) })
	client := newAPITokenClient(t, provider.URL)
	for name, input := range map[string]pipedrive.CreateObjectInput{
		"missing title":          {ObjectType: pipedrive.ObjectTypeDeals, Fields: map[string]json.RawMessage{"value": json.RawMessage("1")}},
		"blank person name":      {ObjectType: pipedrive.ObjectTypePersons, Fields: map[string]json.RawMessage{"name": pipedrive.StringValue(" ")}},
		"invalid owner ID":       {ObjectType: pipedrive.ObjectTypeDeals, Fields: map[string]json.RawMessage{"title": pipedrive.StringValue("A"), "owner_id": pipedrive.IDValue("x7")}},
		"custom field by label":  {ObjectType: pipedrive.ObjectTypeDeals, Fields: map[string]json.RawMessage{"title": pipedrive.StringValue("A")}, CustomFields: map[string]json.RawMessage{"Lead source": pipedrive.StringValue("x")}},
		"custom_fields in Field": {ObjectType: pipedrive.ObjectTypeDeals, Fields: map[string]json.RawMessage{"title": pipedrive.StringValue("A"), "custom_fields": json.RawMessage("{}")}},
	} {
		result, err := sdkgo.RunMutation(newStepDexContext("create-invalid-input"), client.CreateObject(), pipedriveConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, pipedrive.CreateObjectBranchDefect, result.Branch, name)
	}
	require.Empty(t, provider.recordedRequests())
	result, err := sdkgo.RunMutation(newStepDexContext("create-rejected"), client.CreateObject(), pipedriveConnection, dealInput())
	require.NoError(t, err)
	require.Equal(t, pipedrive.CreateObjectBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
}

func TestCreateObjectRecordsNothingWhenDexRefusesTheCheckpoint(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) { writeRecord(t, response, dealRecord) })
	_, err := sdkgo.RunMutation(refusingHeartbeatContext{newStepDexContext("create-no-checkpoint")}, newAPITokenClient(t, provider.URL).CreateObject(), pipedriveConnection, dealInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Empty(t, provider.recordedRequests())
}

type refusingHeartbeatContext struct{ *stepDexContext }

func (refusingHeartbeatContext) RecordHeartbeat(any) error { return errors.New("heartbeat refused") }

func TestUpdateObjectPatchesNamedFieldsAndMapsAMissingRecord(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.path == "/api/v2/deals/404" {
			writeError(t, response, http.StatusNotFound)
			return
		}
		writeRecord(t, response, dealRecord)
	})
	client := newAPITokenClient(t, provider.URL)
	input := pipedrive.UpdateObjectInput{
		ObjectType: pipedrive.ObjectTypeDeals, ObjectID: "42",
		Fields: map[string]json.RawMessage{"stage_id": pipedrive.IDValue("3"), "owner_id": pipedrive.IDValue("7")},
	}
	result, err := sdkgo.RunMutation(newStepDexContext("update-deal"), client.UpdateObject(), pipedriveConnection, input)
	require.NoError(t, err)
	require.Equal(t, pipedrive.UpdateObjectBranchUpdated, result.Branch)
	require.Equal(t, "3", result.Value.StageID)
	request := provider.recordedRequests()[0]
	require.Equal(t, http.MethodPatch, request.method)
	require.Equal(t, "/api/v2/deals/42", request.path)
	require.JSONEq(t, `{"stage_id":3,"owner_id":7}`, string(request.body))

	input.ObjectID = "404"
	missing, err := sdkgo.RunMutation(newStepDexContext("update-missing"), client.UpdateObject(), pipedriveConnection, input)
	require.NoError(t, err)
	require.Equal(t, pipedrive.UpdateObjectBranchNotFound, missing.Branch)
	empty, err := sdkgo.RunMutation(newStepDexContext("update-empty"), client.UpdateObject(), pipedriveConnection, pipedrive.UpdateObjectInput{
		ObjectType: pipedrive.ObjectTypeDeals, ObjectID: "42",
	})
	require.NoError(t, err)
	require.Equal(t, pipedrive.UpdateObjectBranchDefect, empty.Branch)
}

func TestUpdateObjectRetriesAnUnconfirmedUpdateBecauseItIsSafeToRepeat(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) { writeError(t, response, http.StatusBadGateway) })
	_, err := sdkgo.RunMutation(newStepDexContext("update-retry"), newAPITokenClient(t, provider.URL).UpdateObject(), pipedriveConnection, pipedrive.UpdateObjectInput{
		ObjectType: pipedrive.ObjectTypeDeals, ObjectID: "42", Fields: map[string]json.RawMessage{"stage_id": pipedrive.IDValue("3")},
	})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	for _, branch := range pipedrive.UpdateObjectDefinition.Branches {
		require.NotEqual(t, sdkgo.UncertainBranchID, branch.ID)
	}
}
