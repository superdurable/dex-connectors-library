// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package supportreply

import (
	"testing"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
)

func TestBuildReplyRequestValidatesStartFlowInput(t *testing.T) {
	request, err := BuildReplyRequest(Input{CustomerEmail: " jane@acme.example.com ", ReplyText: " Refunded. ", NewMessageSubject: "Your refund"})
	require.NoError(t, err)
	require.Equal(t, ReplyRequest{CustomerEmail: "jane@acme.example.com", ReplyText: "Refunded.", NewMessageSubject: "Your refund"}, request)
	for _, input := range []Input{
		{CustomerEmail: "Jane <jane@acme.example.com>", ReplyText: "Refunded.", NewMessageSubject: "Your refund"},
		{CustomerEmail: "jane@acme.example.com", ReplyText: " ", NewMessageSubject: "Your refund"},
		{CustomerEmail: "jane@acme.example.com", ReplyText: "Refunded.", NewMessageSubject: "Your\nrefund"},
	} {
		_, err := BuildReplyRequest(input)
		require.Error(t, err)
	}
}

func TestChooseLatestCustomerMessageSkipsOtherSenders(t *testing.T) {
	summaries := []outlookmail.MessageSummary{
		{ID: "other", From: &outlookmail.EmailAddress{Address: "ben@meridian.example.com"}},
		{ID: "no-sender"},
		{ID: "latest", From: &outlookmail.EmailAddress{Address: "Jane@Acme.Example.com"}},
		{ID: "older", From: &outlookmail.EmailAddress{Address: "jane@acme.example.com"}},
	}
	summary, isFound := ChooseLatestCustomerMessage(summaries, "jane@acme.example.com")
	require.True(t, isFound)
	require.Equal(t, "latest", summary.ID)
	_, isFound = ChooseLatestCustomerMessage(summaries[:2], "jane@acme.example.com")
	require.False(t, isFound)
}

func TestOperationMappersBuildTheConnectorInputs(t *testing.T) {
	require.Equal(t, outlookmail.SearchMessagesInput{Folder: "inbox", From: "jane@acme.example.com", Limit: 10, PageCursor: "cursor"},
		MapToSearchMessagesInput(CustomerMessageSearch{CustomerEmail: "jane@acme.example.com", PageCursor: "cursor"}))
	require.Equal(t, outlookmail.ReplyToMessageInput{MessageID: "AAMk1=", Text: "Refunded."}, MapToReplyToMessageInput(CustomerReply{MessageID: "AAMk1=", Text: "Refunded."}))
	flags := MapToSetMessageFlagsInput("AAMk1=")
	require.Equal(t, "AAMk1=", flags.MessageID)
	require.True(t, *flags.IsRead)
	require.Nil(t, flags.FlagStatus)
	require.Nil(t, flags.Categories)
	require.Equal(t, outlookmail.SendMessageInput{To: []string{"jane@acme.example.com"}, Subject: "Your refund", Text: "Refunded."},
		MapToSendMessageInput(ReplyRequest{CustomerEmail: "jane@acme.example.com", ReplyText: "Refunded.", NewMessageSubject: "Your refund"}))
}

func TestNewFlowUsesThePickedArchiveFolderOrTheWellKnownArchive(t *testing.T) {
	require.Equal(t, DefaultArchiveFolder, NewFlow(outlookmail.Connection{}, ArchiveFolderSelection{}).archiveFolderID)
	require.Equal(t, "AAMkFolder-9=", NewFlow(outlookmail.Connection{}, ArchiveFolderSelection{FolderID: " AAMkFolder-9= "}).archiveFolderID)
	require.Equal(t, ArchiveFolderConfigurationRef().StepType, archiveCustomerMessageStepType)
	require.Equal(t, "moveMessage", ArchiveFolderConfigurationRef().OperationID)
}
