//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package supportreply

import (
	"testing"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/outlookmailmock"
)

// TestSupportReplyUsesMockDefaultsWithRealDex runs the example on outlookmailmock defaults and checks every
// operation input the Flow mapped.
func TestSupportReplyUsesMockDefaultsWithRealDex(t *testing.T) {
	// The default search serves this customer message on page 2.
	const customerMessageID = "AAMkAGI2MDEyMzQ1AAABC002"
	const secondPageCursor = "https://graph.microsoft.com/v1.0/me/mailFolders/inbox/messages?$top=10&$skip=10"

	mock := outlookmailmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection(), ArchiveFolderSelection{})
	input := integrationInput()
	harness := newReplyHarnessForFlow(t, flow)
	outcome := harness.runReply(t, "mock-default", input)

	require.Equal(t, ReplyActionReplied, outcome.Action)
	require.False(t, outcome.NeedsReview)
	require.Equal(t, customerMessageID, outcome.CustomerMessageID)
	require.Equal(t, 1, outcome.AttachmentCount)
	require.Equal(t, "RE: Refund request for order 88213", outcome.Sent.Subject)
	require.Equal(t, []string{integrationCustomer}, outcome.Sent.Recipients)
	require.NotNil(t, outcome.Flags)
	require.True(t, outcome.Flags.IsRead)
	require.NotNil(t, outcome.ArchivedTo)
	require.Equal(t, "Archive", outcome.ArchivedTo.DestinationFolderName)

	firstPage := outlookmail.SearchMessagesInput{Folder: outlookmail.DefaultFolder, From: integrationCustomer, Limit: customerSearchPageSize}
	secondPage := firstPage
	secondPage.PageCursor = secondPageCursor
	var searches []outlookmail.SearchMessagesInput
	for _, call := range mock.SearchMessages().Calls() {
		searches = append(searches, call.Input)
	}
	// Graph's exact From filter would drop page 1's lookalike; here it exercises the Flow's EqualFold recheck.
	require.Equal(t, []outlookmail.SearchMessagesInput{firstPage, secondPage}, searches, "page 1 has no exact sender match, so page 2 is read")

	reads := mock.GetMessage().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, outlookmail.GetMessageInput{MessageID: customerMessageID}, reads[0].Input)

	replies := mock.ReplyToMessage().Calls()
	require.Len(t, replies, 1)
	require.Equal(t, outlookmail.ReplyToMessageInput{MessageID: customerMessageID, Text: input.ReplyText}, replies[0].Input)

	isRead := true
	flagUpdates := mock.SetMessageFlags().Calls()
	require.Len(t, flagUpdates, 1)
	require.Equal(t, outlookmail.SetMessageFlagsInput{MessageID: customerMessageID, IsRead: &isRead}, flagUpdates[0].Input)

	moves := mock.MoveMessage().Calls()
	require.Len(t, moves, 1)
	require.Equal(t, outlookmail.MoveMessageInput{MessageID: customerMessageID, DestinationFolder: DefaultArchiveFolder}, moves[0].Input)

	require.Empty(t, mock.SendMessage().Calls(), "a customer with an Inbox message gets a reply, not a new message")
}
