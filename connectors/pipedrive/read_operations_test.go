// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var dealRecord = `{"id":42,"title":"Acme renewal","owner_id":7,"person_id":501,"org_id":88,"pipeline_id":1,"stage_id":3,
	"status":"open","value":1500.5,"currency":"EUR","add_time":"2026-01-17T19:55:04Z","update_time":"2026-09-11T13:27:39Z",
	"custom_fields":{"` + sourceFieldKey + `":"webinar","` + tierFieldKey + `":null}}`

func TestSearchObjectsSendsTermFieldsAndFiltersAndReturnsPartialRecords(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[
			{"result_score":1.2,"item":{"id":42,"type":"deal","title":"Acme renewal","value":100,"currency":"USD","status":"open",
			 "owner":{"id":7},"stage":{"id":3,"name":"Qualified"},"person":{"id":501,"name":"Jane"},"organization":null,
			 "custom_fields":[],"notes":[],"is_archived":false}}
		]},"additional_data":{"next_cursor":"eyJmaWVsZCI6ImlkIn0"}}`)
	})
	client := newAPITokenClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newStepDexContext("search-deals"), client.SearchObjects(), pipedriveConnection, pipedrive.SearchObjectsInput{
		ObjectType: pipedrive.ObjectTypeDeals, Term: "Acme", Fields: []pipedrive.SearchField{pipedrive.SearchFieldTitle, pipedrive.SearchFieldTitle},
		PersonID: "501", Statuses: []pipedrive.DealStatus{pipedrive.DealStatusOpen}, Limit: 2, Cursor: "eyJmaWVsZCI6ImlkIn0x",
	})
	require.NoError(t, err)
	require.Equal(t, pipedrive.SearchObjectsBranchSearched, result.Branch)
	requests := provider.recordedRequests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodGet, requests[0].method)
	require.Equal(t, "/api/v2/deals/search", requests[0].path)
	require.Equal(t, testAPIToken, requests[0].apiToken)
	require.Empty(t, requests[0].authorization, "a Personal API token travels only in x-api-token")
	require.Equal(t, map[string][]string{
		"term": {"Acme"}, "fields": {"title"}, "person_id": {"501"}, "status": {"open"}, "limit": {"2"}, "cursor": {"eyJmaWVsZCI6ImlkIn0x"},
	}, requests[0].query)
	page := result.Value
	require.Equal(t, "eyJmaWVsZCI6ImlkIn0", page.NextCursor)
	require.Len(t, page.Objects, 1)
	deal := page.Objects[0]
	require.True(t, deal.IsPartial)
	require.Equal(t, "42", deal.ID)
	require.Equal(t, "Acme renewal", deal.Name)
	require.Equal(t, "7", deal.OwnerID)
	require.Equal(t, "3", deal.StageID)
	require.Equal(t, "501", deal.PersonID)
	require.Empty(t, deal.OrganizationID)
	require.Equal(t, pipedrive.DealStatusOpen, deal.Status)
	require.Equal(t, testCorrelationID, result.Receipt.ProviderRequestID)
}

func TestSearchObjectsFindsPersonsByExactEmailWithTheDefaultLimit(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"success":true,"data":{"items":[{"result_score":1,"item":{"id":9,"type":"person","name":"Jane Smith",
			"emails":["jane@acme.example.com"],"phones":[],"owner":{"id":7},"organization":{"id":88,"name":"Acme"}}}]},
			"additional_data":{"next_cursor":null}}`)
	})
	client := newAPITokenClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newStepDexContext("find-person"), client.SearchObjects(), pipedriveConnection, pipedrive.SearchObjectsInput{
		ObjectType: pipedrive.ObjectTypePersons, Term: "jane@acme.example.com", Fields: []pipedrive.SearchField{pipedrive.SearchFieldEmail}, ExactMatch: true,
	})
	require.NoError(t, err)
	require.Equal(t, map[string][]string{
		"term": {"jane@acme.example.com"}, "fields": {"email"}, "exact_match": {"true"}, "limit": {"25"},
	}, provider.recordedRequests()[0].query)
	require.Equal(t, []string{"jane@acme.example.com"}, result.Value.Objects[0].Emails)
	require.Equal(t, "88", result.Value.Objects[0].OrganizationID)
	require.Empty(t, result.Value.NextCursor)
}

func TestSearchObjectsRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeError(t, response, http.StatusInternalServerError)
	})
	client := newAPITokenClient(t, provider.URL)
	for name, input := range map[string]pipedrive.SearchObjectsInput{
		"unknown object type":             {ObjectType: "leads", Term: "Acme"},
		"one-character inexact term":      {ObjectType: pipedrive.ObjectTypeDeals, Term: "A"},
		"field the endpoint cannot use":   {ObjectType: pipedrive.ObjectTypeDeals, Term: "Acme", Fields: []pipedrive.SearchField{pipedrive.SearchFieldEmail}},
		"person filter outside deals":     {ObjectType: pipedrive.ObjectTypePersons, Term: "Jane", PersonID: "1"},
		"organization filter on orgs":     {ObjectType: pipedrive.ObjectTypeOrganizations, Term: "Acme", OrganizationID: "1"},
		"deleted status in a search":      {ObjectType: pipedrive.ObjectTypeDeals, Term: "Acme", Statuses: []pipedrive.DealStatus{pipedrive.DealStatusDeleted}},
		"limit above Pipedrive's maximum": {ObjectType: pipedrive.ObjectTypeDeals, Term: "Acme", Limit: 501},
		"cursor with a query separator":   {ObjectType: pipedrive.ObjectTypeDeals, Term: "Acme", Cursor: "abc&limit=500"},
	} {
		result, err := sdkgo.RunQuery(newStepDexContext("invalid-search"), client.SearchObjects(), pipedriveConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, pipedrive.SearchObjectsBranchDefect, result.Branch, name)
	}
	require.Empty(t, provider.recordedRequests())
}

func TestGetObjectReturnsEveryFieldAndTheAssociationGraph(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.path == "/api/v2/deals/404" {
			writeError(t, response, http.StatusNotFound)
			return
		}
		writeRecord(t, response, dealRecord)
	})
	client := newAPITokenClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newStepDexContext("get-deal"), client.GetObject(), pipedriveConnection, pipedrive.GetObjectInput{
		ObjectType: pipedrive.ObjectTypeDeals, ObjectID: "42",
	})
	require.NoError(t, err)
	require.Equal(t, pipedrive.GetObjectBranchFound, result.Branch)
	deal := result.Value
	require.Equal(t, pipedrive.CRMObject{
		ObjectType: pipedrive.ObjectTypeDeals, ID: "42", Name: "Acme renewal", OwnerID: "7", OrganizationID: "88", PersonID: "501",
		PipelineID: "1", StageID: "3", Status: pipedrive.DealStatusOpen,
		AddTime: time.Date(2026, time.January, 17, 19, 55, 4, 0, time.UTC), UpdateTime: time.Date(2026, time.September, 11, 13, 27, 39, 0, time.UTC),
		Fields: deal.Fields, CustomFields: deal.CustomFields,
	}, deal)
	var value float64
	isPresent, err := deal.DecodeField("value", &value)
	require.NoError(t, err)
	require.True(t, isPresent)
	require.Equal(t, 1500.5, value)
	require.NotContains(t, deal.Fields, "custom_fields", "custom fields are keyed separately")
	var source string
	isPresent, err = deal.DecodeCustomField(sourceFieldKey, &source)
	require.NoError(t, err)
	require.True(t, isPresent)
	require.Equal(t, "webinar", source)
	isPresent, err = deal.DecodeCustomField(tierFieldKey, &source)
	require.NoError(t, err)
	require.True(t, isPresent, "a null custom field is returned and leaves the destination unchanged")
	require.Equal(t, "webinar", source)
	require.Equal(t, "42", result.Receipt.ProviderObjectID)

	missing, err := sdkgo.RunQuery(newStepDexContext("get-missing"), client.GetObject(), pipedriveConnection, pipedrive.GetObjectInput{
		ObjectType: pipedrive.ObjectTypeDeals, ObjectID: "404",
	})
	require.NoError(t, err)
	require.Equal(t, pipedrive.GetObjectBranchNotFound, missing.Branch)
	require.Equal(t, sdkgo.FailureNotFound, missing.Failure.Kind)
	require.NotContains(t, missing.Failure.Message, providerMessageSentinel)
}

func TestGetObjectSelectsInvalidResponseForAnotherRecordOrABrokenEnvelope(t *testing.T) {
	for name, body := range map[string]string{
		"another record":      `{"success":true,"data":{"id":43,"title":"Other"}}`,
		"success false":       `{"success":false,"data":{"id":42}}`,
		"text owner":          `{"success":true,"data":{"id":42,"owner_id":"seven"}}`,
		"missing data":        `{"success":true}`,
		"array instead of id": `{"success":true,"data":[]}`,
	} {
		provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) { writeJSON(t, response, http.StatusOK, body) })
		client := newAPITokenClient(t, provider.URL)
		result, err := sdkgo.RunQuery(newStepDexContext("get-invalid"), client.GetObject(), pipedriveConnection, pipedrive.GetObjectInput{
			ObjectType: pipedrive.ObjectTypeDeals, ObjectID: "42",
		})
		require.NoError(t, err, name)
		require.Equal(t, pipedrive.GetObjectBranchInvalidResponse, result.Branch, name)
	}
}

func TestListObjectsPollsRecordsChangedSinceAndPagesWithTheCursor(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"success":true,"data":[`+dealRecord+`,{"id":43,"title":"Beta","owner_id":7,"person_id":null,"org_id":null,
			"pipeline_id":1,"stage_id":4,"status":"won","custom_fields":{}}],"additional_data":{"next_cursor":"bmV4dA=="}}`)
	})
	client := newAPITokenClient(t, provider.URL)
	since := time.Date(2026, time.September, 1, 8, 30, 0, 0, time.FixedZone("CEST", 2*60*60))
	result, err := sdkgo.RunQuery(newStepDexContext("list-deals"), client.ListObjects(), pipedriveConnection, pipedrive.ListObjectsInput{
		ObjectType: pipedrive.ObjectTypeDeals, UpdatedSince: since, PipelineID: "1", OwnerID: "7",
		Statuses:        []pipedrive.DealStatus{pipedrive.DealStatusOpen, pipedrive.DealStatusWon, pipedrive.DealStatusDeleted},
		CustomFieldKeys: []string{sourceFieldKey}, Limit: 100,
	})
	require.NoError(t, err)
	require.Equal(t, pipedrive.ListObjectsBranchListed, result.Branch)
	request := provider.recordedRequests()[0]
	require.Equal(t, "/api/v2/deals", request.path)
	require.Equal(t, map[string][]string{
		"updated_since": {"2026-09-01T06:30:00Z"}, "sort_by": {"update_time"}, "pipeline_id": {"1"}, "owner_id": {"7"},
		"status": {"open,won,deleted"}, "custom_fields": {sourceFieldKey}, "limit": {"100"},
	}, request.query)
	require.Len(t, result.Value.Objects, 2)
	require.False(t, result.Value.Objects[0].IsPartial)
	require.Equal(t, pipedrive.DealStatusWon, result.Value.Objects[1].Status)
	require.Empty(t, result.Value.Objects[1].PersonID)
	require.Equal(t, "bmV4dA==", result.Value.NextCursor)
}

func TestListObjectsRejectsFiltersTheObjectTypeDoesNotHave(t *testing.T) {
	provider := newRecordingPipedrive(t, func(response http.ResponseWriter, _ recordedRequest) { writeJSON(t, response, http.StatusOK, `{}`) })
	client := newAPITokenClient(t, provider.URL)
	until := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	for name, input := range map[string]pipedrive.ListObjectsInput{
		"stage filter on persons":      {ObjectType: pipedrive.ObjectTypePersons, StageID: "3"},
		"organization filter on orgs":  {ObjectType: pipedrive.ObjectTypeOrganizations, OrganizationID: "3"},
		"statuses on organizations":    {ObjectType: pipedrive.ObjectTypeOrganizations, Statuses: []pipedrive.DealStatus{pipedrive.DealStatusOpen}},
		"until before since":           {ObjectType: pipedrive.ObjectTypeDeals, UpdatedSince: until, UpdatedUntil: until},
		"unknown sort":                 {ObjectType: pipedrive.ObjectTypeDeals, SortBy: "title"},
		"too many custom field keys":   {ObjectType: pipedrive.ObjectTypeDeals, CustomFieldKeys: make([]string, 16)},
		"owner ID that is not decimal": {ObjectType: pipedrive.ObjectTypeDeals, OwnerID: "07"},
	} {
		result, err := sdkgo.RunQuery(newStepDexContext("invalid-list"), client.ListObjects(), pipedriveConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, pipedrive.ListObjectsBranchDefect, result.Branch, name)
	}
	require.Empty(t, provider.recordedRequests())
}

func TestValueHelpersEncodeJSONForWrites(t *testing.T) {
	require.JSONEq(t, `"Jane \"JJ\" Smith"`, string(pipedrive.StringValue(`Jane "JJ" Smith`)))
	require.Equal(t, json.RawMessage("17"), pipedrive.IDValue("17"))
	require.False(t, json.Valid(pipedrive.IDValue("seventeen")), "an invalid ID becomes invalid JSON that the operation rejects")
}
