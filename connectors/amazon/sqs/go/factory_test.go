// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type receivedMessagesTarget struct {
	dex.StepDefaultsNoWaitFor[sqs.ReceiveMessagesResult]
}

func (receivedMessagesTarget) Execute(dex.Context, sqs.ReceiveMessagesResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type deletedMessageTarget struct {
	dex.StepDefaultsNoWaitFor[sqs.DeleteMessageResult]
}

func (deletedMessageTarget) Execute(dex.Context, sqs.DeleteMessageResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestFactoriesRequireOnlyTheHappyPathAndTheirOwnConnection(t *testing.T) {
	client, err := sqs.New(sqs.Config{}, staticCredentials(""))
	require.NoError(t, err)
	connection, err := sqs.NewConnection(client, testConnection)
	require.NoError(t, err)
	annotations := sdkgo.StepAnnotations{GroupID: "sqs", GroupLabel: "Amazon SQS", Explanation: "Receive messages."}
	mapToReceive := func(string) sqs.ReceiveMessagesInput { return sqs.ReceiveMessagesInput{} }
	require.NotPanics(t, func() {
		sqs.NewReceiveMessagesStep(sqs.ReceiveMessagesStepConfig[string]{
			StepType: "Receive", Annotations: annotations, Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: mapToReceive, Received: sdkgo.GoTo(receivedMessagesTarget{}),
		})
	})
	require.Panics(t, func() {
		sqs.NewReceiveMessagesStep(sqs.ReceiveMessagesStepConfig[string]{
			StepType: "Receive", Annotations: annotations, Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: mapToReceive, Empty: sdkgo.GoTo(receivedMessagesTarget{}),
		})
	}, "the received branch is required")
	require.Panics(t, func() {
		sqs.NewReceiveMessagesStep(sqs.ReceiveMessagesStepConfig[string]{
			StepType: "Receive", Annotations: annotations, Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: mapToReceive, Received: sdkgo.GoTo(receivedMessagesTarget{}),
		})
	}, "the static connection name must match the runtime connection")
	require.NotPanics(t, func() {
		sqs.NewDeleteMessageStep(sqs.DeleteMessageStepConfig[string]{
			StepType: "Acknowledge", Annotations: annotations, Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: func(string) sqs.DeleteMessageInput { return sqs.DeleteMessageInput{} }, Deleted: sdkgo.GoTo(deletedMessageTarget{}),
		})
	})
}

// TestOnlyTheLongPollIsSyncAndNoOperationDeclaresUncertainty: every operation converges when repeated.
func TestOnlyTheLongPollIsSyncAndNoOperationDeclaresUncertainty(t *testing.T) {
	for _, test := range []struct {
		operation  string
		branches   []sdkgo.BranchDefinition
		defaults   sdkgo.StepDefaults
		durability dex.StepDurability
	}{
		{"receiveMessages", sqs.ReceiveMessagesDefinition.Branches, sqs.ReceiveMessagesDefinition.StepDefaults, dex.StepDurabilitySync},
		{"deleteMessage", sqs.DeleteMessageDefinition.Branches, sqs.DeleteMessageDefinition.StepDefaults, dex.StepDurabilityAsync},
		{"changeMessageVisibility", sqs.ChangeMessageVisibilityDefinition.Branches, sqs.ChangeMessageVisibilityDefinition.StepDefaults, dex.StepDurabilityAsync},
	} {
		required := 0
		for _, branch := range test.branches {
			require.NotEqual(t, sdkgo.UncertainBranchID, branch.ID, test.operation)
			if !branch.Optional {
				required++
			}
		}
		require.Equal(t, 1, required, test.operation)
		require.Equal(t, test.durability, test.defaults.ExecuteDurability, test.operation)
		require.Greater(t, test.defaults.ExecuteMethodTimeout.Seconds(), 25.0, "%s: the 25-second operation deadline fits the Execute timeout", test.operation)
	}
}
