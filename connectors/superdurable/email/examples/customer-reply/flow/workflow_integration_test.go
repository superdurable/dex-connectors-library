//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerreply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	gomessage "github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email/internal/mailtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationMailbox  = "support@example.com"
	integrationPassword = "integration-app-password-SECRET"
	integrationCustomer = "jane@acme.example.com"

	// slowReplyDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowReplyDelay = 9 * time.Second
)

var integrationReceivedAt = time.Date(2026, 1, 28, 9, 0, 0, 0, time.UTC)

func integrationInput() Input {
	return Input{
		CustomerEmail: integrationCustomer, ReplyText: "Hi Jane,\n\nThe duplicate charge on order 88213 was refunded.",
		NewMessageSubject: "Your refund for order 88213", ArchiveMailbox: "Archive",
	}
}

// seedCustomerThread stores the customer's latest message among decoys and returns its reference.
func seedCustomerThread(t *testing.T, harness *replyHarness) email.MessageReference {
	t.Helper()
	harness.appendMessage(t, customerMessage("Jane Smith <jane@acme.example.com>", "Refund request", "older@acme.example.com",
		"First message."), []imap.Flag{imap.FlagSeen}, integrationReceivedAt.Add(-48*time.Hour))
	latest := harness.appendMessage(t, customerMessage("Jane Smith <jane@acme.example.com>", "Refund request for order 88213",
		"latest@acme.example.com", "I was charged twice for order 88213.", "References: <older@acme.example.com>"), nil, integrationReceivedAt)
	harness.appendMessage(t, customerMessage("Jane Lookalike <jane@acme.example.com.au>", "Refund request", "lookalike@acme.example.com.au",
		"Wrong sender."), nil, integrationReceivedAt.Add(time.Hour))
	harness.appendMessage(t, customerMessage("Ben <ben@meridian.example.com>", "Refund request", "ben@meridian.example.com",
		"Other customer."), nil, integrationReceivedAt.Add(2*time.Hour))
	return latest
}

func TestLatestCustomerMessageGetsOneThreadedReplyWithRealDex(t *testing.T) {
	harness := newReplyHarness(t)
	latest := seedCustomerThread(t, harness)

	outcome := harness.runReply(t, "threaded-reply", integrationInput())
	require.Equal(t, ReplyActionReplied, outcome.Action)
	require.False(t, outcome.NeedsReview)
	require.Equal(t, &latest, outcome.CustomerMessage, "the lookalike sender's newer message is skipped")
	require.Equal(t, "latest@acme.example.com", outcome.CustomerMessageID)
	require.Equal(t, "Re: Refund request for order 88213", outcome.Sent.Subject)
	require.Equal(t, []string{integrationCustomer}, outcome.Sent.Recipients)
	require.True(t, strings.HasPrefix(outcome.Sent.MessageID, "dex-") && strings.HasSuffix(outcome.Sent.MessageID, "@example.com"))

	submissions := harness.smtp.Submissions()
	require.Len(t, submissions, 1)
	require.Equal(t, integrationMailbox, submissions[0].From)
	require.Equal(t, []string{integrationCustomer}, submissions[0].Recipients)
	header, text := readSubmission(t, submissions[0])
	require.Equal(t, "<latest@acme.example.com>", header.Get("In-Reply-To"))
	require.Equal(t, "<older@acme.example.com> <latest@acme.example.com>", header.Get("References"))
	messageID, err := header.MessageID()
	require.NoError(t, err)
	require.Equal(t, outcome.Sent.MessageID, messageID)
	require.Contains(t, text, "The duplicate charge on order 88213 was refunded.")
	require.Contains(t, text, "Jane Smith <jane@acme.example.com> wrote:\r\n> I was charged twice for order 88213.")

	require.Equal(t, &email.MessageFlags{Message: latest, IsSeen: true, IsAnswered: true}, outcome.Flags)
	archived := harness.imap.Messages(t, "Archive")
	require.Len(t, archived, 1)
	require.Equal(t, "latest@acme.example.com", archived[0].MessageID)
	require.ElementsMatch(t, []imap.Flag{imap.FlagSeen, imap.FlagAnswered}, archived[0].Flags)
	require.Equal(t, &email.MessageReference{Mailbox: "Archive", UIDValidity: harness.imap.UIDValidity(t, "Archive"), UID: uint32(archived[0].UID)},
		outcome.ArchivedTo)
	inbox := harness.imap.Messages(t, "INBOX")
	require.Len(t, inbox, 3, "only the answered message left INBOX")
	for _, decoy := range inbox {
		require.NotEqual(t, "latest@acme.example.com", decoy.MessageID)
		if decoy.MessageID != "older@acme.example.com" {
			require.Empty(t, decoy.Flags, "decoy %s must not be touched", decoy.MessageID)
		}
	}
}

func TestCustomerWithoutAMessageGetsOneNewMessageWithRealDex(t *testing.T) {
	harness := newReplyHarness(t)
	harness.appendMessage(t, customerMessage("Jane Lookalike <jane@acme.example.com.au>", "Refund request", "lookalike@acme.example.com.au",
		"Wrong sender."), nil, integrationReceivedAt)

	outcome := harness.runReply(t, "new-message", integrationInput())
	require.Equal(t, ReplyActionSentNewMessage, outcome.Action)
	require.Nil(t, outcome.CustomerMessage)
	require.Equal(t, "Your refund for order 88213", outcome.Sent.Subject)
	require.Empty(t, outcome.Sent.InReplyTo)
	submissions := harness.smtp.Submissions()
	require.Len(t, submissions, 1)
	header, _ := readSubmission(t, submissions[0])
	require.Empty(t, header.Get("In-Reply-To"))
	require.Empty(t, harness.imap.Messages(t, "INBOX")[0].Flags)
	require.Zero(t, harness.imap.MoveCount())
}

func TestCustomerMessageOnALaterSearchPageIsFoundWithRealDex(t *testing.T) {
	harness := newReplyHarness(t)
	latest := harness.appendMessage(t, customerMessage("jane@acme.example.com", "Original question", "page-two@acme.example.com", "Question."),
		nil, integrationReceivedAt)
	for index := 0; index < customerSearchPageSize; index++ {
		harness.appendMessage(t, customerMessage("jane@acme.example.com.au", fmt.Sprintf("Lookalike %d", index),
			fmt.Sprintf("lookalike-%d@acme.example.com.au", index), "Wrong sender."), nil, integrationReceivedAt.Add(time.Duration(index+1)*time.Minute))
	}
	input := integrationInput()
	input.ArchiveMailbox = ""
	outcome := harness.runReply(t, "second-page", input)
	require.Equal(t, ReplyActionReplied, outcome.Action)
	require.Equal(t, &latest, outcome.CustomerMessage)
	require.Nil(t, outcome.ArchivedTo, "a blank archiveMailbox leaves the message in INBOX")
}

// TestSlowSMTPReplyIsSubmittedOnceWithRealDex is the duplicate-dispatch test: the server holds its answer to the
// final dot past Dex's async local phase, and sync durability means Dex never dispatches a second submission.
func TestSlowSMTPReplyIsSubmittedOnceWithRealDex(t *testing.T) {
	harness := newReplyHarness(t)
	seedCustomerThread(t, harness)
	harness.smtp.HoldNextEndOfDataReply(slowReplyDelay)

	startedAt := time.Now()
	outcome := harness.runReply(t, "slow-reply", integrationInput())
	require.GreaterOrEqual(t, time.Since(startedAt), slowReplyDelay)
	require.Equal(t, ReplyActionReplied, outcome.Action)
	harness.smtp.WaitForHeldReplies()
	require.Equal(t, 1, harness.smtp.DataCount(), "no second submission while the first was in flight")
	require.Equal(t, 1, harness.smtp.MailCount())
	require.Len(t, harness.smtp.Submissions(), 1)
}

func TestSlowSMTPNewMessageIsSubmittedOnceWithRealDex(t *testing.T) {
	harness := newReplyHarness(t)
	harness.smtp.HoldNextEndOfDataReply(slowReplyDelay)

	outcome := harness.runReply(t, "slow-new-message", integrationInput())
	require.Equal(t, ReplyActionSentNewMessage, outcome.Action)
	harness.smtp.WaitForHeldReplies()
	require.Equal(t, 1, harness.smtp.DataCount(), "no second submission while the first was in flight")
	require.Equal(t, 1, harness.smtp.MailCount())
}

func TestLostSMTPAnswerCompletesForReviewAndIsNeverResentWithRealDex(t *testing.T) {
	harness := newReplyHarness(t)
	latest := seedCustomerThread(t, harness)
	harness.smtp.DropNextEndOfDataReply()

	outcome := harness.runReply(t, "lost-answer", integrationInput())
	require.Equal(t, ReplyActionDeliveryUncertain, outcome.Action)
	require.True(t, outcome.NeedsReview)
	require.Equal(t, "the message was submitted but the SMTP server's answer did not arrive, so it may have been sent; it is not submitted again",
		outcome.ReviewDetail)
	require.Equal(t, &latest, outcome.CustomerMessage)
	submissions := harness.smtp.Submissions()
	require.Len(t, submissions, 1, "the server did accept the reply, which a person must now find")
	header, _ := readSubmission(t, submissions[0])
	messageID, err := header.MessageID()
	require.NoError(t, err)
	require.Equal(t, outcome.Sent.MessageID, messageID, "the review names the Message-ID that was submitted")
	require.Equal(t, 1, harness.smtp.MailCount(), "an unconfirmed reply is never resent")
	require.Empty(t, harness.imap.Messages(t, "INBOX")[1].Flags, "an uncertain reply does not mark the message answered")
}

// TestLostWorkerDuringSMTPSubmissionReportsUncertainWithoutResendingWithRealDex replaces the Worker while the SMTP
// server holds its answer; the next attempt finds the submission checkpoint and submits nothing.
func TestLostWorkerDuringSMTPSubmissionReportsUncertainWithoutResendingWithRealDex(t *testing.T) {
	harness := newReplyHarness(t)
	seedCustomerThread(t, harness)
	release := make(chan struct{})
	harness.smtp.HoldNextEndOfDataReplyUntil(release)
	flowID := harness.startReply(t, "lost-worker", integrationInput())
	require.Eventually(t, func() bool { return harness.smtp.DataCount() == 1 }, 60*time.Second, 50*time.Millisecond,
		"the first attempt must reach the final dot")
	harness.replaceWorker(t)

	result := harness.waitForFlow(t, flowID)
	close(release)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome ReplyOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, ReplyActionDeliveryUncertain, outcome.Action)
	require.Equal(t, "an earlier attempt of this Step may have submitted the message, so it is not submitted again", outcome.ReviewDetail,
		"the new Worker's attempt found the checkpoint the lost attempt recorded")
	harness.smtp.WaitForHeldReplies()
	require.Equal(t, 1, harness.smtp.MailCount(), "the attempt on the new Worker did not resend the reply")
	require.Equal(t, 1, harness.smtp.DataCount())
}

func TestTemporarySMTPRefusalIsRetriedAndSendsOnceWithRealDex(t *testing.T) {
	harness := newReplyHarness(t)
	seedCustomerThread(t, harness)
	harness.smtp.FailNextMailCommands(1)

	outcome := harness.runReply(t, "temporary-refusal", integrationInput())
	require.Equal(t, ReplyActionReplied, outcome.Action, "a 451 clears the checkpoint, so the retry may submit")
	require.Equal(t, 2, harness.smtp.MailCount())
	require.Len(t, harness.smtp.Submissions(), 1)
}

// TestSlowIMAPMoveIsDispatchedAgainAndMovesOnceWithRealDex lets async Dex dispatch the move again past its local
// phase; the repeat is safe because a message can be moved only once.
func TestSlowIMAPMoveIsDispatchedAgainAndMovesOnceWithRealDex(t *testing.T) {
	harness := newReplyHarness(t)
	seedCustomerThread(t, harness)
	harness.imap.HoldNextMove(slowReplyDelay)

	outcome := harness.runReply(t, "slow-move", integrationInput())
	require.Equal(t, ReplyActionReplied, outcome.Action)
	harness.imap.WaitForDelayedMoves()
	require.GreaterOrEqual(t, harness.imap.MoveCount(), 2, "Dex dispatched the move again past its local phase")
	archived := harness.imap.Messages(t, "Archive")
	require.Len(t, archived, 1, "the repeated move did not copy the message twice")
	require.Equal(t, "latest@acme.example.com", archived[0].MessageID)
	require.Len(t, harness.smtp.Submissions(), 1)
	t.Logf("slow move: moves=%d archivedTo=%+v", harness.imap.MoveCount(), outcome.ArchivedTo)
}

func TestRejectedRecipientFailsTheFlowWithoutServerTextWithRealDex(t *testing.T) {
	harness := newReplyHarness(t)
	seedCustomerThread(t, harness)
	harness.smtp.RejectRecipient(integrationCustomer)
	flowID := harness.startReply(t, "rejected", integrationInput())

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.NotContains(t, result.ErrorMessage, mailtest.SentinelText)
	require.NotContains(t, result.ErrorMessage, integrationPassword)
	require.Zero(t, harness.smtp.DataCount(), "nothing was submitted")
	require.Equal(t, 1, harness.smtp.MailCount(), "a conclusive refusal is not retried")
	t.Logf("rejected recipient failure: %s", result.ErrorMessage)
}

func TestInvalidRequestFailsBeforeContactingTheServersWithRealDex(t *testing.T) {
	harness := newReplyHarness(t)
	input := integrationInput()
	input.CustomerEmail = "Jane <jane@acme.example.com>"
	flowID := harness.startReply(t, "invalid", input)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, harness.imap.LoginCount())
	require.Zero(t, harness.smtp.AuthCount())
}

func customerMessage(from string, subject string, messageID string, body string, extraHeaders ...string) string {
	lines := []string{
		"From: " + from, "To: " + integrationMailbox, "Subject: " + subject, "Date: Wed, 28 Jan 2026 09:00:00 +0000",
		"Message-ID: <" + messageID + ">", "MIME-Version: 1.0", "Content-Type: text/plain; charset=utf-8",
	}
	return strings.Join(append(lines, extraHeaders...), "\r\n") + "\r\n\r\n" + body + "\r\n"
}

func readSubmission(t *testing.T, submission mailtest.Submission) (mail.Header, string) {
	t.Helper()
	entity, err := gomessage.Read(bytes.NewReader(submission.Data))
	require.NoError(t, err)
	text, err := io.ReadAll(entity.Body)
	require.NoError(t, err)
	return mail.Header{Header: entity.Header}, string(text)
}

// replyHarness owns in-process IMAP and SMTP servers, a real Worker, and a Client against the Dex Server at
// DEX_FLOW_SERVICE_ADDRESS.
type replyHarness struct {
	imap          *mailtest.IMAPServer
	smtp          *mailtest.SMTPServer
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newReplyHarness(t *testing.T) *replyHarness {
	t.Helper()
	certificates := mailtest.NewCertificates(t)
	harness := &replyHarness{
		imap: mailtest.StartIMAPServer(t, mailtest.IMAPServerConfig{
			Certificates: certificates, Security: mailtest.ImplicitTLS, Username: integrationMailbox, Password: integrationPassword,
			Mailboxes: []string{"Archive"},
		}),
		smtp: mailtest.StartSMTPServer(t, mailtest.SMTPServerConfig{
			Certificates: certificates, Security: mailtest.StartTLS, Username: integrationMailbox, Password: integrationPassword,
		}),
		serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
	}
	reference := sdkgo.ConnectionRef{Provider: "email", Name: ConnectionName}
	providerClient, err := email.New(email.Config{
		IMAPHost: harness.imap.Host, IMAPPort: int64(harness.imap.Port), IMAPSecurity: email.IMAPSecurityImplicitTLS,
		SMTPHost: harness.smtp.Host, SMTPPort: int64(harness.smtp.Port), SMTPSecurity: email.SMTPSecurityStartTLS,
		FromName: "Acme Support",
	}, sdkgo.StaticCredentialProvider[email.Credentials]{
		reference: {Username: integrationMailbox, Password: sdkgo.NewSecretString(integrationPassword)},
	}, email.WithTLSRootCAs(certificates.RootCAs))
	require.NoError(t, err)
	connection, err := email.NewConnection(providerClient, reference)
	require.NoError(t, err)
	harness.flow = NewFlow(connection)
	harness.registry, err = dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.workerAddress = net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.startWorker(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return harness
}

func (harness *replyHarness) appendMessage(t *testing.T, raw string, flags []imap.Flag, receivedAt time.Time) email.MessageReference {
	t.Helper()
	uid := harness.imap.AppendMessage(t, "INBOX", []byte(raw), flags, receivedAt)
	return email.MessageReference{Mailbox: "INBOX", UIDValidity: harness.imap.UIDValidity(t, "INBOX"), UID: uint32(uid)}
}

func (harness *replyHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress, WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	harness.worker, harness.workerResult = worker, workerResult
}

// replaceWorker force-stops the Worker without draining its handlers, like a crash, then starts a new one.
func (harness *replyHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *replyHarness) runReply(t *testing.T, scenario string, input Input) ReplyOutcome {
	t.Helper()
	flowID := harness.startReply(t, scenario, input)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome ReplyOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

func (harness *replyHarness) startReply(t *testing.T, scenario string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "email-customer-reply-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *replyHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for {
		result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err, "Flow %s did not close", flowID)
		return result
	}
}

func availableIntegrationPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
