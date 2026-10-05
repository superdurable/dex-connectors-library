// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dealintake

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// testSourceFieldKey is built at run time, so no token-shaped 40-character literal is checked in.
var testSourceFieldKey = strings.Repeat("ab", 20)

var testSettings = Settings{
	LeadOwner: LeadOwnerConfiguration{OwnerID: "7"},
	Deal:      DealConfiguration{PipelineID: "1", StageID: "3", SourceObjectType: "deals", SourceFieldKey: testSourceFieldKey},
}

func TestMapToUpsertLeadPersonInputUpsertsByEmailWithTheOrganizationAndOwner(t *testing.T) {
	flow := mustNewFlow(t, pipedrive.Connection{}, &testSettings)
	input := flow.MapToUpsertLeadPersonInput(DealIntake{Lead: Input{Email: "jane@acme.example.com", Name: "Jane Smith"}, OrganizationID: "88"})
	require.Equal(t, pipedrive.UpsertObjectInput{
		ObjectType: pipedrive.ObjectTypePersons, IDProperty: "email", IDValue: "jane@acme.example.com",
		Fields: map[string]json.RawMessage{"name": json.RawMessage(`"Jane Smith"`), "org_id": json.RawMessage("88"), "owner_id": json.RawMessage("7")},
	}, input)

	withoutPicks := mustNewFlow(t, pipedrive.Connection{}, &Settings{Deal: DealConfiguration{PipelineID: "1", StageID: "3"}})
	fields := withoutPicks.MapToUpsertLeadPersonInput(DealIntake{Lead: Input{Email: "jane@acme.example.com", Name: "Jane"}}).Fields
	require.NotContains(t, fields, "owner_id", "a blank owner pick sets no owner")
	require.NotContains(t, fields, "org_id", "an organization that did not match exactly once is not linked")
}

func TestMapToFindOpenDealInputListsThePersonsNewestOpenDealInThePipeline(t *testing.T) {
	flow := mustNewFlow(t, pipedrive.Connection{}, &testSettings)
	require.Equal(t, pipedrive.ListObjectsInput{
		ObjectType: pipedrive.ObjectTypeDeals, PersonID: "901", PipelineID: "1", Statuses: []pipedrive.DealStatus{pipedrive.DealStatusOpen},
		SortBy: pipedrive.ListSortFieldUpdateTime, SortDirection: pipedrive.SortDirectionDescending, Limit: 1,
	}, flow.MapToFindOpenDealInput(DealIntake{PersonID: "901"}))
}

func TestMapToCreateDealInputWritesTheSourceOnlyWhenAFieldAndSourceExist(t *testing.T) {
	flow := mustNewFlow(t, pipedrive.Connection{}, &testSettings)
	intake := DealIntake{Lead: Input{DealTitle: "Acme renewal", Source: "webinar"}, PersonID: "901", OrganizationID: "88"}
	require.Equal(t, pipedrive.CreateObjectInput{
		ObjectType: pipedrive.ObjectTypeDeals,
		Fields: map[string]json.RawMessage{
			"title": json.RawMessage(`"Acme renewal"`), "person_id": json.RawMessage("901"), "org_id": json.RawMessage("88"),
			"pipeline_id": json.RawMessage("1"), "stage_id": json.RawMessage("3"), "owner_id": json.RawMessage("7"),
		},
		CustomFields: map[string]json.RawMessage{testSourceFieldKey: json.RawMessage(`"webinar"`)},
	}, flow.MapToCreateDealInput(intake))
	intake.Lead.Source = ""
	require.Nil(t, flow.MapToCreateDealInput(intake).CustomFields, "a blank source writes no custom field")
}

func TestMapToAdvanceAndReadBackUseTheListedDealAndConfiguredStage(t *testing.T) {
	flow := mustNewFlow(t, pipedrive.Connection{}, &testSettings)
	intake := DealIntake{DealID: "42"}
	require.Equal(t, pipedrive.UpdateObjectInput{
		ObjectType: pipedrive.ObjectTypeDeals, ObjectID: "42",
		Fields: map[string]json.RawMessage{"stage_id": json.RawMessage("3"), "owner_id": json.RawMessage("7")},
	}, flow.MapToAdvanceDealInput(intake))
	require.Equal(t, pipedrive.GetObjectInput{ObjectType: pipedrive.ObjectTypeDeals, ObjectID: "42"}, flow.MapToReadBackDealInput(intake))
	require.Equal(t, pipedrive.SearchObjectsInput{
		ObjectType: pipedrive.ObjectTypeOrganizations, Term: "Acme Corp", Fields: []pipedrive.SearchField{pipedrive.SearchFieldName}, ExactMatch: true, Limit: 10,
	}, flow.MapToFindOrganizationInput(DealIntake{Lead: Input{Organization: "Acme Corp"}}))
}

func TestNewFlowRequiresTheStageAndValidPicks(t *testing.T) {
	for name, settings := range map[string]*Settings{
		"no settings":           nil,
		"no stage":              {Deal: DealConfiguration{PipelineID: "1"}},
		"named stage":           {Deal: DealConfiguration{PipelineID: "1", StageID: "qualified"}},
		"owner email":           {LeadOwner: LeadOwnerConfiguration{OwnerID: "ada@example.com"}, Deal: DealConfiguration{PipelineID: "1", StageID: "3"}},
		"person custom field":   {Deal: DealConfiguration{PipelineID: "1", StageID: "3", SourceObjectType: "persons", SourceFieldKey: testSourceFieldKey}},
		"field label not a key": {Deal: DealConfiguration{PipelineID: "1", StageID: "3", SourceObjectType: "deals", SourceFieldKey: "Lead source"}},
	} {
		_, err := NewFlow(pipedrive.Connection{}, settings)
		require.Error(t, err, name)
	}
}

func TestValidateLeadTrimsAndRejectsInvalidInput(t *testing.T) {
	lead, err := ValidateLead(Input{Email: " jane@acme.example.com ", Name: " Jane ", DealTitle: " Renewal ", Source: " webinar "})
	require.NoError(t, err)
	require.Equal(t, Input{Email: "jane@acme.example.com", Name: "Jane", DealTitle: "Renewal", Source: "webinar"}, lead)
	for name, input := range map[string]Input{
		"display-name address": {Email: "Jane <jane@acme.example.com>", Name: "Jane", DealTitle: "Renewal"},
		"missing name":         {Email: "jane@acme.example.com", DealTitle: "Renewal"},
		"missing deal title":   {Email: "jane@acme.example.com", Name: "Jane"},
	} {
		_, err := ValidateLead(input)
		require.Error(t, err, name)
	}
}

func TestStepIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := mustNewFlow(t, pipedrive.Connection{}, &testSettings)
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordLeadStepType, dex.GetFinalStepType[Input](recordLead{}))
	require.Equal(t, routeOrganizationStepType, dex.GetFinalStepType[pipedrive.SearchObjectsResult](routeOrganization{}))
	require.Equal(t, recordLeadPersonStepType, dex.GetFinalStepType[pipedrive.UpsertObjectResult](recordLeadPerson{}))
	require.Equal(t, recordUnresolvedLeadStepType, dex.GetFinalStepType[pipedrive.UpsertObjectResult](recordUnresolvedLead{}))
	require.Equal(t, routeOpenDealStepType, dex.GetFinalStepType[pipedrive.ListObjectsResult](routeOpenDeal{}))
	require.Equal(t, recordCreatedDealStepType, dex.GetFinalStepType[pipedrive.CreateObjectResult](recordCreatedDeal{}))
	require.Equal(t, recordAdvancedDealStepType, dex.GetFinalStepType[pipedrive.UpdateObjectResult](recordAdvancedDeal{}))
	require.Equal(t, recordUnresolvedDealStepType, dex.GetFinalStepType[pipedrive.CreateObjectResult](recordUnresolvedDeal{}))
	require.Equal(t, completeDealIntakeStepType, dex.GetFinalStepType[pipedrive.GetObjectResult](completeDealIntake{}))
	wait, err := recordLead{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestConfigurationReferencesNameTheirOperationSteps(t *testing.T) {
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: pipedrive.ConnectorID, ConnectionName: ConnectionName, OperationID: "upsertObject",
		FlowType: FlowType, StepType: upsertLeadPersonStepType,
	}, LeadOwnerConfigurationRef())
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: pipedrive.ConnectorID, ConnectionName: ConnectionName, OperationID: "createObject",
		FlowType: FlowType, StepType: createDealStepType,
	}, DealConfigurationRef())
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := pipedrive.New(pipedrive.Config{}, sdkgo.StaticCredentialProvider[pipedrive.Credentials]{})
	require.NoError(t, err)
	connection, err := pipedrive.NewConnection(client, sdkgo.ConnectionRef{Provider: "pipedrive", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{mustNewFlow(t, connection, &testSettings)})
	require.NoError(t, err)

	otherConnection, err := pipedrive.NewConnection(client, sdkgo.ConnectionRef{Provider: "pipedrive", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{mustNewFlow(t, otherConnection, &testSettings)}) })
}

func mustNewFlow(t *testing.T, connection pipedrive.Connection, settings *Settings) *Flow {
	t.Helper()
	flow, err := NewFlow(connection, settings)
	require.NoError(t, err)
	return flow
}
