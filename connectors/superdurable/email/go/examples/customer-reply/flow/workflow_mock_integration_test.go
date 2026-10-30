//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerreply

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email/go"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email/go/emailmock"
)

func TestCustomerReplyUsesMockWithRealDex(t *testing.T) {
	mock := emailmock.New(t, ConnectionName)
	harness := newReplyHarnessForConnection(t, mock.Connection())
	input := Input{
		CustomerEmail:     "jane@acme.example.com",
		ReplyText:         "Hi Jane,\n\nThe duplicate charge on order 88213 was refunded.",
		NewMessageSubject: "Your refund for order 88213", ArchiveMailbox: "Archive",
	}
	outcome := harness.runReply(t, "mock-default", input)
	reference := email.MessageReference{Mailbox: "INBOX", UIDValidity: 42, UID: 100}
	require.Equal(t, ReplyActionReplied, outcome.Action)
	require.Equal(t, &reference, outcome.CustomerMessage)
	require.Equal(t, "latest@acme.example.com", outcome.CustomerMessageID)
	require.Equal(t, &email.MessageReference{Mailbox: "Archive", UIDValidity: 43, UID: 50}, outcome.ArchivedTo)

	searchCalls := mock.SearchMessages().Calls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, email.SearchMessagesInput{Mailbox: "INBOX", From: "jane@acme.example.com", Limit: 10}, searchCalls[0].Input)
	require.Equal(t, email.SearchMessagesBranchSearched, searchCalls[0].Branch)
	getCalls := mock.GetMessage().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, email.GetMessageInput{Message: reference}, getCalls[0].Input)
	require.Equal(t, email.GetMessageBranchFound, getCalls[0].Branch)
	replyCalls := mock.ReplyToMessage().Calls()
	require.Len(t, replyCalls, 1)
	require.Equal(t, email.ReplyToMessageInput{
		Message: reference,
		Text:    "Hi Jane,\n\nThe duplicate charge on order 88213 was refunded.\n\nOn Wed, 28 Jan 2026 at 09:00 UTC, Jane Smith <jane@acme.example.com> wrote:\n> I was charged twice for order 88213.\n",
	}, replyCalls[0].Input)
	require.Equal(t, email.ReplyToMessageBranchSent, replyCalls[0].Branch)
	require.Empty(t, mock.SendMessage().Calls())
	flagsCalls := mock.SetFlags().Calls()
	require.Len(t, flagsCalls, 1)
	isSet := true
	require.Equal(t, email.SetFlagsInput{Message: reference, IsSeen: &isSet, IsAnswered: &isSet}, flagsCalls[0].Input)
	require.Equal(t, email.SetFlagsBranchUpdated, flagsCalls[0].Branch)
	moveCalls := mock.MoveMessage().Calls()
	require.Len(t, moveCalls, 1)
	require.Equal(t, email.MoveMessageInput{
		Message: reference, DestinationMailbox: "Archive", MessageID: "latest@acme.example.com",
	}, moveCalls[0].Input)
	require.Equal(t, email.MoveMessageBranchMoved, moveCalls[0].Branch)
}
