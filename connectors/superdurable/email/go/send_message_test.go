// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	gomessage "github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email/internal/mailtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validSendMessageInput() email.SendMessageInput {
	return email.SendMessageInput{
		To: []string{"jane@acme.example.com"}, Cc: []string{"finance@acme.example.com"}, Bcc: []string{"audit@example.com"},
		ReplyTo: "billing@example.com", Subject: "Ihre Rückerstattung für Bestellung 88213",
		Text: "Hi Jane,\n\nYour refund of $250.00 was approved.\nA line that starts with a dot:\n.\nThanks",
	}
}

func TestSendMessageSubmitsOneMessageWithAStableMessageID(t *testing.T) {
	fixture := newMailFixture(t)
	ctx := newEmailDexContext("send")
	result, err := sdkgo.RunMutation(ctx, fixture.client(t).SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchSent, result.Branch, result.Failure)
	expectedMessageID := "dex-" + string(result.Receipt.CallID) + "@example.com"
	require.Equal(t, expectedMessageID, result.Value.MessageID, "the Message-ID comes from the Step's idempotency key")
	require.Equal(t, string(result.Receipt.CallID), string(result.Receipt.IdempotencyKey))
	require.Equal(t, expectedMessageID, result.Receipt.ProviderObjectID)
	require.Equal(t, testUsername, result.Value.From, "a blank fromAddress uses the complete username")
	require.Equal(t, []string{"jane@acme.example.com", "finance@acme.example.com", "audit@example.com"}, result.Value.Recipients)
	require.False(t, result.Value.SentAt.IsZero())
	require.JSONEq(t, `{"emailSubmittedCallId":"`+string(result.Receipt.CallID)+`"}`, string(ctx.recordedHeartbeat),
		"the submission marker is recorded before the message is sent")

	submissions := fixture.smtp.Submissions()
	require.Len(t, submissions, 1)
	require.Equal(t, testUsername, submissions[0].From)
	require.Equal(t, []string{"jane@acme.example.com", "finance@acme.example.com", "audit@example.com"}, submissions[0].Recipients)
	header, text := readSubmittedMessage(t, submissions[0].Data)
	from, err := header.AddressList("From")
	require.NoError(t, err)
	require.Equal(t, []*mail.Address{{Name: "Acme Support", Address: testUsername}}, from)
	require.Equal(t, "<jane@acme.example.com>", header.Get("To"))
	require.Equal(t, "<finance@acme.example.com>", header.Get("Cc"))
	require.Equal(t, "<billing@example.com>", header.Get("Reply-To"))
	require.Empty(t, header.Get("Bcc"), "blind copies never appear in a header")
	subject, err := header.Subject()
	require.NoError(t, err)
	require.Equal(t, "Ihre Rückerstattung für Bestellung 88213", subject)
	require.NotContains(t, string(submissions[0].Data), "Rückerstattung", "non-ASCII headers are encoded")
	messageID, err := header.MessageID()
	require.NoError(t, err)
	require.Equal(t, expectedMessageID, messageID)
	require.Equal(t, "quoted-printable", header.Get("Content-Transfer-Encoding"))
	require.Equal(t, strings.ReplaceAll(validSendMessageInput().Text, "\n", "\r\n")+"\r\n", text,
		"a line holding only a dot survives dot-stuffing, and SMTP ends the message with a line break")
}

func TestSendMessageNeverSubmitsAgainAfterAnEarlierAttemptClaimed(t *testing.T) {
	fixture := newMailFixture(t)
	first := newEmailDexContext("claimed")
	first.recordedHeartbeat = []byte(`{"emailSubmittedCallId":"earlier"}`)
	result, err := sdkgo.RunMutation(first.nextAttempt(), fixture.client(t).SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchUncertain, result.Branch)
	require.Equal(t, "an earlier attempt of this Step may have submitted the message, so it is not submitted again", result.Failure.Message)
	require.Equal(t, "dex-"+string(result.Receipt.CallID)+"@example.com", result.Value.MessageID, "the Message-ID to look for is still reported")
	require.True(t, result.Value.SentAt.IsZero())
	require.Zero(t, fixture.smtp.AuthCount(), "the attempt never connects")
}

func TestSendMessageRetriesOnlyWhenTheServerProvablyDidNotAccept(t *testing.T) {
	fixture := newMailFixture(t)
	fixture.smtp.FailNextMailCommands(1)
	client := fixture.client(t)
	first := newEmailDexContext("temporary")
	_, err := sdkgo.RunMutation(first, client.SendMessage(), emailConnection, validSendMessageInput())
	message := requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, "the SMTP server answered MAIL FROM with 451 4.3.0", message)
	require.NotContains(t, message, mailtest.SentinelText)
	require.Nil(t, first.recordedHeartbeat, "a provable refusal clears the marker so the retry may submit")
	require.Equal(t, 2, first.heartbeatCount)

	result, err := sdkgo.RunMutation(first.nextAttempt(), client.SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchSent, result.Branch)
	require.Len(t, fixture.smtp.Submissions(), 1)
	require.Equal(t, 2, fixture.smtp.MailCount())
}

func TestSendMessageAnswerLostAfterTheFinalDotIsUncertainAndNeverResent(t *testing.T) {
	fixture := newMailFixture(t)
	fixture.smtp.DropNextEndOfDataReply()
	client := fixture.client(t)
	first := newEmailDexContext("lost-answer")
	result, err := sdkgo.RunMutation(first, client.SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "may have been sent")
	require.Len(t, fixture.smtp.Submissions(), 1, "the server did accept the message")
	require.NotNil(t, first.recordedHeartbeat, "the marker stays, so a replayed attempt submits nothing")

	replayed, err := sdkgo.RunMutation(first.nextAttempt(), client.SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchUncertain, replayed.Branch)
	require.Equal(t, result.Value.MessageID, replayed.Value.MessageID)
	require.Equal(t, 1, fixture.smtp.MailCount(), "the replayed attempt never connected")
}

func TestSendMessageRejectedRecipientSendsNothing(t *testing.T) {
	fixture := newMailFixture(t)
	fixture.smtp.RejectRecipient("finance@acme.example.com")
	ctx := newEmailDexContext("rejected")
	input := validSendMessageInput()
	input.Bcc = nil
	result, err := sdkgo.RunMutation(ctx, fixture.client(t).SendMessage(), emailConnection, input)
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureProviderRejection, result.Failure.Kind)
	require.Equal(t, "the SMTP server answered RCPT TO for recipient 2 of 2 with 550 5.1.1", result.Failure.Message)
	requireNoSecretsOrServerText(t, result.Failure)
	require.Zero(t, fixture.smtp.DataCount())
	require.Nil(t, ctx.recordedHeartbeat)
}

func TestSendMessageRequiresAVerifiedTLSServerAndTheRightPassword(t *testing.T) {
	t.Run("startTLS", func(t *testing.T) {
		fixture := newMailFixtureWithSecurity(t, mailtest.ImplicitTLS, mailtest.StartTLS)
		result, err := sdkgo.RunMutation(newEmailDexContext("starttls"), fixture.client(t).SendMessage(), emailConnection, validSendMessageInput())
		require.NoError(t, err)
		require.Equal(t, email.SendMessageBranchSent, result.Branch, result.Failure)
	})
	t.Run("server without STARTTLS", func(t *testing.T) {
		fixture := newMailFixtureWithSecurity(t, mailtest.ImplicitTLS, mailtest.PlaintextOnly)
		ctx := newEmailDexContext("plaintext")
		result, err := sdkgo.RunMutation(ctx, fixture.client(t).SendMessage(), emailConnection, validSendMessageInput())
		require.NoError(t, err)
		require.Equal(t, email.SendMessageBranchProviderRejected, result.Branch)
		require.Contains(t, result.Failure.Message, "does not offer STARTTLS")
		require.Zero(t, fixture.smtp.AuthCount(), "the password is never sent over plaintext")
		require.Nil(t, ctx.recordedHeartbeat)
	})
	t.Run("wrong password", func(t *testing.T) {
		fixture := newMailFixture(t)
		client := fixture.clientWithCredentials(t, email.Credentials{Username: testUsername, Password: sdkgo.NewSecretString("wrong-password")})
		result, err := sdkgo.RunMutation(newEmailDexContext("wrong-password"), client.SendMessage(), emailConnection, validSendMessageInput())
		require.NoError(t, err)
		require.Equal(t, email.SendMessageBranchProviderRejected, result.Branch)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
		require.Equal(t, "the SMTP server answered AUTH with 535 5.7.8", result.Failure.Message)
		requireNoSecretsOrServerText(t, result.Failure)
	})
	t.Run("separate SMTP relay credentials", func(t *testing.T) {
		fixture := newMailFixture(t)
		relay := mailtest.StartSMTPServer(t, mailtest.SMTPServerConfig{
			Certificates: fixture.certificates, Security: mailtest.ImplicitTLS, Username: "relay-user", Password: "relay-password",
		})
		fixture.config.SMTPPort = int64(relay.Port)
		client := fixture.clientWithCredentials(t, email.Credentials{
			Username: testUsername, Password: sdkgo.NewSecretString(testPassword),
			SMTPUsername: "relay-user", SMTPPassword: sdkgo.NewSecretString("relay-password"),
		})
		result, err := sdkgo.RunMutation(newEmailDexContext("relay"), client.SendMessage(), emailConnection, validSendMessageInput())
		require.NoError(t, err)
		require.Equal(t, email.SendMessageBranchSent, result.Branch, result.Failure)
		require.Len(t, relay.Submissions(), 1)
	})
}

func TestSendMessageDoesNotSubmitWhenDexRejectsTheCheckpoint(t *testing.T) {
	fixture := newMailFixture(t)
	ctx := newEmailDexContext("unrecordable")
	ctx.rejectsHeartbeat = true
	_, err := sdkgo.RunMutation(ctx, fixture.client(t).SendMessage(), emailConnection, validSendMessageInput())
	require.Equal(t, "Dex did not record the submission checkpoint; nothing was submitted", requireRetry(t, err, sdkgo.FailureAvailability))
	require.Zero(t, fixture.smtp.MailCount())
}

func TestSendMessageRejectsInvalidInputWithoutConnecting(t *testing.T) {
	fixture := newMailFixture(t)
	client := fixture.client(t)
	tooMany := make([]string, email.MaxRecipients+1)
	for index := range tooMany {
		tooMany[index] = "customer@example.com"
	}
	for name, change := range map[string]func(*email.SendMessageInput){
		"no recipient":          func(input *email.SendMessageInput) { input.To = nil },
		"too many recipients":   func(input *email.SendMessageInput) { input.To = tooMany },
		"display-name address":  func(input *email.SendMessageInput) { input.To = []string{"Jane <jane@acme.example.com>"} },
		"non-ASCII address":     func(input *email.SendMessageInput) { input.Cc = []string{"jäne@acme.example.com"} },
		"line break in subject": func(input *email.SendMessageInput) { input.Subject = "Refund\r\nBcc: attacker@example.com" },
		"blank text":            func(input *email.SendMessageInput) { input.Text = " \n " },
		"NUL in text":           func(input *email.SendMessageInput) { input.Text = "Hi\x00" },
		"display-name reply-to": func(input *email.SendMessageInput) { input.ReplyTo = "Billing <billing@example.com>" },
	} {
		input := validSendMessageInput()
		change(&input)
		ctx := newEmailDexContext("invalid")
		result, err := sdkgo.RunMutation(ctx, client.SendMessage(), emailConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, email.SendMessageBranchDefect, result.Branch, name)
		require.Zero(t, ctx.heartbeatCount, name)
	}
	require.Zero(t, fixture.smtp.MailCount())
}

// readSubmittedMessage parses a submitted message and returns its header and decoded text.
func readSubmittedMessage(t *testing.T, data []byte) (mail.Header, string) {
	t.Helper()
	entity, err := gomessage.Read(bytes.NewReader(data))
	require.NoError(t, err)
	text, err := io.ReadAll(entity.Body)
	require.NoError(t, err)
	return mail.Header{Header: entity.Header}, string(text)
}
