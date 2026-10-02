// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/internal/graphtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestMoveMessageKeepsTheImmutableIDAndSkipsARepeatedMove(t *testing.T) {
	fake := newGraphFake(t)
	id := seedCustomerMessage(fake, "Refund", searchBase)
	client, _ := delegatedClient(t, fake)
	result, err := sdkgo.RunMutation(newOutlookDexContext("move"), client.MoveMessage(), outlookConnection, outlookmail.MoveMessageInput{MessageID: id, DestinationFolder: "Archive"})
	require.NoError(t, err)
	require.Equal(t, outlookmail.MoveMessageBranchMoved, result.Branch, result.Failure)
	require.Equal(t, outlookmail.MovedMessage{MessageID: id, DestinationFolderID: fake.FolderID("archive"), DestinationFolderName: "Archive"}, result.Value)
	require.Equal(t, "/v1.0/me/mailFolders/archive", fake.Requests(graphtest.EndpointGetMailFolder)[0].Path, "a well-known name is sent in lowercase")
	var body map[string]string
	require.NoError(t, json.Unmarshal(fake.Requests(graphtest.EndpointMoveMessage)[0].Body, &body))
	require.Equal(t, map[string]string{"destinationId": fake.FolderID("archive")}, body)

	repeated, err := sdkgo.RunMutation(newOutlookDexContext("move-again"), client.MoveMessage(), outlookConnection, outlookmail.MoveMessageInput{MessageID: id, DestinationFolder: "archive"})
	require.NoError(t, err)
	require.Equal(t, outlookmail.MoveMessageBranchMoved, repeated.Branch)
	require.True(t, repeated.Value.WasAlreadyMoved)
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointMoveMessage), "a message already in the folder is not moved again")
	require.Len(t, fake.MessagesInFolder("archive"), 1)
}

func TestMoveMessageLostMoveAnswerIsRetriedAndFindsTheMessageMoved(t *testing.T) {
	fake := newGraphFake(t)
	id := seedCustomerMessage(fake, "Refund", searchBase)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointMoveMessage, ShouldApplyFirst: true, Status: http.StatusInternalServerError})
	client, _ := delegatedClient(t, fake)
	first := newOutlookDexContext("lost-move")
	input := outlookmail.MoveMessageInput{MessageID: id, DestinationFolder: "archive"}
	_, err := sdkgo.RunMutation(first, client.MoveMessage(), outlookConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.MoveMessage(), outlookConnection, input)
	require.NoError(t, err)
	require.Equal(t, outlookmail.MoveMessageBranchMoved, result.Branch)
	require.True(t, result.Value.WasAlreadyMoved)
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointMoveMessage))
}

func TestMoveMessageMissingDestinationOrMessage(t *testing.T) {
	fake := newGraphFake(t)
	id := seedCustomerMessage(fake, "Refund", searchBase)
	client, _ := delegatedClient(t, fake)
	result, err := sdkgo.RunMutation(newOutlookDexContext("no-folder"), client.MoveMessage(), outlookConnection, outlookmail.MoveMessageInput{MessageID: id, DestinationFolder: "AAMkFolder-404_gone="})
	require.NoError(t, err)
	require.Equal(t, outlookmail.MoveMessageBranchProviderRejected, result.Branch)
	require.Equal(t, "the destination folder does not exist in the mailbox; the connector never creates folders", result.Failure.Message)
	result, err = sdkgo.RunMutation(newOutlookDexContext("no-message"), client.MoveMessage(), outlookConnection, outlookmail.MoveMessageInput{MessageID: "AAMkMessage-404_gone=", DestinationFolder: "archive"})
	require.NoError(t, err)
	require.Equal(t, outlookmail.MoveMessageBranchNotFound, result.Branch)
	result, err = sdkgo.RunMutation(newOutlookDexContext("blank"), client.MoveMessage(), outlookConnection, outlookmail.MoveMessageInput{MessageID: id})
	require.NoError(t, err)
	require.Equal(t, outlookmail.MoveMessageBranchDefect, result.Branch)
	require.Zero(t, fake.RequestCount(graphtest.EndpointMoveMessage))
}

func TestSetMessageFlagsWritesOnlyTheDifferences(t *testing.T) {
	fake := newGraphFake(t)
	id := fake.AddMessage(graphtest.SeedMessage{FromAddress: testCustomer, Subject: "Refund", ReceivedAt: searchBase, Categories: []string{"Billing"}})
	client, _ := delegatedClient(t, fake)
	isRead, flagged, categories := true, outlookmail.FlagStatusComplete, []string{"billing"}
	result, err := sdkgo.RunMutation(newOutlookDexContext("flags"), client.SetMessageFlags(), outlookConnection, outlookmail.SetMessageFlagsInput{
		MessageID: id, IsRead: &isRead, FlagStatus: &flagged, Categories: &categories,
	})
	require.NoError(t, err)
	require.Equal(t, outlookmail.SetMessageFlagsBranchUpdated, result.Branch, result.Failure)
	require.Equal(t, outlookmail.MessageFlags{MessageID: id, IsRead: true, FlagStatus: outlookmail.FlagStatusComplete, Categories: []string{"Billing"}}, result.Value)
	require.JSONEq(t, `{"isRead":true,"flag":{"flagStatus":"complete"}}`, string(fake.Requests(graphtest.EndpointUpdateMessage)[0].Body),
		"categories already match without case, so only the read state and flag are written")
	stored, _ := fake.Message(id)
	require.True(t, stored.IsRead)
	require.Equal(t, "complete", stored.FlagStatus)

	repeated, err := sdkgo.RunMutation(newOutlookDexContext("flags-again"), client.SetMessageFlags(), outlookConnection, outlookmail.SetMessageFlagsInput{
		MessageID: id, IsRead: &isRead, FlagStatus: &flagged,
	})
	require.NoError(t, err)
	require.True(t, repeated.Value.WasAlreadyApplied)
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointUpdateMessage), "absolute values already held are not written again")
}

func TestSetMessageFlagsReplacesAndClearsCategories(t *testing.T) {
	fake := newGraphFake(t)
	id := fake.AddMessage(graphtest.SeedMessage{FromAddress: testCustomer, Subject: "Refund", ReceivedAt: searchBase, Categories: []string{"Billing", "VIP"}})
	client, _ := delegatedClient(t, fake)
	cleared := []string{}
	result, err := sdkgo.RunMutation(newOutlookDexContext("clear"), client.SetMessageFlags(), outlookConnection, outlookmail.SetMessageFlagsInput{MessageID: id, Categories: &cleared})
	require.NoError(t, err)
	require.Equal(t, outlookmail.SetMessageFlagsBranchUpdated, result.Branch)
	require.Empty(t, result.Value.Categories)
	stored, _ := fake.Message(id)
	require.Empty(t, stored.Categories)
}

func TestSetMessageFlagsRejectsInvalidInput(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := delegatedClient(t, fake)
	unknownFlag := outlookmail.FlagStatus("starred")
	duplicates := []string{"Billing", "billing"}
	padded := []string{" Billing"}
	for _, input := range []outlookmail.SetMessageFlagsInput{
		{MessageID: "AAMkMessage-1="},
		{MessageID: "AAMkMessage-1=", FlagStatus: &unknownFlag},
		{MessageID: "AAMkMessage-1=", Categories: &duplicates},
		{MessageID: "AAMkMessage-1=", Categories: &padded},
	} {
		result, err := sdkgo.RunMutation(newOutlookDexContext("invalid-flags"), client.SetMessageFlags(), outlookConnection, input)
		require.NoError(t, err)
		require.Equal(t, outlookmail.SetMessageFlagsBranchDefect, result.Branch)
	}
	require.Zero(t, fake.RequestCount(graphtest.EndpointGetMessage))
}
