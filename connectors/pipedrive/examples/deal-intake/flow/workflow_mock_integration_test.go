//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dealintake

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive/pipedrivemock"
)

func TestNewLeadCreatesDealWithMockConnectionAndRealDex(t *testing.T) {
	mock := pipedrivemock.New(t, ConnectionName)
	mock.SearchObjects().When(func(input pipedrive.SearchObjectsInput) bool {
		return input.ObjectType == pipedrive.ObjectTypeOrganizations && input.Term == "Acme Corp"
	}, pipedrivemock.SearchObjectsSearched(pipedrive.ObjectPage{
		ObjectType: pipedrive.ObjectTypeOrganizations,
		Objects:    []pipedrive.CRMObject{{ObjectType: pipedrive.ObjectTypeOrganizations, ID: "201", Name: "Acme Corp", IsPartial: true}},
	}, nil))
	mock.ListObjects().When(func(input pipedrive.ListObjectsInput) bool {
		return input.ObjectType == pipedrive.ObjectTypeDeals && input.PersonID == "101"
	}, pipedrivemock.ListObjectsListed(pipedrive.ObjectPage{ObjectType: pipedrive.ObjectTypeDeals, Objects: []pipedrive.CRMObject{}}, nil))
	harness := newIntakeHarnessForConnection(t, mock.Connection())
	intake := harness.runIntake(t, "mock-new-lead", integrationLead())
	require.Equal(t, PhaseCompleted, intake.Phase)
	require.Equal(t, integrationLead(), intake.Lead)
	require.Equal(t, DealActionCreated, intake.DealAction)
	require.Equal(t, "201", intake.OrganizationID)
	require.Equal(t, "101", intake.PersonID)
	require.Equal(t, "301", intake.DealID)
	require.Equal(t, "3", intake.StageID)
	require.Equal(t, 1, intake.OrganizationMatches)

	searchCalls := mock.SearchObjects().Calls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, pipedrive.SearchObjectsInput{
		ObjectType: pipedrive.ObjectTypeOrganizations, Term: "Acme Corp", Fields: []pipedrive.SearchField{pipedrive.SearchFieldName}, ExactMatch: true, Limit: 10,
	}, searchCalls[0].Input)
	upsertCalls := mock.UpsertObject().Calls()
	require.Len(t, upsertCalls, 1)
	require.Equal(t, pipedrive.UpsertObjectInput{
		ObjectType: pipedrive.ObjectTypePersons, IDProperty: pipedrive.IdentityPropertyEmail, IDValue: "jane@acme.example.com",
		Fields: map[string]json.RawMessage{"name": pipedrive.StringValue("Jane Smith"), "org_id": pipedrive.IDValue("201"), "owner_id": pipedrive.IDValue("7")},
	}, upsertCalls[0].Input)
	listCalls := mock.ListObjects().Calls()
	require.Len(t, listCalls, 1)
	require.Equal(t, pipedrive.ListObjectsInput{
		ObjectType: pipedrive.ObjectTypeDeals, PersonID: "101", PipelineID: "1", Statuses: []pipedrive.DealStatus{pipedrive.DealStatusOpen},
		SortBy: pipedrive.ListSortFieldUpdateTime, SortDirection: pipedrive.SortDirectionDescending, Limit: 1,
	}, listCalls[0].Input)
	createCalls := mock.CreateObject().Calls()
	require.Len(t, createCalls, 1)
	require.Equal(t, pipedrive.CreateObjectInput{
		ObjectType: pipedrive.ObjectTypeDeals,
		Fields: map[string]json.RawMessage{
			"title": pipedrive.StringValue("Acme platform deal"), "person_id": pipedrive.IDValue("101"),
			"org_id": pipedrive.IDValue("201"), "pipeline_id": pipedrive.IDValue("1"), "stage_id": pipedrive.IDValue("3"), "owner_id": pipedrive.IDValue("7"),
		},
		CustomFields: map[string]json.RawMessage{testSourceFieldKey: pipedrive.StringValue("webinar")},
	}, createCalls[0].Input)
	require.Equal(t, pipedrive.CreateObjectBranchCreated, createCalls[0].Branch)
	getCalls := mock.GetObject().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, pipedrive.GetObjectInput{ObjectType: pipedrive.ObjectTypeDeals, ObjectID: "301"}, getCalls[0].Input)
	require.Empty(t, mock.UpdateObject().Calls())
}
