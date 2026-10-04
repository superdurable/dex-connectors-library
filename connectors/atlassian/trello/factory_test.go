// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type createdCardTarget struct {
	dex.StepDefaultsNoWaitFor[trello.CreateCardResult]
}

func (createdCardTarget) Execute(dex.Context, trello.CreateCardResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestCreateCardFactoryRequiresOnlyTheHappyPath(t *testing.T) {
	connection, err := trello.NewConnection(newTrelloClient(t, "http://127.0.0.1:1"), trelloConnection)
	require.NoError(t, err)
	annotations := sdkgo.StepAnnotations{GroupID: "trello", GroupLabel: "Trello", Explanation: "Create a card."}
	mapToInput := func(string) trello.CreateCardInput { return trello.CreateCardInput{} }
	require.NotPanics(t, func() {
		trello.NewCreateCardStep(trello.CreateCardStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: trelloConnection.Name,
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdCardTarget{}),
		})
	})
	require.Panics(t, func() {
		trello.NewCreateCardStep(trello.CreateCardStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: trelloConnection.Name,
			MapToOperationInput: mapToInput, Uncertain: sdkgo.GoTo(createdCardTarget{}),
		})
	})
	require.Panics(t, func() {
		trello.NewCreateCardStep(trello.CreateCardStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdCardTarget{}),
		})
	})
}

// TestOnlyUnkeyedWritesAreUncertainAndSync records the duplicate-dispatch decision per operation.
func TestOnlyUnkeyedWritesAreUncertainAndSync(t *testing.T) {
	for _, test := range []struct {
		operation    string
		branches     []sdkgo.BranchDefinition
		defaults     sdkgo.StepDefaults
		hasUncertain bool
	}{
		{"listCards", trello.ListCardsDefinition.Branches, trello.ListCardsDefinition.StepDefaults, false},
		{"getCard", trello.GetCardDefinition.Branches, trello.GetCardDefinition.StepDefaults, false},
		{"createCard", trello.CreateCardDefinition.Branches, trello.CreateCardDefinition.StepDefaults, true},
		{"updateCard", trello.UpdateCardDefinition.Branches, trello.UpdateCardDefinition.StepDefaults, false},
		{"addComment", trello.AddCommentDefinition.Branches, trello.AddCommentDefinition.StepDefaults, true},
	} {
		required, hasUncertain := 0, false
		for _, branch := range test.branches {
			if branch.ID == sdkgo.UncertainBranchID {
				hasUncertain = true
			}
			if !branch.Optional {
				required++
			}
		}
		require.Equal(t, 1, required, test.operation)
		require.Equal(t, test.hasUncertain, hasUncertain, test.operation)
		expectedDurability := dex.StepDurabilityAsync
		if test.hasUncertain {
			expectedDurability = dex.StepDurabilitySync
		}
		require.Equal(t, expectedDurability, test.defaults.ExecuteDurability, "%s: an async fallback attempt would resend an unkeyed write", test.operation)
	}
}

func TestTrelloConnectionCannotBeSerialized(t *testing.T) {
	connection, err := trello.NewConnection(newTrelloClient(t, "http://127.0.0.1:1"), trelloConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.Equal(t, "trello.Connection{[REDACTED]}", fmt.Sprint(connection))
}
