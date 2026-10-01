// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerreply

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := NewFlow(newUnitTestConnection(t, ConnectionName))
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordReplyRequestStepType, dex.GetFinalStepType[Input](recordReplyRequest{}))
	require.Equal(t, chooseLatestCustomerMessageStepType, dex.GetFinalStepType[email.SearchMessagesResult](chooseLatestCustomerMessage{}))
	require.Equal(t, prepareThreadedReplyStepType, dex.GetFinalStepType[email.GetMessageResult](prepareThreadedReply{}))
	require.Equal(t, recordReplySentStepType, dex.GetFinalStepType[email.ReplyToMessageResult](recordReplySent{}))
	require.Equal(t, recordMessageMarkedStepType, dex.GetFinalStepType[email.SetFlagsResult](recordMessageMarked{}))
	require.Equal(t, completeCustomerReplyStepType, dex.GetFinalStepType[email.MoveMessageResult](completeCustomerReply{}))
	require.Equal(t, recordNewMessageSentStepType, dex.GetFinalStepType[email.SendMessageResult](recordNewMessageSent{}))
	require.Equal(t, recordUncertainDeliveryStepType, dex.GetFinalStepType[email.SendMessageResult](recordUncertainDelivery{}))
	wait, err := recordReplyRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(newUnitTestConnection(t, ConnectionName))})
	require.NoError(t, err)
	require.Panics(t, func() {
		_, _ = dex.NewRegistry([]dex.Flow{NewFlow(newUnitTestConnection(t, "another-connection"))})
	})
}

func TestBuildReplyRequestValidatesStartInput(t *testing.T) {
	request, err := BuildReplyRequest(Input{
		CustomerEmail: " jane@acme.example.com ", ReplyText: " Refunded. ", NewMessageSubject: " Your refund ", ArchiveMailbox: " Archive ",
	})
	require.NoError(t, err)
	require.Equal(t, ReplyRequest{CustomerEmail: "jane@acme.example.com", ReplyText: "Refunded.", NewMessageSubject: "Your refund", ArchiveMailbox: "Archive"}, request)
	valid := Input{CustomerEmail: "jane@acme.example.com", ReplyText: "Refunded.", NewMessageSubject: "Your refund"}
	for name, change := range map[string]func(*Input){
		"display address":    func(input *Input) { input.CustomerEmail = "Jane <jane@acme.example.com>" },
		"two addresses":      func(input *Input) { input.CustomerEmail = "jane@acme.example.com, ben@example.com" },
		"blank reply":        func(input *Input) { input.ReplyText = " " },
		"multi-line title":   func(input *Input) { input.NewMessageSubject = "Refund\r\nBcc: x@example.com" },
		"archive into INBOX": func(input *Input) { input.ArchiveMailbox = "inbox" },
	} {
		input := valid
		change(&input)
		_, err := BuildReplyRequest(input)
		require.Error(t, err, name)
	}
}

func TestChooseLatestCustomerMessageSkipsLookalikeSenders(t *testing.T) {
	summaries := []email.MessageSummary{
		{Reference: email.MessageReference{UID: 9}, From: email.EmailAddress{Address: "jane@acme.example.com.au"}},
		{Reference: email.MessageReference{UID: 8}, From: email.EmailAddress{Address: "Jane@Acme.Example.com"}},
		{Reference: email.MessageReference{UID: 7}, From: email.EmailAddress{Address: "jane@acme.example.com"}},
	}
	chosen, isFound := ChooseLatestCustomerMessage(summaries, "jane@acme.example.com")
	require.True(t, isFound)
	require.Equal(t, uint32(8), chosen.Reference.UID, "addresses compare without letter case and the newest match wins")
	_, isFound = ChooseLatestCustomerMessage(summaries[:1], "jane@acme.example.com")
	require.False(t, isFound)
}

func TestBuildThreadedReplyTextQuotesABoundedExcerpt(t *testing.T) {
	message := email.Message{
		MessageSummary: email.MessageSummary{
			From: email.EmailAddress{Name: "Jane Smith", Address: "jane@acme.example.com"}, ReceivedAt: time.Date(2026, 1, 28, 9, 12, 0, 0, time.UTC),
		},
		Text: "I was charged twice.\r\nOrder 88213.",
	}
	require.Equal(t, "Refunded.\n\nOn Wed, 28 Jan 2026 at 09:12 UTC, Jane Smith <jane@acme.example.com> wrote:\n> I was charged twice.\n> Order 88213.\n",
		BuildThreadedReplyText(" Refunded. ", message))
	long := message
	for index := 0; index < maximumQuotedLines+5; index++ {
		long.Text += "\nline"
	}
	quoted := BuildThreadedReplyText("Refunded.", long)
	require.Contains(t, quoted, "> ...\n")
}

func newUnitTestConnection(t *testing.T, name string) email.Connection {
	t.Helper()
	client, err := email.New(email.Config{IMAPHost: "imap.example.com", SMTPHost: "smtp.example.com"}, sdkgo.StaticCredentialProvider[email.Credentials]{})
	require.NoError(t, err)
	connection, err := email.NewConnection(client, sdkgo.ConnectionRef{Provider: "email", Name: name})
	require.NoError(t, err)
	return connection
}
