// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestReplyToMessageThreadsTheReplyWithoutMarkingTheSourceSeen(t *testing.T) {
	fixture := newMailFixture(t)
	source := fixture.appendMessage(t, "INBOX", plainMessage("Jane Smith <jane@acme.example.com>", testUsername,
		"Refund request for order 88213", "refund@acme.example.com", "I was charged twice.",
		"Cc: Finance <finance@acme.example.com>", "References: <first@acme.example.com>"), nil, januaryTwentyEighth)
	ctx := newEmailDexContext("reply")
	result, err := sdkgo.RunMutation(ctx, fixture.client(t).ReplyToMessage(), emailConnection, email.ReplyToMessageInput{
		Message: source, Text: "Hi Jane,\n\nThe duplicate charge was refunded.",
	})
	require.NoError(t, err)
	require.Equal(t, email.ReplyToMessageBranchSent, result.Branch, result.Failure)
	require.Equal(t, "Re: Refund request for order 88213", result.Value.Subject)
	require.Equal(t, "refund@acme.example.com", result.Value.InReplyTo)
	require.Equal(t, []string{"first@acme.example.com", "refund@acme.example.com"}, result.Value.References)
	require.Equal(t, []string{"jane@acme.example.com"}, result.Value.Recipients, "a plain reply goes to the sender only")
	require.Equal(t, "dex-"+string(result.Receipt.CallID)+"@example.com", result.Value.MessageID)
	require.NotNil(t, ctx.recordedHeartbeat)

	submissions := fixture.smtp.Submissions()
	require.Len(t, submissions, 1)
	header, text := readSubmittedMessage(t, submissions[0].Data)
	require.Equal(t, "<refund@acme.example.com>", header.Get("In-Reply-To"))
	require.Equal(t, "<first@acme.example.com> <refund@acme.example.com>", header.Get("References"))
	require.Equal(t, "<jane@acme.example.com>", header.Get("To"))
	require.Empty(t, header.Get("Cc"))
	require.Equal(t, "Hi Jane,\r\n\r\nThe duplicate charge was refunded.\r\n", text)
	require.Empty(t, fixture.imap.Messages(t, "INBOX")[0].Flags, "reading the source never sets \\Seen")
}

func TestReplyToMessageReplyAllHonorsReplyToAndSkipsTheSender(t *testing.T) {
	fixture := newMailFixture(t)
	source := fixture.appendMessage(t, "INBOX", plainMessage("Jane Smith <jane@acme.example.com>",
		"support@example.com, Ops <ops@acme.example.com>", "RE: Refund", "thread@acme.example.com", "Following up.",
		"Reply-To: billing@acme.example.com", "Cc: finance@acme.example.com, SUPPORT@example.com"), nil, januaryTwentyEighth)
	result, err := sdkgo.RunMutation(newEmailDexContext("reply-all"), fixture.client(t).ReplyToMessage(), emailConnection, email.ReplyToMessageInput{
		Message: source, Text: "Done.", IsReplyAll: true, Cc: []string{"finance@acme.example.com", "manager@example.com"},
	})
	require.NoError(t, err)
	require.Equal(t, email.ReplyToMessageBranchSent, result.Branch, result.Failure)
	require.Equal(t, "RE: Refund", result.Value.Subject, "an existing Re: prefix is not repeated")
	require.Equal(t, []string{"billing@acme.example.com", "ops@acme.example.com", "finance@acme.example.com", "manager@example.com"},
		result.Value.Recipients, "Reply-To replaces From, and the connection's own address is skipped in any case")
}

func TestReplyToMessageSendsNothingWhenTheSourceIsMissingOrUnusable(t *testing.T) {
	fixture := newMailFixture(t)
	noSender := fixture.appendMessage(t, "INBOX", "Subject: No sender\r\nMessage-ID: <nosender@example.com>\r\n\r\nBody.\r\n", nil, januaryTwentyEighth)
	client := fixture.client(t)

	missingContext := newEmailDexContext("missing")
	missing, err := sdkgo.RunMutation(missingContext, client.ReplyToMessage(), emailConnection, email.ReplyToMessageInput{
		Message: email.MessageReference{Mailbox: "INBOX", UIDValidity: noSender.UIDValidity, UID: noSender.UID + 5}, Text: "Hello.",
	})
	require.NoError(t, err)
	require.Equal(t, email.ReplyToMessageBranchNotFound, missing.Branch)
	require.Nil(t, missingContext.recordedHeartbeat, "no submission was claimed")

	unusable, err := sdkgo.RunMutation(newEmailDexContext("unusable"), client.ReplyToMessage(), emailConnection, email.ReplyToMessageInput{
		Message: noSender, Text: "Hello.",
	})
	require.NoError(t, err)
	require.Equal(t, email.ReplyToMessageBranchInvalidResponse, unusable.Branch)
	require.Zero(t, fixture.smtp.AuthCount())
}

func TestReplyToMessageAfterAnEarlierClaimIsUncertainWithoutReadingOrSending(t *testing.T) {
	fixture := newMailFixture(t)
	source := fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername, "Hello", "hello@acme.example.com", "Body."),
		nil, januaryTwentyEighth)
	first := newEmailDexContext("claimed-reply")
	first.recordedHeartbeat = []byte(`{"emailSubmittedCallId":"earlier"}`)
	result, err := sdkgo.RunMutation(first.nextAttempt(), fixture.client(t).ReplyToMessage(), emailConnection, email.ReplyToMessageInput{
		Message: source, Text: "Hello.",
	})
	require.NoError(t, err)
	require.Equal(t, email.ReplyToMessageBranchUncertain, result.Branch)
	require.Equal(t, "dex-"+string(result.Receipt.CallID)+"@example.com", result.Value.MessageID)
	require.Zero(t, fixture.imap.LoginCount(), "the source may have moved since, so it is not read")
	require.Zero(t, fixture.smtp.AuthCount())
}
