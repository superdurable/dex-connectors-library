//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package supportreply

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/internal/graphtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationMailbox      = "support@contoso.example"
	integrationCustomer     = "jane@acme.example.com"
	integrationClientSecret = "integration-client-secret-not-an-entra-value"
	integrationRefreshToken = "integration-refresh-token"

	// slowGraphDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowGraphDelay = 9 * time.Second
)

var integrationReceivedAt = time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)

func integrationClientID() string { return "6731de76-14a6-49ae" + "-8b2e-0ec0c2d8d5a1" }

func integrationInput() Input {
	return Input{CustomerEmail: integrationCustomer, ReplyText: "Hi Jane,\n\nThe duplicate charge on order 88213 was refunded.", NewMessageSubject: "Your refund for order 88213"}
}

// seedCustomerThread stores the customer's latest message among decoys and returns its ID.
func seedCustomerThread(harness *replyHarness) string {
	harness.graph.AddMessage(graphtest.SeedMessage{FromAddress: integrationCustomer, Subject: "Refund request", Body: "First message.",
		IsRead: true, ReceivedAt: integrationReceivedAt.Add(-48 * time.Hour)})
	latest := harness.graph.AddMessage(graphtest.SeedMessage{FromName: "Jane Smith", FromAddress: integrationCustomer, To: []string{integrationMailbox},
		Subject: "Refund request for order 88213", Body: "I was charged twice for order 88213.", ReceivedAt: integrationReceivedAt,
		Attachments: []graphtest.SeedAttachment{{Name: "receipt.pdf", ContentType: "application/pdf", Size: 20480}}})
	harness.graph.AddMessage(graphtest.SeedMessage{FromAddress: "jane@acme.example.com.au", Subject: "Refund request", Body: "Lookalike.",
		ReceivedAt: integrationReceivedAt.Add(time.Hour)})
	harness.graph.AddMessage(graphtest.SeedMessage{FromAddress: "ben@meridian.example.com", Subject: "Refund request", Body: "Other customer.",
		ReceivedAt: integrationReceivedAt.Add(2 * time.Hour)})
	return latest
}

func TestLatestCustomerMessageGetsOneReplyIsMarkedReadAndArchivedWithRealDex(t *testing.T) {
	harness := newReplyHarness(t, "")
	latest := seedCustomerThread(harness)

	outcome := harness.runReply(t, "threaded-reply", integrationInput())
	require.Equal(t, ReplyActionReplied, outcome.Action)
	require.False(t, outcome.NeedsReview)
	require.Equal(t, latest, outcome.CustomerMessageID, "the lookalike and the other customer are skipped")
	require.Equal(t, 1, outcome.AttachmentCount)
	require.Equal(t, "RE: Refund request for order 88213", outcome.Sent.Subject)
	require.Equal(t, []string{integrationCustomer}, outcome.Sent.Recipients)
	require.False(t, outcome.Sent.WasAlreadySent)
	require.Equal(t, &outlookmail.MessageFlags{MessageID: latest, IsRead: true, FlagStatus: outlookmail.FlagStatusNotFlagged}, outcome.Flags)
	require.Equal(t, &outlookmail.MovedMessage{MessageID: latest, DestinationFolderID: harness.graph.FolderID("archive"), DestinationFolderName: "Archive"},
		outcome.ArchivedTo)

	deliveries := harness.graph.Deliveries()
	require.Len(t, deliveries, 1)
	require.Contains(t, deliveries[0].Body, "The duplicate charge on order 88213 was refunded.")
	require.Contains(t, deliveries[0].Body, "I was charged twice for order 88213.", "Outlook quotes the original")
	archived := harness.graph.MessagesInFolder("archive")
	require.Len(t, archived, 1)
	require.Equal(t, latest, archived[0].ID, "the immutable ID survives the move")
	require.True(t, archived[0].IsRead)
	for _, decoy := range harness.graph.MessagesInFolder("inbox") {
		require.NotEqual(t, latest, decoy.ID)
	}
	require.Empty(t, harness.graph.MessagesInFolder("drafts"))
}

func TestPickedArchiveFolderReceivesTheAnsweredMessageWithRealDex(t *testing.T) {
	harness := newReplyHarness(t, "Answered")
	answered := harness.archiveFolderID
	latest := seedCustomerThread(harness)
	outcome := harness.runReply(t, "picked-folder", integrationInput())
	require.Equal(t, ReplyActionReplied, outcome.Action)
	require.Equal(t, answered, outcome.ArchivedTo.DestinationFolderID)
	require.Equal(t, latest, harness.graph.MessagesInFolder(answered)[0].ID)
	require.Empty(t, harness.graph.MessagesInFolder("archive"))
}

func TestCustomerWithoutAMessageGetsOneNewMessageWithRealDex(t *testing.T) {
	harness := newReplyHarness(t, "")
	harness.graph.AddMessage(graphtest.SeedMessage{FromAddress: "jane@acme.example.com.au", Subject: "Refund", ReceivedAt: integrationReceivedAt})

	outcome := harness.runReply(t, "new-message", integrationInput())
	require.Equal(t, ReplyActionSentNewMessage, outcome.Action)
	require.Empty(t, outcome.CustomerMessageID)
	require.Equal(t, "Your refund for order 88213", outcome.Sent.Subject)
	deliveries := harness.graph.Deliveries()
	require.Len(t, deliveries, 1)
	require.Equal(t, []string{integrationCustomer}, deliveries[0].Recipients)
	require.Zero(t, harness.graph.RequestCount(graphtest.EndpointCreateReply))
}

// TestSlowReplySendIsDispatchedOnceWithRealDex is the duplicate-dispatch test: Graph holds the send for nine
// seconds, past Dex's async local phase, and sync durability means Dex never dispatches a second send.
func TestSlowReplySendIsDispatchedOnceWithRealDex(t *testing.T) {
	harness := newReplyHarness(t, "")
	seedCustomerThread(harness)
	harness.graph.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, Delay: slowGraphDelay})

	startedAt := time.Now()
	outcome := harness.runReply(t, "slow-send", integrationInput())
	require.GreaterOrEqual(t, time.Since(startedAt), slowGraphDelay)
	require.Equal(t, ReplyActionReplied, outcome.Action)
	harness.graph.WaitForHeldRequests()
	require.Equal(t, 1, harness.graph.RequestCount(graphtest.EndpointSendDraft), "no second send while the first was in flight")
	require.Equal(t, 1, harness.graph.RequestCount(graphtest.EndpointCreateReply))
	require.Len(t, harness.graph.Deliveries(), 1)
}

func TestSlowNewMessageSendIsDispatchedOnceWithRealDex(t *testing.T) {
	harness := newReplyHarness(t, "")
	harness.graph.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, Delay: slowGraphDelay})

	outcome := harness.runReply(t, "slow-new-message", integrationInput())
	require.Equal(t, ReplyActionSentNewMessage, outcome.Action)
	harness.graph.WaitForHeldRequests()
	require.Equal(t, 1, harness.graph.RequestCount(graphtest.EndpointSendDraft))
	require.Equal(t, 1, harness.graph.RequestCount(graphtest.EndpointCreateDraft))
	require.Len(t, harness.graph.Deliveries(), 1)
}

// TestSlowMoveIsDispatchedAgainAndArchivesOnceWithRealDex lets async Dex dispatch the move again past its local
// phase; the repeat is safe because the message ID is immutable and the move is read before it is written.
func TestSlowMoveIsDispatchedAgainAndArchivesOnceWithRealDex(t *testing.T) {
	harness := newReplyHarness(t, "")
	latest := seedCustomerThread(harness)
	harness.graph.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointMoveMessage, Delay: slowGraphDelay})

	outcome := harness.runReply(t, "slow-move", integrationInput())
	require.Equal(t, ReplyActionReplied, outcome.Action)
	harness.graph.WaitForHeldRequests()
	require.GreaterOrEqual(t, harness.graph.RequestCount(graphtest.EndpointGetMailFolder), 2, "Dex dispatched the move again past its local phase")
	archived := harness.graph.MessagesInFolder("archive")
	require.Len(t, archived, 1, "the repeated move did not duplicate the message")
	require.Equal(t, latest, archived[0].ID)
	require.Len(t, harness.graph.Deliveries(), 1)
	t.Logf("slow move: move requests=%d archivedTo=%+v", harness.graph.RequestCount(graphtest.EndpointMoveMessage), outcome.ArchivedTo)
}

func TestLostSendAnswerIsConfirmedFromSentItemsAndNeverResentWithRealDex(t *testing.T) {
	harness := newReplyHarness(t, "")
	seedCustomerThread(harness)
	harness.graph.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, ShouldApplyFirst: true, ShouldDropConnection: true})

	outcome := harness.runReply(t, "lost-answer", integrationInput())
	require.Equal(t, ReplyActionReplied, outcome.Action)
	require.True(t, outcome.Sent.WasAlreadySent, "the retry read the Sent Items copy instead of sending again")
	require.Equal(t, 1, harness.graph.RequestCount(graphtest.EndpointSendDraft))
	require.Len(t, harness.graph.Deliveries(), 1)
	require.NotNil(t, outcome.ArchivedTo, "a confirmed reply continues to mark and archive the message")
}

func TestUnconfirmedSendCompletesForReviewAndIsNeverResentWithRealDex(t *testing.T) {
	harness := newReplyHarness(t, "")
	latest := seedCustomerThread(harness)
	harness.graph.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, ShouldDropConnection: true})

	outcome := harness.runReply(t, "unconfirmed", integrationInput())
	require.Equal(t, ReplyActionDeliveryUncertain, outcome.Action)
	require.True(t, outcome.NeedsReview)
	require.Equal(t, "the send was dispatched but its answer was lost and the draft has not appeared as sent, so the message may have been sent; it is not sent again",
		outcome.ReviewDetail)
	require.Equal(t, latest, outcome.CustomerMessageID)
	require.NotEmpty(t, outcome.Sent.MessageID, "the review names the draft to inspect")
	require.Equal(t, 1, harness.graph.RequestCount(graphtest.EndpointSendDraft), "an unconfirmed send is never dispatched again")
	require.Empty(t, harness.graph.Deliveries())
	require.Len(t, harness.graph.MessagesInFolder("drafts"), 1)
	message, _ := harness.graph.Message(latest)
	require.False(t, message.IsRead, "an uncertain reply does not mark the message read")
}

// TestLostWorkerDuringSendIsConfirmedWithoutResendingWithRealDex replaces the Worker while Graph holds the send; the
// next attempt finds the dispatch checkpoint and confirms the send from the draft instead of sending again.
func TestLostWorkerDuringSendIsConfirmedWithoutResendingWithRealDex(t *testing.T) {
	harness := newReplyHarness(t, "")
	seedCustomerThread(harness)
	release := make(chan struct{})
	harness.graph.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, HoldUntil: release})
	flowID := harness.startReply(t, "lost-worker", integrationInput())
	require.Eventually(t, func() bool { return harness.graph.RequestCount(graphtest.EndpointSendDraft) == 1 }, 60*time.Second, 50*time.Millisecond,
		"the first attempt must dispatch the send")
	harness.replaceWorker(t)
	close(release)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome ReplyOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, ReplyActionReplied, outcome.Action)
	require.True(t, outcome.Sent.WasAlreadySent, "the new Worker's attempt confirmed the lost attempt's send")
	harness.graph.WaitForHeldRequests()
	require.Equal(t, 1, harness.graph.RequestCount(graphtest.EndpointSendDraft), "the attempt on the new Worker did not send again")
	require.Len(t, harness.graph.Deliveries(), 1)
}

func TestExpiredAccessTokenIsRefreshedDuringTheFlowWithRealDex(t *testing.T) {
	harness := newReplyHarness(t, "")
	seedCustomerThread(harness)
	harness.graph.ExpireAccessTokens()
	outcome := harness.runReply(t, "refresh", integrationInput())
	require.Equal(t, ReplyActionReplied, outcome.Action)
	require.Equal(t, 1, harness.graph.RequestCount(graphtest.EndpointDelegatedToken))
	stored, _ := harness.credentials.Stored()
	require.Equal(t, harness.graph.CurrentRefreshToken(), stored.RefreshToken.Reveal(), "the rotated refresh token was kept")
}

func TestRejectedRecipientFailsTheFlowWithoutServerTextWithRealDex(t *testing.T) {
	harness := newReplyHarness(t, "")
	seedCustomerThread(harness)
	harness.graph.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, Status: http.StatusBadRequest, ErrorCode: "ErrorInvalidRecipients"})
	flowID := harness.startReply(t, "rejected", integrationInput())

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.NotContains(t, result.ErrorMessage, graphtest.SentinelText)
	require.NotContains(t, result.ErrorMessage, integrationClientSecret)
	require.Empty(t, harness.graph.Deliveries())
	require.Equal(t, 1, harness.graph.RequestCount(graphtest.EndpointSendDraft), "a conclusive refusal is not retried")
	t.Logf("rejected recipient failure: %s", result.ErrorMessage)
}

func TestInvalidRequestFailsBeforeContactingGraphWithRealDex(t *testing.T) {
	harness := newReplyHarness(t, "")
	input := integrationInput()
	input.CustomerEmail = "Jane <jane@acme.example.com>"
	result := harness.waitForFlow(t, harness.startReply(t, "invalid", input))
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, harness.graph.RequestCount(graphtest.EndpointListFolderMessages))
}

// replyHarness owns a Graph fake, a real Worker, and a Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type replyHarness struct {
	graph           *graphtest.Server
	archiveFolderID string
	credentials     *graphtest.CredentialHost[outlookmail.Credentials]
	flow            *Flow
	registry        *dex.Registry
	cache           *blobcache.Cache
	serverAddress   string
	workerAddress   string
	worker          *dex.Worker
	workerResult    chan error
	client          *dex.Client
}

// newReplyHarness starts the fake and a Worker; a non-empty pickedFolderName is created and saved as the picker value.
func newReplyHarness(t *testing.T, pickedFolderName string) *replyHarness {
	t.Helper()
	harness := &replyHarness{
		graph: graphtest.Start(t, graphtest.ServerConfig{
			MailboxAddress: integrationMailbox, ClientID: integrationClientID(), ClientSecret: integrationClientSecret, RefreshToken: integrationRefreshToken,
		}),
		serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
	}
	var archiveFolder ArchiveFolderSelection
	if pickedFolderName != "" {
		harness.archiveFolderID = harness.graph.AddFolder(pickedFolderName)
		archiveFolder = ArchiveFolderSelection{FolderID: harness.archiveFolderID, FolderName: pickedFolderName}
	}
	harness.credentials = harness.newDelegatedCredentials()
	var err error
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.workerAddress = net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness.registerFlow(t, archiveFolder)
	harness.startWorker(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return harness
}

// newDelegatedCredentials holds the credential Dex Web saves after a delegated Microsoft consent.
func (harness *replyHarness) newDelegatedCredentials() *graphtest.CredentialHost[outlookmail.Credentials] {
	expiresAt := time.Now().Add(time.Hour)
	return graphtest.NewCredentialHost(outlookmail.Credentials{
		AuthMethodID: outlookmail.MicrosoftOAuthAuthMethodID, ClientID: integrationClientID(),
		ClientSecret: sdkgo.NewSecretString(integrationClientSecret), AccessToken: sdkgo.NewSecretString(harness.graph.IssueDelegatedAccessToken()),
		RefreshToken: sdkgo.NewSecretString(integrationRefreshToken),
	}, &expiresAt)
}

func (harness *replyHarness) registerFlow(t *testing.T, archiveFolder ArchiveFolderSelection) {
	t.Helper()
	providerClient, err := outlookmail.New(outlookmail.Config{}, harness.credentials, outlookmail.WithLocalProviderURL(harness.graph.URL))
	require.NoError(t, err)
	connection, err := outlookmail.NewConnection(providerClient, sdkgo.ConnectionRef{Provider: "microsoft", Name: ConnectionName})
	require.NoError(t, err)
	harness.flow = NewFlow(connection, archiveFolder)
	harness.registry, err = dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
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
	flowID := "outlook-support-reply-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *replyHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
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
