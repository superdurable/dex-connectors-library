// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email_test

import (
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email/internal/mailtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestMoveMessageMovesOnceAndRecognizesAnEarlierMove(t *testing.T) {
	fixture := newMailFixture(t)
	source := fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername, "Hello", "move@acme.example.com", "Body."),
		nil, januaryTwentyEighth)
	client := fixture.client(t)
	input := email.MoveMessageInput{Message: source, DestinationMailbox: "Archive", MessageID: "move@acme.example.com"}

	moved, err := sdkgo.RunMutation(newEmailDexContext("move"), client.MoveMessage(), emailConnection, input)
	require.NoError(t, err)
	require.Equal(t, email.MoveMessageBranchMoved, moved.Branch, moved.Failure)
	require.False(t, moved.Value.WasAlreadyMoved)
	archived := fixture.imap.Messages(t, "Archive")
	require.Len(t, archived, 1)
	require.Equal(t, email.MessageReference{Mailbox: "Archive", UIDValidity: fixture.imap.UIDValidity(t, "Archive"), UID: uint32(archived[0].UID)},
		moved.Value.Destination, "COPYUID names the destination UID")
	require.Empty(t, fixture.imap.Messages(t, "INBOX"))

	repeated, err := sdkgo.RunMutation(newEmailDexContext("move-again"), client.MoveMessage(), emailConnection, input)
	require.NoError(t, err)
	require.Equal(t, email.MoveMessageBranchMoved, repeated.Branch)
	require.True(t, repeated.Value.WasAlreadyMoved)
	require.Equal(t, moved.Value.Destination, repeated.Value.Destination)
	require.Equal(t, 1, fixture.imap.MoveCount(), "the repeat found the message by Message-ID and moved nothing")

	input.MessageID = ""
	withoutMessageID, err := sdkgo.RunMutation(newEmailDexContext("move-blind"), client.MoveMessage(), emailConnection, input)
	require.NoError(t, err)
	require.Equal(t, email.MoveMessageBranchNotFound, withoutMessageID.Branch)
}

func TestMoveMessageLostReplyIsRetriedAndFindsTheMessageMoved(t *testing.T) {
	fixture := newMailFixture(t)
	source := fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername, "Hello", "lost@acme.example.com", "Body."),
		nil, januaryTwentyEighth)
	fixture.imap.DropNextMoveReply()
	client := fixture.client(t)
	input := email.MoveMessageInput{Message: source, DestinationMailbox: "Archive", MessageID: "lost@acme.example.com"}

	_, err := sdkgo.RunMutation(newEmailDexContext("lost-move"), client.MoveMessage(), emailConnection, input)
	requireRetry(t, err, sdkgo.FailureTransport)
	retried, err := sdkgo.RunMutation(newEmailDexContext("lost-move"), client.MoveMessage(), emailConnection, input)
	require.NoError(t, err)
	require.Equal(t, email.MoveMessageBranchMoved, retried.Branch)
	require.True(t, retried.Value.WasAlreadyMoved)
	require.Len(t, fixture.imap.Messages(t, "Archive"), 1)
}

func TestMoveMessageFallsBackToCopyOnlyWithUIDPlus(t *testing.T) {
	certificates := mailtest.NewCertificates(t)
	for name, capabilities := range map[string]imap.CapSet{
		"UIDPLUS without MOVE":     {imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}},
		"neither MOVE nor UIDPLUS": {imap.CapIMAP4rev1: {}},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newMailFixture(t)
			fixture.imap = mailtest.StartIMAPServer(t, mailtest.IMAPServerConfig{
				Certificates: certificates, Security: mailtest.ImplicitTLS, Username: testUsername, Password: testPassword,
				Mailboxes: []string{"Archive"}, Capabilities: capabilities,
			})
			fixture.certificates, fixture.config.IMAPPort = certificates, int64(fixture.imap.Port)
			kept := fixture.appendMessage(t, "INBOX", plainMessage("ben@example.com", testUsername, "Keep", "keep@example.com", "Body."),
				[]imap.Flag{imap.FlagDeleted}, januaryTwentyEighth)
			source := fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername, "Move", "copy@example.com", "Body."),
				nil, januaryTwentyEighth)
			result, err := sdkgo.RunMutation(newEmailDexContext("fallback"), fixture.client(t).MoveMessage(), emailConnection,
				email.MoveMessageInput{Message: source, DestinationMailbox: "Archive"})
			require.NoError(t, err)
			inbox := fixture.imap.Messages(t, "INBOX")
			if capabilities.Has(imap.CapUIDPlus) {
				require.Equal(t, email.MoveMessageBranchMoved, result.Branch, result.Failure)
				require.Len(t, fixture.imap.Messages(t, "Archive"), 1)
				require.Len(t, inbox, 1, "UID EXPUNGE removed only the moved message")
				require.Equal(t, imap.UID(kept.UID), inbox[0].UID)
				return
			}
			require.Equal(t, email.MoveMessageBranchProviderRejected, result.Branch)
			require.Len(t, inbox, 2, "nothing is expunged without UIDPLUS")
		})
	}
}

func TestMoveMessageRejectsMissingDestinationsAndInvalidInput(t *testing.T) {
	fixture := newMailFixture(t)
	source := fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername, "Hello", "dest@acme.example.com", "Body."),
		nil, januaryTwentyEighth)
	client := fixture.client(t)

	missing, err := sdkgo.RunMutation(newEmailDexContext("missing-destination"), client.MoveMessage(), emailConnection,
		email.MoveMessageInput{Message: source, DestinationMailbox: "Projects"})
	require.NoError(t, err)
	require.Equal(t, email.MoveMessageBranchProviderRejected, missing.Branch)
	require.Equal(t, sdkgo.FailureNotFound, missing.Failure.Kind)
	require.Equal(t, "the IMAP server answered MOVE with NO [TRYCREATE]", missing.Failure.Message)

	mismatched, err := sdkgo.RunMutation(newEmailDexContext("mismatch"), client.MoveMessage(), emailConnection,
		email.MoveMessageInput{Message: source, DestinationMailbox: "Archive", MessageID: "other@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, email.MoveMessageBranchNotFound, mismatched.Branch)
	require.Len(t, fixture.imap.Messages(t, "INBOX"), 1)

	for name, input := range map[string]email.MoveMessageInput{
		"same mailbox":      {Message: source, DestinationMailbox: "inbox"},
		"blank destination": {Message: source},
		"bracketed id":      {Message: source, DestinationMailbox: "Archive", MessageID: "<dest@acme.example.com>"},
	} {
		result, err := sdkgo.RunMutation(newEmailDexContext("invalid"), client.MoveMessage(), emailConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, email.MoveMessageBranchDefect, result.Branch, name)
	}
}
