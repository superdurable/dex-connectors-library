// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadqualification

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testSettings = Settings{
	LeadOwner:          LeadOwnerConfiguration{OwnerID: "77"},
	QualifiedDealStage: QualifiedDealStageConfiguration{PipelineID: "default", StageID: "qualifiedtobuy"},
}

func TestMapToUpsertLeadContactInputUpsertsByEmailWithOnlySuppliedFields(t *testing.T) {
	flow := mustNewFlow(t, hubspot.Connection{}, &testSettings)
	input := flow.MapToUpsertLeadContactInput(LeadQualification{Lead: Input{Email: "ada@example.com", FirstName: "Ada"}})
	require.Equal(t, hubspot.UpsertObjectInput{
		ObjectType: hubspot.ObjectTypeContacts, IDProperty: "email", IDValue: "ada@example.com",
		Properties: map[string]string{"firstname": "Ada", "hubspot_owner_id": "77"},
	}, input)

	withoutOwner := mustNewFlow(t, hubspot.Connection{}, &Settings{QualifiedDealStage: testSettings.QualifiedDealStage})
	require.NotContains(t, withoutOwner.MapToUpsertLeadContactInput(LeadQualification{Lead: Input{Email: "ada@example.com"}}).Properties,
		"hubspot_owner_id", "a blank owner pick leaves the contact's owner unchanged")
}

func TestMapToFindOpenDealInputSearchesTheContactsOpenDealInThePipeline(t *testing.T) {
	flow := mustNewFlow(t, hubspot.Connection{}, &testSettings)
	input := flow.MapToFindOpenDealInput(LeadQualification{ContactID: "501"})
	require.Equal(t, hubspot.ObjectTypeDeals, input.ObjectType)
	require.Equal(t, []hubspot.SearchFilter{
		{PropertyName: "associations.contact", Operator: hubspot.FilterOperatorEqual, Value: "501"},
		{PropertyName: "pipeline", Operator: hubspot.FilterOperatorEqual, Value: "default"},
		{PropertyName: "hs_is_closed", Operator: hubspot.FilterOperatorEqual, Value: "false"},
	}, input.FilterGroups[0].Filters)
	require.Equal(t, 1, input.Limit)
}

func TestMapToAdvanceAndReadBackUseTheFoundDealAndConfiguredStage(t *testing.T) {
	flow := mustNewFlow(t, hubspot.Connection{}, &testSettings)
	qualification := LeadQualification{DealID: "9001"}
	require.Equal(t, hubspot.UpdateObjectInput{
		ObjectType: hubspot.ObjectTypeDeals, ObjectID: "9001", Properties: map[string]string{"dealstage": "qualifiedtobuy"},
	}, flow.MapToAdvanceOpenDealInput(qualification))
	require.Equal(t, hubspot.GetObjectInput{
		ObjectType: hubspot.ObjectTypeDeals, ObjectID: "9001", Properties: []string{"dealname", "dealstage", "pipeline"},
	}, flow.MapToReadBackDealInput(qualification))
}

func TestNewFlowRequiresTheQualifiedDealPipelineAndStage(t *testing.T) {
	_, err := NewFlow(hubspot.Connection{}, nil)
	require.Error(t, err)
	_, err = NewFlow(hubspot.Connection{}, &Settings{QualifiedDealStage: QualifiedDealStageConfiguration{PipelineID: "default"}})
	require.Error(t, err)
	_, err = NewFlow(hubspot.Connection{}, &Settings{QualifiedDealStage: QualifiedDealStageConfiguration{StageID: "qualifiedtobuy"}})
	require.Error(t, err)
}

func TestValidateLeadTrimsAndRejectsInvalidEmail(t *testing.T) {
	lead, err := validateLead(Input{Email: "  ada@example.com ", FirstName: " Ada "})
	require.NoError(t, err)
	require.Equal(t, Input{Email: "ada@example.com", FirstName: "Ada"}, lead)
	for _, email := range []string{"", "not-an-email", "Ada <ada@example.com>", "a@example.com, b@example.com"} {
		_, err := validateLead(Input{Email: email})
		require.Error(t, err, email)
	}
}

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := mustNewFlow(t, hubspot.Connection{}, &testSettings)
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordLeadStepType, dex.GetFinalStepType[Input](recordLead{}))
	require.Equal(t, recordLeadContactStepType, dex.GetFinalStepType[hubspot.UpsertObjectResult](recordLeadContact{}))
	require.Equal(t, routeOpenDealStepType, dex.GetFinalStepType[hubspot.SearchObjectsResult](routeOpenDeal{}))
	require.Equal(t, recordAdvancedDealStepType, dex.GetFinalStepType[hubspot.UpdateObjectResult](recordAdvancedDeal{}))
	require.Equal(t, completeLeadQualificationStepType, dex.GetFinalStepType[hubspot.GetObjectResult](completeLeadQualification{}))
	require.Equal(t, recordRejectedLeadStepType, dex.GetFinalStepType[hubspot.UpsertObjectResult](recordRejectedLead{}))
	wait, err := recordLead{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestConfigurationReferencesNameTheirOperationSteps(t *testing.T) {
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: hubspot.ConnectorID, ConnectionName: ConnectionName, OperationID: "upsertObject",
		FlowType: FlowType, StepType: upsertLeadContactStepType,
	}, LeadOwnerConfigurationRef())
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: hubspot.ConnectorID, ConnectionName: ConnectionName, OperationID: "updateObject",
		FlowType: FlowType, StepType: advanceOpenDealStepType,
	}, QualifiedDealStageConfigurationRef())
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := hubspot.New(hubspot.Config{}, sdkgo.StaticCredentialProvider[hubspot.Credentials]{})
	require.NoError(t, err)
	connection, err := hubspot.NewConnection(client, sdkgo.ConnectionRef{Provider: "hubspot", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{mustNewFlow(t, connection, &testSettings)})
	require.NoError(t, err)

	otherConnection, err := hubspot.NewConnection(client, sdkgo.ConnectionRef{Provider: "hubspot", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{mustNewFlow(t, otherConnection, &testSettings)}) })
}

func mustNewFlow(t *testing.T, connection hubspot.Connection, settings *Settings) *Flow {
	t.Helper()
	flow, err := NewFlow(connection, settings)
	require.NoError(t, err)
	return flow
}
