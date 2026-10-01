// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordsync

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func newTestFlow(configuration SyncConfiguration) *Flow {
	return NewFlow(salesforce.Connection{}, sdkgo.ConnectorLoadedConfiguration[SyncConfiguration]{Reference: SyncConfigurationRef(), Value: configuration})
}

func TestNewFlowDefaultsToContactsMatchedByEmail(t *testing.T) {
	flow := newTestFlow(SyncConfiguration{ExternalIDField: "ERP_Id__c"})
	require.Equal(t, SyncConfiguration{SObjectType: "Contact", MatchField: "Email", ExternalIDField: "ERP_Id__c"}, flow.configuration)
	custom := newTestFlow(SyncConfiguration{SObjectType: "Lead", MatchField: "Company", ExternalIDField: "ERP_Id__c"})
	require.Equal(t, "Lead", custom.configuration.SObjectType)
	require.Equal(t, "Company", custom.configuration.MatchField)
}

func TestMapToQueryRecordsInputBindsNamesAndTheMatchValue(t *testing.T) {
	input := newTestFlow(SyncConfiguration{ExternalIDField: "ERP_Id__c"}).MapToQueryRecordsInput(Input{MatchValue: "o'brien@example.com"})
	require.Equal(t, matchQuery, input.SOQL)
	require.Equal(t, map[string]salesforce.SOQLValue{
		"externalIdField": salesforce.SOQLIdentifier("ERP_Id__c"), "sObjectType": salesforce.SOQLIdentifier("Contact"),
		"matchField": salesforce.SOQLIdentifier("Email"), "matchValue": salesforce.SOQLString("o'brien@example.com"),
	}, input.Bindings)
}

func TestWriteMappersStampOrKeyByTheExternalID(t *testing.T) {
	flow := newTestFlow(SyncConfiguration{ExternalIDField: "ERP_Id__c"})
	request := WriteRequest{RecordID: "003RM000001AbcdYAC", ExternalID: "ERP-88213", Fields: map[string]string{"Title": `CTO "Platform"`}}
	require.Equal(t, salesforce.UpdateRecordInput{
		SObjectType: "Contact", RecordID: "003RM000001AbcdYAC",
		Fields: map[string]json.RawMessage{"Title": json.RawMessage(`"CTO \"Platform\""`), "ERP_Id__c": json.RawMessage(`"ERP-88213"`)},
	}, flow.MapToUpdateRecordInput(request))
	require.Len(t, request.Fields, 1, "the update mapper must not change the durable request")
	require.Equal(t, salesforce.UpsertRecordByExternalIDInput{
		SObjectType: "Contact", ExternalIDField: "ERP_Id__c", ExternalIDValue: "ERP-88213",
		Fields: map[string]json.RawMessage{"Title": json.RawMessage(`"CTO \"Platform\""`)},
	}, flow.MapToUpsertRecordByExternalIDInput(request))
	readBack := flow.configuration.readBackRequest("003RM000001AbcdYAC", Input{Fields: map[string]string{"Title": "CTO", "LastName": "Raman"}})
	require.Equal(t, ReadBackRequest{RecordID: "003RM000001AbcdYAC", Fields: []string{"ERP_Id__c", "LastName", "Title"}}, readBack)
	require.Equal(t, salesforce.GetRecordInput{SObjectType: "Contact", RecordID: "003RM000001AbcdYAC", Fields: []string{"ERP_Id__c", "LastName", "Title"}},
		flow.MapToGetRecordInput(readBack))
}

func TestReservedFieldsAreTheIDAndExternalIDFields(t *testing.T) {
	require.True(t, hasReservedField(map[string]string{"erp_id__c": "x"}, "ERP_Id__c"))
	require.True(t, hasReservedField(map[string]string{"ID": "x"}, "ERP_Id__c"))
	require.False(t, hasReservedField(map[string]string{"LastName": "x"}, "ERP_Id__c"))
}

func TestSyncConfigurationRefMatchesTheQueryStep(t *testing.T) {
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: salesforce.ConnectorID, ConnectionName: ConnectionName, OperationID: "queryRecords",
		FlowType: FlowType, StepType: findMatchesStepType,
	}, SyncConfigurationRef())
}

func TestStepIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(newTestFlow(SyncConfiguration{})))
	require.Equal(t, recordRequestStepType, dex.GetFinalStepType[Input](recordSyncRequest{}))
	require.Equal(t, reviewMatchesStepType, dex.GetFinalStepType[salesforce.QueryRecordsResult](reviewMatchingRecords{}))
	require.Equal(t, prepareLinkedReadBackStepType, dex.GetFinalStepType[salesforce.UpdateRecordResult](prepareLinkedReadBack{}))
	require.Equal(t, prepareCreatedReadBackStepType, dex.GetFinalStepType[salesforce.UpsertRecordByExternalIDResult](prepareCreatedReadBack{}))
	require.Equal(t, reportLinkRejectedStepType, dex.GetFinalStepType[salesforce.UpdateRecordResult](reportLinkRejected{}))
	require.Equal(t, reportCreateRejectedStepType, dex.GetFinalStepType[salesforce.UpsertRecordByExternalIDResult](reportCreateRejected{}))
	require.Equal(t, completeStepType, dex.GetFinalStepType[salesforce.GetRecordResult](completeRecordSync{}))
	wait, err := recordSyncRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := salesforce.New(salesforce.Config{}, sdkgo.StaticCredentialProvider[salesforce.Credentials]{})
	require.NoError(t, err)
	connection, err := salesforce.NewConnection(client, sdkgo.ConnectionRef{Provider: "salesforce", Name: ConnectionName})
	require.NoError(t, err)
	unset := sdkgo.ConnectorLoadedConfiguration[SyncConfiguration]{}
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, unset)})
	require.NoError(t, err)

	otherConnection, err := salesforce.NewConnection(client, sdkgo.ConnectionRef{Provider: "salesforce", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection, unset)}) })
}
