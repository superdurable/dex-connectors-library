// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cardchanged

import (
	"testing"

	"github.com/stretchr/testify/require"
	trello "github.com/superdurable/dex-connectors-library/connectors/atlassian/trello/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(actionType trello.CardActionType) sdkgo.TriggerEvent[trello.CardChangedEvent] {
	return sdkgo.TriggerEvent[trello.CardChangedEvent]{
		ID: "trello-action-6512f0a1c2d3e4f5a6b74001",
		Payload: trello.CardChangedEvent{
			BoardID: "5b6893f01cb3228998cf629e", ActionID: "6512f0a1c2d3e4f5a6b74001", ActionType: actionType,
			Card: trello.ActionCard{ID: "6512f0a1c2d3e4f5a6b72001", Name: "Replace badge reader"},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerCardAction(t *testing.T) {
	event := sampleTriggerEvent(trello.CardActionUpdated)
	require.Equal(t, "trello-card-change-trello-action-6512f0a1c2d3e4f5a6b74001", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryCardChange(event))
	require.Equal(t, trello.GetCardInput{CardID: "6512f0a1c2d3e4f5a6b72001"}, MapToGetCardInput(input))
	require.Equal(t, ChangeOutcome{BoardID: "5b6893f01cb3228998cf629e", CardID: "6512f0a1c2d3e4f5a6b72001", ActionType: trello.CardActionUpdated}, outcomeOf(input, nil))
}

func TestValidateInputRejectsIncompleteCardActions(t *testing.T) {
	for _, actionType := range knownCardActionTypes {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(actionType))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(trello.CardActionCreated))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingCard := MapToFlowInput(sampleTriggerEvent(trello.CardActionCreated))
	missingCard.Change.Card.ID = ""
	require.ErrorContains(t, validateInput(missingCard), "change.card.id")
	unknownType := MapToFlowInput(sampleTriggerEvent("updateBoard"))
	require.ErrorContains(t, validateInput(unknownType), "change.actionType")
}

func TestTriggerBindingShowsOneGuidedRequiredBoardInput(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, trello.CardChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, CardChangedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 1, "the board is the only visible Trigger field")
	unit := binding.ConfigurationUI.Units[0]
	require.Equal(t, trello.UIUnitBoardInput, unit.UnitID)
	require.Equal(t, "Watched board", unit.Label)
	require.True(t, unit.Required, "a binding without a board stops the Worker at start")
	for _, guidance := range []string{
		"https://trello.com", "https://trello.com/b/<short link>/<board name>", "8-character short link",
		"24-character board ID", "not secret", "blank", "baseline", "Restart the Worker",
	} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: trello.UIBoardInputPortBoardID, JSONPointer: "/boardId"}}, unit.Bindings)
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(trello.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordCardChangeStepType, dex.GetFinalStepType[Input](recordCardChange{}))
	require.Equal(t, completeCardChangeStepType, dex.GetFinalStepType[trello.GetCardResult](completeCardChange{}))
	require.Equal(t, recordCardRemovedSinceStepType, dex.GetFinalStepType[trello.GetCardResult](recordCardRemovedSince{}))
	wait, err := recordCardChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := trello.New(trello.Config{}, sdkgo.StaticCredentialProvider[trello.Credentials]{})
	require.NoError(t, err)
	connection, err := trello.NewConnection(client, sdkgo.ConnectionRef{Provider: "trello", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
