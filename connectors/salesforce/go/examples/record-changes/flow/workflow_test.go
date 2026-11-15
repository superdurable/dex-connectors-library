// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordchanges

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	salesforce "github.com/superdurable/dex-connectors-library/connectors/salesforce/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(kind salesforce.RecordChangeKind) sdkgo.TriggerEvent[salesforce.RecordModifiedEvent] {
	return sdkgo.TriggerEvent[salesforce.RecordModifiedEvent]{
		ID: "sfdc-0123456789abcdef0123456789abcdef",
		Payload: salesforce.RecordModifiedEvent{
			SObjectType: "Opportunity", RecordID: "006RM0000000001AAA", ChangeKind: kind,
			SystemModstamp: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC),
			Record: salesforce.Record{Type: "Opportunity", ID: "006RM0000000001AAA", Fields: map[string]json.RawMessage{
				"Id": json.RawMessage(`"006RM0000000001AAA"`), "StageName": json.RawMessage(`"Closed Won"`), "Name": json.RawMessage(`"Renewal"`),
			}},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerChange(t *testing.T) {
	event := sampleTriggerEvent(salesforce.RecordChangeUpdated)
	require.Equal(t, "salesforce-record-change-sfdc-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryRecordChange(event))
	require.Equal(t, salesforce.GetRecordInput{
		SObjectType: "Opportunity", RecordID: "006RM0000000001AAA", Fields: []string{"Id", "Name", "StageName"},
	}, MapToGetRecordInput(input), "every delivered field is read back in a stable order")
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	for _, kind := range []salesforce.RecordChangeKind{salesforce.RecordChangeCreated, salesforce.RecordChangeUpdated} {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(kind))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(salesforce.RecordChangeCreated))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingRecord := MapToFlowInput(sampleTriggerEvent(salesforce.RecordChangeCreated))
	missingRecord.Change.RecordID = ""
	require.ErrorContains(t, validateInput(missingRecord), "change.recordId")
	unknownKind := MapToFlowInput(sampleTriggerEvent("deleted"))
	require.ErrorContains(t, validateInput(unknownKind), "changeKind")
}

func TestTriggerBindingShowsTheGuidedObjectAndFieldUnits(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, salesforce.RecordModifiedPolledTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, RecordModifiedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 2, "the object and the field list are the only visible Trigger fields")

	object := binding.ConfigurationUI.Units[0]
	require.Equal(t, salesforce.UIUnitSObjectPicker, object.UnitID)
	require.Equal(t, "Watched object", object.Label)
	require.True(t, object.Required, "a binding without an object cannot poll")
	for _, guidance := range []string{"Setup > Object Manager", "API Name", "required", "baseline", "Restart the Worker"} {
		require.Contains(t, object.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: salesforce.UISObjectPickerPortSObjectType, JSONPointer: "/sObjectType"}}, object.Bindings)

	fields := binding.ConfigurationUI.Units[1]
	require.Equal(t, salesforce.UIUnitFieldNamesInput, fields.UnitID)
	require.Equal(t, "Delivered fields", fields.Label)
	require.False(t, fields.Required)
	for _, guidance := range []string{"Fields & Relationships", "Field Name column", "Blank delivers only Id", "Restart the Worker"} {
		require.Contains(t, fields.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: salesforce.UIFieldNamesInputPortFieldNames, JSONPointer: "/fieldNames"}}, fields.Bindings)
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(salesforce.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordRecordChangeStepType, dex.GetFinalStepType[Input](recordRecordChange{}))
	require.Equal(t, completeRecordChangeStepType, dex.GetFinalStepType[salesforce.GetRecordResult](completeRecordChange{}))
	require.Equal(t, completeDeletedRecordType, dex.GetFinalStepType[salesforce.GetRecordResult](completeDeletedRecord{}))
	wait, err := recordRecordChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := salesforce.New(salesforce.Config{}, sdkgo.StaticCredentialProvider[salesforce.Credentials]{})
	require.NoError(t, err)
	connection, err := salesforce.NewConnection(client, sdkgo.ConnectionRef{Provider: "salesforce", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
