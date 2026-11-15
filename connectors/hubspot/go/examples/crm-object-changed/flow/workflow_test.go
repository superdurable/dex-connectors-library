// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crmobjectchanged

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(kind hubspot.CRMObjectChangeKind) sdkgo.TriggerEvent[hubspot.CRMObjectChangedEvent] {
	return sdkgo.TriggerEvent[hubspot.CRMObjectChangedEvent]{
		ID: "hubspot-0123456789abcdef0123456789abcdef",
		Payload: hubspot.CRMObjectChangedEvent{
			ObjectType: hubspot.ObjectTypeDeals, ChangeKind: kind,
			Object: hubspot.CRMObject{ObjectType: hubspot.ObjectTypeDeals, ID: "9001", Properties: map[string]string{"dealname": "Renewal"}},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerRecordVersion(t *testing.T) {
	event := sampleTriggerEvent(hubspot.CRMObjectChangeUpdated)
	require.Equal(t, "hubspot-crm-object-change-hubspot-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryCRMObjectChange(event))
	require.Equal(t, hubspot.GetObjectInput{ObjectType: hubspot.ObjectTypeDeals, ObjectID: "9001", Properties: DeliveredProperties}, MapToGetObjectInput(input))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	for _, kind := range []hubspot.CRMObjectChangeKind{hubspot.CRMObjectChangeCreated, hubspot.CRMObjectChangeUpdated} {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(kind))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(hubspot.CRMObjectChangeCreated))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingObject := MapToFlowInput(sampleTriggerEvent(hubspot.CRMObjectChangeCreated))
	missingObject.Change.Object.ID = ""
	require.ErrorContains(t, validateInput(missingObject), "change.object.id")
	unknownKind := MapToFlowInput(sampleTriggerEvent("deleted"))
	require.ErrorContains(t, validateInput(unknownKind), "changeKind")
}

func TestTriggerBindingShowsOneGuidedRequiredObjectTypePicker(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, hubspot.CRMObjectChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, CRMObjectChangedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 1, "the picker is the only visible Trigger field")
	unit := binding.ConfigurationUI.Units[0]
	require.Equal(t, hubspot.UIUnitObjectTypePicker, unit.UnitID)
	require.Equal(t, "Watched object type", unit.Label)
	require.True(t, unit.Required, "the Trigger cannot poll without an object type")
	for _, guidance := range []string{"contacts, companies, or deals", "object name", "crm.objects.<type>.read", "cannot be blank", "baseline", "Restart the Worker"} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: hubspot.UIObjectTypePickerPortObjectType, JSONPointer: "/objectType"}}, unit.Bindings)
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(hubspot.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordCRMObjectChangeStepType, dex.GetFinalStepType[Input](recordCRMObjectChange{}))
	require.Equal(t, completeCRMObjectChangeStepType, dex.GetFinalStepType[hubspot.GetObjectResult](completeCRMObjectChange{}))
	wait, err := recordCRMObjectChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := hubspot.New(hubspot.Config{}, sdkgo.StaticCredentialProvider[hubspot.Credentials]{})
	require.NoError(t, err)
	connection, err := hubspot.NewConnection(client, sdkgo.ConnectionRef{Provider: "hubspot", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
