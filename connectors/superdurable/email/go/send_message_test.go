// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email_test

import (
	"bytes"
	"io"
	"strconv"
	"strings"
	"testing"

	gomessage "github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email/go"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email/go/internal/mailtest"
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
	require.JSONEq(t, `{"emailSubmittedCallId":"`+string(result.Receipt.CallID)+`","emailAttempt":1,"emailSubmissionCount":1,"emailAccepted":true}`,
		string(ctx.recordedHeartbeat), "the claim before the message and the acceptance after it are recorded")
	require.Equal(t, 2, ctx.heartbeatCount)

	replayed, err := sdkgo.RunMutation(ctx.nextAttempt(), fixture.client(t).SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchUncertain, replayed.Branch, "a replay after an accepted submission never resubmits")
	require.Len(t, fixture.smtp.Submissions(), 1)

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

func TestSendMessageRetriesWithoutSubmittingAfterAForeignCheckpoint(t *testing.T) {
	fixture := newMailFixture(t)
	first := newEmailDexContext("foreign")
	first.recordedHeartbeat = []byte(`{"emailSubmittedCallId":"earlier"}`)
	_, err := sdkgo.RunMutation(first.nextAttempt(), fixture.client(t).SendMessage(), emailConnection, validSendMessageInput())
	require.Equal(t, "the submission checkpoint could not be read; nothing was submitted in this attempt", requireRetry(t, err, sdkgo.FailureAvailability))
	require.Zero(t, fixture.smtp.AuthCount(), "the attempt never connects")
}

func TestSendMessageResubmitsAfterALegacyCheckpointBelowTheSendCap(t *testing.T) {
	callID := sendMessageCallID(t, "legacy")
	fixture := newMailFixture(t)
	first := newEmailDexContext("legacy")
	// A marker written before bounded resend holds only the Call ID, so the earlier attempt counts once.
	first.recordedHeartbeat = []byte(`{"emailSubmittedCallId":"` + string(callID) + `"}`)
	second := first.nextAttempt()
	result, err := sdkgo.RunMutation(second, fixture.client(t).SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchSent, result.Branch)
	require.JSONEq(t, `{"emailSubmittedCallId":"`+string(callID)+`","emailAttempt":2,"emailSubmissionCount":2,"emailAccepted":true}`,
		string(second.recordedHeartbeat))
}

func TestSendMessageRetriesATemporaryRefusalWithoutCountingIt(t *testing.T) {
	fixture := newMailFixture(t)
	fixture.smtp.FailNextMailCommands(3)
	client := fixture.client(t)
	attempt := newEmailDexContext("temporary")
	for range 3 {
		_, err := sdkgo.RunMutation(attempt, client.SendMessage(), emailConnection, validSendMessageInput())
		message := requireRetry(t, err, sdkgo.FailureAvailability)
		require.Equal(t, "the SMTP server answered MAIL FROM with 451 4.3.0", message)
		require.NotContains(t, message, mailtest.SentinelText)
		require.NotContains(t, string(attempt.recordedHeartbeat), "emailSubmissionCount", "a provable refusal is not counted")
		attempt = attempt.nextAttempt()
	}

	result, err := sdkgo.RunMutation(attempt, client.SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchSent, result.Branch, "three refusals leave the whole send cap")
	require.Len(t, fixture.smtp.Submissions(), 1)
	require.Equal(t, 4, fixture.smtp.MailCount())
}

func TestSendMessageResubmitsALostAnswerUntilTheSendCap(t *testing.T) {
	fixture := newMailFixture(t)
	client := fixture.client(t)
	attempt := newEmailDexContext("lost-answer")
	for submission := 1; submission < int(email.SendMessageDefinition.MaxSends); submission++ {
		fixture.smtp.DropNextEndOfDataReply()
		_, err := sdkgo.RunMutation(attempt, client.SendMessage(), emailConnection, validSendMessageInput())
		require.Contains(t, requireRetry(t, err, sdkgo.FailureTransport), "may have been sent")
		require.Contains(t, string(attempt.recordedHeartbeat), `"emailSubmissionCount":`+strconv.Itoa(submission))
		attempt = attempt.nextAttempt()
	}
	fixture.smtp.DropNextEndOfDataReply()
	result, err := sdkgo.RunMutation(attempt, client.SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchUncertain, result.Branch, "the last submission the cap allows is not repeated")
	require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)

	replayed, err := sdkgo.RunMutation(attempt.nextAttempt(), client.SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchUncertain, replayed.Branch)
	require.Equal(t, result.Value.MessageID, replayed.Value.MessageID)
	submissions := fixture.smtp.Submissions()
	require.Len(t, submissions, int(email.SendMessageDefinition.MaxSends), "the replayed attempt never connected")
	for _, submission := range submissions {
		header, _ := readSubmittedMessage(t, submission.Data)
		messageID, err := header.MessageID()
		require.NoError(t, err)
		require.Equal(t, result.Value.MessageID, messageID, "every resubmission repeats the Message-ID")
	}
}

func TestSendMessageResubmitsALostAnswerOnceAndReportsTheAcceptedOne(t *testing.T) {
	fixture := newMailFixture(t)
	fixture.smtp.DropNextEndOfDataReply()
	client := fixture.client(t)
	first := newEmailDexContext("lost-then-sent")
	_, err := sdkgo.RunMutation(first, client.SendMessage(), emailConnection, validSendMessageInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchSent, result.Branch)
	require.Len(t, fixture.smtp.Submissions(), 2)
}

func TestSendMessageRefusalAfterAnEarlierPossibleSubmissionIsUncertain(t *testing.T) {
	fixture := newMailFixture(t)
	fixture.smtp.DropNextEndOfDataReply()
	first := newEmailDexContext("lost-then-refused")
	_, err := sdkgo.RunMutation(first, fixture.client(t).SendMessage(), emailConnection, validSendMessageInput())
	requireRetry(t, err, sdkgo.FailureTransport)

	fixture.smtp.RejectRecipient("finance@acme.example.com")
	result, err := sdkgo.RunMutation(first.nextAttempt(), fixture.client(t).SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchUncertain, result.Branch, "the first submission may have been accepted")

	unusable := fixture.clientWithCredentials(t, email.Credentials{Username: testUsername})
	result, err = sdkgo.RunMutation(first.nextAttempt(), unusable.SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchUncertain, result.Branch, "unusable credentials after a possible submission are not a defect")
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
	require.JSONEq(t, `{"emailSubmittedCallId":"`+string(result.Receipt.CallID)+`","emailAttempt":1}`, string(ctx.recordedHeartbeat),
		"the refusal is recorded as not submitted")
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
		require.NotContains(t, string(ctx.recordedHeartbeat), "emailSubmissionCount", "nothing was submitted")
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

// sendMessageCallID returns the Call ID of a sendMessage Step execution named step.
func sendMessageCallID(t *testing.T, step string) sdkgo.CallID {
	t.Helper()
	result, err := sdkgo.RunMutation(newEmailDexContext(step), newMailFixture(t).client(t).SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	return result.Receipt.CallID
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
