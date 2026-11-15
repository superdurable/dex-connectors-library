// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package itemchanged

import (
	"testing"

	"github.com/stretchr/testify/require"
	monday "github.com/superdurable/dex-connectors-library/connectors/monday/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(kind monday.ItemChangeKind) sdkgo.TriggerEvent[monday.ItemChangedEvent] {
	return sdkgo.TriggerEvent[monday.ItemChangedEvent]{
		ID: "monday-item-0123456789abcdef0123456789abcdef",
		Payload: monday.ItemChangedEvent{
			BoardID: "1234567890", ChangeKind: kind,
			Item: monday.Item{ID: "5550003", Name: "New request", BoardID: "1234567890"},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerChange(t *testing.T) {
	event := sampleTriggerEvent(monday.ItemChangeUpdated)
	require.Equal(t, "monday-item-change-monday-item-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryItemChange(event))
	require.Equal(t, monday.GetItemInput{ItemID: "5550003"}, MapToGetItemInput(input))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	for _, kind := range []monday.ItemChangeKind{monday.ItemChangeCreated, monday.ItemChangeUpdated} {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(kind))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(monday.ItemChangeCreated))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingItem := MapToFlowInput(sampleTriggerEvent(monday.ItemChangeCreated))
	missingItem.Change.Item.ID = ""
	require.ErrorContains(t, validateInput(missingItem), "change.item.id")
	unknownKind := MapToFlowInput(sampleTriggerEvent("archived"))
	require.ErrorContains(t, validateInput(unknownKind), "changeKind")
}

func TestTriggerBindingShowsOneGuidedBoardIDUnit(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, monday.ItemChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, ItemChangedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 1, "the board ID is the only visible Trigger field")
	unit := binding.ConfigurationUI.Units[0]
	require.Equal(t, monday.UIUnitBoardIDInput, unit.UnitID)
	require.Equal(t, "Watched board", unit.Label)
	require.True(t, unit.Required, "the Trigger has no default board")
	for _, guidance := range []string{"https://monday.com/", "digits after /boards/", "1234567890", "whole address", "read the board", "Blank", "baseline", "Restart the Worker"} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: monday.UIBoardIDInputPortBoardID, JSONPointer: "/boardId"}}, unit.Bindings)
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(monday.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordItemChangeStepType, dex.GetFinalStepType[Input](recordItemChange{}))
	require.Equal(t, completeItemChangeStepType, dex.GetFinalStepType[monday.GetItemResult](completeItemChange{}))
	wait, err := recordItemChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := monday.New(monday.Config{}, sdkgo.StaticCredentialProvider[monday.Credentials]{})
	require.NoError(t, err)
	connection, err := monday.NewConnection(client, sdkgo.ConnectionRef{Provider: "monday", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
