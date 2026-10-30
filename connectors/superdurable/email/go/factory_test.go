// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type completeTarget[IN any] struct {
	dex.StepDefaultsNoWaitFor[IN]
}

func (completeTarget[IN]) Execute(dex.Context, IN) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

// GetStepType names the target: Dex gives a generic Step type no default name.
func (completeTarget[IN]) GetStepType() string { return "Complete" }

func newUnitTestConnection(t *testing.T) email.Connection {
	t.Helper()
	client, err := email.New(validConfig(), sdkgo.StaticCredentialProvider[email.Credentials]{})
	require.NoError(t, err)
	connection, err := email.NewConnection(client, emailConnection)
	require.NoError(t, err)
	return connection
}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newUnitTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "email", GroupLabel: "Email", Explanation: "Call the mail server."}
	require.NotPanics(t, func() {
		email.NewSearchMessagesStep(email.SearchMessagesStepConfig[string]{
			StepType: "Search", Annotations: annotations, Connection: connection, ConnectionName: emailConnection.Name,
			MapToOperationInput: func(from string) email.SearchMessagesInput { return email.SearchMessagesInput{From: from} },
			Searched:            sdkgo.GoTo(completeTarget[email.SearchMessagesResult]{}),
		})
		email.NewGetMessageStep(email.GetMessageStepConfig[email.MessageReference]{
			StepType: "Read", Annotations: annotations, Connection: connection, ConnectionName: emailConnection.Name,
			MapToOperationInput: func(reference email.MessageReference) email.GetMessageInput {
				return email.GetMessageInput{Message: reference}
			},
			Found: sdkgo.GoTo(completeTarget[email.GetMessageResult]{}),
		})
		email.NewSendMessageStep(email.SendMessageStepConfig[string]{
			StepType: "Send", Annotations: annotations, Connection: connection, ConnectionName: emailConnection.Name,
			MapToOperationInput: func(string) email.SendMessageInput { return validSendMessageInput() },
			Sent:                sdkgo.GoTo(completeTarget[email.SendMessageResult]{}),
		})
		email.NewReplyToMessageStep(email.ReplyToMessageStepConfig[email.MessageReference]{
			StepType: "Reply", Annotations: annotations, Connection: connection, ConnectionName: emailConnection.Name,
			MapToOperationInput: func(reference email.MessageReference) email.ReplyToMessageInput {
				return email.ReplyToMessageInput{Message: reference, Text: "Thanks."}
			},
			Sent:      sdkgo.GoTo(completeTarget[email.ReplyToMessageResult]{}),
			Uncertain: sdkgo.GoTo(completeTarget[email.ReplyToMessageResult]{}),
		})
		email.NewMoveMessageStep(email.MoveMessageStepConfig[email.MessageReference]{
			StepType: "Archive", Annotations: annotations, Connection: connection, ConnectionName: emailConnection.Name,
			MapToOperationInput: func(reference email.MessageReference) email.MoveMessageInput {
				return email.MoveMessageInput{Message: reference, DestinationMailbox: "Archive"}
			},
			Moved: sdkgo.GoTo(completeTarget[email.MoveMessageResult]{}),
		})
		email.NewSetFlagsStep(email.SetFlagsStepConfig[email.MessageReference]{
			StepType: "MarkRead", Annotations: annotations, Connection: connection, ConnectionName: emailConnection.Name,
			MapToOperationInput: func(reference email.MessageReference) email.SetFlagsInput {
				isSeen := true
				return email.SetFlagsInput{Message: reference, IsSeen: &isSeen}
			},
			Updated: sdkgo.GoTo(completeTarget[email.SetFlagsResult]{}),
		})
	})
	require.Panics(t, func() {
		email.NewSendMessageStep(email.SendMessageStepConfig[string]{
			StepType: "Send", Annotations: annotations, Connection: connection, ConnectionName: emailConnection.Name,
			MapToOperationInput: func(string) email.SendMessageInput { return validSendMessageInput() },
			Uncertain:           sdkgo.GoTo(completeTarget[email.SendMessageResult]{}),
		})
	}, "sent is the required branch")
	require.Panics(t, func() {
		email.NewSearchMessagesStep(email.SearchMessagesStepConfig[string]{
			StepType: "Search", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(string) email.SearchMessagesInput { return email.SearchMessagesInput{} },
			Searched:            sdkgo.GoTo(completeTarget[email.SearchMessagesResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndDurability(t *testing.T) {
	require.Equal(t, map[sdkgo.BranchID]bool{"searched": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(email.SearchMessagesDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(email.GetMessageDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"sent": false, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(email.SendMessageDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"sent": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "uncertain": true, "defect": true},
		branchOptionality(email.ReplyToMessageDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"moved": false, "notFound": true, "providerRejected": true, "defect": true},
		branchOptionality(email.MoveMessageDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"updated": false, "notFound": true, "providerRejected": true, "defect": true},
		branchOptionality(email.SetFlagsDefinition.Branches))

	for _, defaults := range []sdkgo.StepDefaults{
		email.SearchMessagesDefinition.StepDefaults, email.GetMessageDefinition.StepDefaults,
		email.MoveMessageDefinition.StepDefaults, email.SetFlagsDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "IMAP reads and repeatable writes keep async durability")
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout)
	}
	for _, defaults := range []sdkgo.StepDefaults{email.SendMessageDefinition.StepDefaults, email.ReplyToMessageDefinition.StepDefaults} {
		require.Equal(t, dex.StepDurabilitySync, defaults.ExecuteDurability, "SMTP has no idempotency, so Dex must never dispatch a second attempt")
		require.Equal(t, 150*time.Second, defaults.ExecuteMethodTimeout)
		require.Equal(t, defaults.ExecuteMethodTimeout, defaults.HeartbeatTimeout, "a silent SMTP server must not fail a healthy attempt")
		require.Equal(t, 10*time.Minute, defaults.ExecuteRetry.TotalDuration)
	}
}

func branchOptionality(branches []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	optionality := make(map[sdkgo.BranchID]bool, len(branches))
	for _, branch := range branches {
		optionality[branch.ID] = branch.Optional
	}
	return optionality
}
