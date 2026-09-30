//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textmessagedelivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// A split literal keeps the synthetic SID from matching repository secret scanning.
	integrationAccountSID = "AC" + "0123456789abcdef0123456789abcdef"
	integrationAuthToken  = "integration-auth-token-0123456789"
	integrationSender     = "+14155550100"

	recipientDelivered   = "+14155550101"
	recipientRejected    = "+14155550102"
	recipientTimeout     = "+14155550103"
	recipientServerError = "+14155550104"
	recipientRateLimited = "+14155550105"
	recipientUnconfirmed = "+14155550106"
	recipientSlow        = "+14155550107"

	integrationMaximumChecks = 3
	shortRequestTimeout      = 500 * time.Millisecond
	// slowRequestTimeout outlasts Dex's roughly seven-second async local phase.
	slowRequestTimeout = 9 * time.Second
)

func TestAcceptedTextIsSentOnceAndFollowedToDeliveryWithRealDex(t *testing.T) {
	provider, flow, harness := newTwilioIntegrationHarness(t)
	ctx := integrationContext(t)

	delivery := runTextMessageFlow(t, ctx, harness, flow, "delivered", Input{To: recipientDelivered, Body: "Your table is ready."})
	require.Equal(t, PhaseFinished, delivery.Phase)
	require.Equal(t, messaging.MessageStatusDelivered, delivery.Message.Status)
	require.Equal(t, provider.messageSID(recipientDelivered, 1), delivery.Message.SID)
	require.Equal(t, 2, delivery.StatusChecks, "the first read saw sent, the second delivered")
	require.Equal(t, 1, delivery.SendAttempts)
	require.Equal(t, 1, provider.createCount(recipientDelivered))
	require.Equal(t, 2, provider.readCount(delivery.Message.SID))
}

func TestRejectedTextCompletesWithTheTwilioErrorCodeWithRealDex(t *testing.T) {
	provider, flow, harness := newTwilioIntegrationHarness(t)
	ctx := integrationContext(t)

	delivery := runTextMessageFlow(t, ctx, harness, flow, "rejected", Input{To: recipientRejected, Body: "Trial accounts need verified numbers."})
	require.Equal(t, PhaseRejected, delivery.Phase)
	require.Equal(t, 21608, delivery.Message.ErrorCode)
	require.Empty(t, delivery.Message.SID)
	require.Equal(t, 1, provider.createCount(recipientRejected))
}

func TestRateLimitedTextIsRetriedAfterRetryAfterWithRealDex(t *testing.T) {
	provider, flow, harness := newTwilioIntegrationHarness(t)
	ctx := integrationContext(t)

	delivery := runTextMessageFlow(t, ctx, harness, flow, "rate-limited", Input{To: recipientRateLimited, Body: "Busy hour."})
	require.Equal(t, PhaseFinished, delivery.Phase)
	require.Equal(t, 2, provider.createCount(recipientRateLimited), "a 429 created nothing, so Dex retries the send")
	attempts := provider.createTimes(recipientRateLimited)
	require.GreaterOrEqual(t, attempts[1].Sub(attempts[0]), time.Second, "the retry waits for Retry-After")
}

func TestTimeoutAfterDispatchIsReconciledWithoutResendingWithRealDex(t *testing.T) {
	provider, flow, harness := newTwilioIntegrationHarness(t)
	ctx := integrationContext(t)
	flowID := startTextMessageFlow(t, ctx, harness, flow, "timeout", Input{To: recipientTimeout, Body: "Your code is ready."})

	uncertain := waitForPhase(t, ctx, harness, flow, flowID, PhaseNeedsReconciliation)
	require.NotNil(t, uncertain.UncertainSend)
	require.Equal(t, sdkgo.FailureTransport, uncertain.UncertainSend.FailureKind)
	require.NotEmpty(t, uncertain.UncertainSend.CallID)
	require.Empty(t, uncertain.Message.SID)
	require.Equal(t, 1, provider.createCount(recipientTimeout), "a timeout after dispatch is never retried")

	unknownSID := "SM" + strings.Repeat("0", 32)
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ConfirmSentTextMessage, ConfirmSentTextMessageInput{MessageSID: unknownSID}, nil))
	unknown := waitForReconciliationNote(t, ctx, harness, flow, flowID, "Twilio has no message with the reported SID")
	require.Equal(t, PhaseNeedsReconciliation, unknown.Phase)

	otherRecipientSID := provider.createUnrelatedMessage(recipientDelivered)
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ConfirmSentTextMessage, ConfirmSentTextMessageInput{MessageSID: otherRecipientSID}, nil))
	wrongRecipient := waitForReconciliationNote(t, ctx, harness, flow, flowID, "the reported message was sent to a different recipient")
	require.Equal(t, PhaseNeedsReconciliation, wrongRecipient.Phase)

	createdSID := provider.messageSID(recipientTimeout, 1)
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ConfirmSentTextMessage, ConfirmSentTextMessageInput{MessageSID: createdSID}, nil))
	delivery := waitForFlowOutput(t, ctx, harness, flowID)
	require.Equal(t, PhaseFinished, delivery.Phase)
	require.Equal(t, createdSID, delivery.Message.SID)
	require.Equal(t, messaging.MessageStatusDelivered, delivery.Message.Status)
	require.Nil(t, delivery.UncertainSend)
	require.Equal(t, 1, delivery.SendAttempts)
	require.Equal(t, 1, provider.createCount(recipientTimeout), "reconciliation adopted the existing message")
}

// TestSlowDispatchIsSentOnceWithRealDex guards sync durability; an async fallback attempt re-sends slow requests.
func TestSlowDispatchIsSentOnceWithRealDex(t *testing.T) {
	provider, flow, harness := newTwilioIntegrationHarnessWithRequestTimeout(t, slowRequestTimeout)
	ctx := integrationContext(t)
	flowID := startTextMessageFlow(t, ctx, harness, flow, "slow", Input{To: recipientSlow, Body: "Your code is ready."})

	uncertain := waitForPhase(t, ctx, harness, flow, flowID, PhaseNeedsReconciliation)
	require.Equal(t, sdkgo.FailureTransport, uncertain.UncertainSend.FailureKind)
	require.Equal(t, 1, provider.createCount(recipientSlow), "one Step execution dispatches one create request")
}

func TestServerErrorIsResentOnlyAfterOperatorApprovalWithRealDex(t *testing.T) {
	provider, flow, harness := newTwilioIntegrationHarness(t)
	ctx := integrationContext(t)
	flowID := startTextMessageFlow(t, ctx, harness, flow, "server-error", Input{To: recipientServerError, Body: "Your order shipped."})

	uncertain := waitForPhase(t, ctx, harness, flow, flowID, PhaseNeedsReconciliation)
	require.Equal(t, sdkgo.FailureAvailability, uncertain.UncertainSend.FailureKind)
	require.Equal(t, 1, provider.createCount(recipientServerError))

	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ApproveTextMessageResend, nil, nil))
	delivery := waitForFlowOutput(t, ctx, harness, flowID)
	require.Equal(t, PhaseFinished, delivery.Phase)
	require.Equal(t, 2, delivery.SendAttempts)
	require.Equal(t, provider.messageSID(recipientServerError, 2), delivery.Message.SID)
	require.Equal(t, 2, provider.createCount(recipientServerError), "only the approved resend created a second request")
}

func TestUnconfirmedDeliveryStopsAfterTheLastStatusReadWithRealDex(t *testing.T) {
	provider, flow, harness := newTwilioIntegrationHarness(t)
	ctx := integrationContext(t)

	delivery := runTextMessageFlow(t, ctx, harness, flow, "unconfirmed", Input{To: recipientUnconfirmed, Body: "No receipt carrier."})
	require.Equal(t, PhaseDeliveryStatusUnconfirmed, delivery.Phase)
	require.Equal(t, messaging.MessageStatusSent, delivery.Message.Status)
	require.Equal(t, integrationMaximumChecks, delivery.StatusChecks)
	require.Equal(t, integrationMaximumChecks, provider.readCount(delivery.Message.SID))
}

func runTextMessageFlow(t *testing.T, ctx context.Context, harness *twilioIntegrationHarness, flow *Flow, scenario string, input Input) TextMessageDelivery {
	t.Helper()
	flowID := startTextMessageFlow(t, ctx, harness, flow, scenario, input)
	return waitForFlowOutput(t, ctx, harness, flowID)
}

func startTextMessageFlow(t *testing.T, ctx context.Context, harness *twilioIntegrationHarness, flow *Flow, scenario string, input Input) string {
	t.Helper()
	flowID := "twilio-text-message-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

func waitForFlowOutput(t *testing.T, ctx context.Context, harness *twilioIntegrationHarness, flowID string) TextMessageDelivery {
	t.Helper()
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s", flowID)
	var delivery TextMessageDelivery
	require.NoError(t, result.DecodeSingleOutput(&delivery))
	return delivery
}

func waitForPhase(t *testing.T, ctx context.Context, harness *twilioIntegrationHarness, flow *Flow, flowID string, phase string) TextMessageDelivery {
	t.Helper()
	return waitForDelivery(t, ctx, harness, flow, flowID, func(delivery TextMessageDelivery) bool { return delivery.Phase == phase })
}

func waitForReconciliationNote(t *testing.T, ctx context.Context, harness *twilioIntegrationHarness, flow *Flow, flowID string, note string) TextMessageDelivery {
	t.Helper()
	return waitForDelivery(t, ctx, harness, flow, flowID, func(delivery TextMessageDelivery) bool {
		return delivery.Phase == PhaseNeedsReconciliation && delivery.ReconciliationNote == note
	})
}

func waitForDelivery(
	t *testing.T,
	ctx context.Context,
	harness *twilioIntegrationHarness,
	flow *Flow,
	flowID string,
	isExpected func(TextMessageDelivery) bool,
) TextMessageDelivery {
	t.Helper()
	var delivery TextMessageDelivery
	require.Eventually(t, func() bool {
		delivery = TextMessageDelivery{}
		return harness.client.InvokeRPC(ctx, flowID, flow.GetTextMessageDelivery, nil, &delivery) == nil && isExpected(delivery)
	}, 30*time.Second, 100*time.Millisecond, "Flow %s last delivery %+v", flowID, delivery)
	return delivery
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// fakeTwilio is a credential-safe Messages API fake whose behavior is chosen by the recipient number.
type fakeTwilio struct {
	*httptest.Server
	t                    *testing.T
	mutex                sync.Mutex
	createTimesRecipient map[string][]time.Time
	messages             map[string]*fakeMessage
	readsByMessageSID    map[string]int
}

type fakeMessage struct {
	to       string
	statuses []string
}

func newFakeTwilio(t *testing.T) *fakeTwilio {
	t.Helper()
	provider := &fakeTwilio{
		t: t, createTimesRecipient: map[string][]time.Time{}, messages: map[string]*fakeMessage{}, readsByMessageSID: map[string]int{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeTwilio) serveHTTP(response http.ResponseWriter, request *http.Request) {
	username, password, hasBasicAuth := request.BasicAuth()
	if !hasBasicAuth || username != integrationAccountSID || password != integrationAuthToken {
		provider.writeJSON(response, http.StatusUnauthorized, `{"code":20003,"message":"Authenticate","status":401}`)
		return
	}
	messagesPath := "/2010-04-01/Accounts/" + integrationAccountSID + "/Messages"
	switch {
	case request.Method == http.MethodPost && request.URL.Path == messagesPath+".json":
		provider.createMessage(response, request)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, messagesPath+"/"):
		provider.readMessage(response, strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, messagesPath+"/"), ".json"))
	default:
		provider.writeJSON(response, http.StatusNotFound, `{"code":20404,"message":"Not found","status":404}`)
	}
}

func (provider *fakeTwilio) createMessage(response http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil {
		provider.writeJSON(response, http.StatusBadRequest, `{"code":21601,"message":"Bad form","status":400}`)
		return
	}
	recipient := request.PostForm.Get("To")
	if request.PostForm.Get("From") != integrationSender || request.PostForm.Get("Body") == "" {
		provider.writeJSON(response, http.StatusBadRequest, `{"code":21603,"message":"Missing From or Body","status":400}`)
		return
	}
	provider.mutex.Lock()
	provider.createTimesRecipient[recipient] = append(provider.createTimesRecipient[recipient], time.Now())
	attempt := len(provider.createTimesRecipient[recipient])
	provider.mutex.Unlock()
	switch {
	case recipient == recipientRejected:
		provider.writeJSON(response, http.StatusBadRequest, `{"code":21608,"message":"SENTINEL unverified trial recipient","status":400}`)
	case recipient == recipientServerError && attempt == 1:
		provider.writeJSON(response, http.StatusInternalServerError, `{"code":20500,"message":"SENTINEL internal error","status":500}`)
	case recipient == recipientRateLimited && attempt == 1:
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"code":20429,"message":"SENTINEL too many requests","status":429}`)
	case recipient == recipientTimeout || recipient == recipientSlow:
		provider.storeMessage(recipient, attempt, []string{"delivered"})
		// Twilio created the message, but the response never arrives before the connector gives up.
		<-request.Context().Done()
	default:
		statuses := []string{"sent", "delivered"}
		if recipient == recipientUnconfirmed {
			statuses = []string{"sent"}
		}
		messageSID := provider.storeMessage(recipient, attempt, statuses)
		provider.writeJSON(response, http.StatusCreated, provider.messageJSON(messageSID, recipient, "queued"))
	}
}

func (provider *fakeTwilio) readMessage(response http.ResponseWriter, messageSID string) {
	provider.mutex.Lock()
	message, found := provider.messages[messageSID]
	var status string
	if found {
		provider.readsByMessageSID[messageSID]++
		reads := provider.readsByMessageSID[messageSID]
		status = message.statuses[min(reads, len(message.statuses))-1]
	}
	provider.mutex.Unlock()
	if !found {
		provider.writeJSON(response, http.StatusNotFound, `{"code":20404,"message":"SENTINEL not found","status":404}`)
		return
	}
	provider.writeJSON(response, http.StatusOK, provider.messageJSON(messageSID, message.to, status))
}

func (provider *fakeTwilio) storeMessage(recipient string, attempt int, statuses []string) string {
	messageSID := provider.messageSID(recipient, attempt)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.messages[messageSID] = &fakeMessage{to: recipient, statuses: statuses}
	return messageSID
}

func (provider *fakeTwilio) createUnrelatedMessage(recipient string) string {
	return provider.storeMessage(recipient, 99, []string{"delivered"})
}

func (provider *fakeTwilio) messageSID(recipient string, attempt int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", recipient, attempt)))
	return "SM" + hex.EncodeToString(digest[:16])
}

func (provider *fakeTwilio) messageJSON(messageSID string, recipient string, status string) string {
	return `{"sid":"` + messageSID + `","account_sid":"` + integrationAccountSID + `","messaging_service_sid":null,` +
		`"from":"` + integrationSender + `","to":"` + recipient + `","status":"` + status + `","direction":"outbound-api",` +
		`"body":"SENTINEL message body","error_code":null,"error_message":null,"num_segments":"1","num_media":"0",` +
		`"price":null,"price_unit":"USD","date_created":"Wed, 30 Sep 2026 17:04:05 +0000","date_sent":null,` +
		`"date_updated":"Wed, 30 Sep 2026 17:04:05 +0000"}`
}

func (provider *fakeTwilio) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		provider.t.Logf("fake Twilio response write failed: %v", err)
	}
}

func (provider *fakeTwilio) createCount(recipient string) int {
	return len(provider.createTimes(recipient))
}

func (provider *fakeTwilio) createTimes(recipient string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]time.Time(nil), provider.createTimesRecipient[recipient]...)
}

func (provider *fakeTwilio) readCount(messageSID string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.readsByMessageSID[messageSID]
}

type twilioIntegrationHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newTwilioIntegrationHarness(t *testing.T) (*fakeTwilio, *Flow, *twilioIntegrationHarness) {
	t.Helper()
	return newTwilioIntegrationHarnessWithRequestTimeout(t, shortRequestTimeout)
}

func newTwilioIntegrationHarnessWithRequestTimeout(t *testing.T, requestTimeout time.Duration) (*fakeTwilio, *Flow, *twilioIntegrationHarness) {
	t.Helper()
	provider := newFakeTwilio(t)
	reference := sdkgo.ConnectionRef{Provider: "twilio", Name: ConnectionName}
	providerClient, err := messaging.New(
		messaging.Config{AccountSID: integrationAccountSID, DefaultSender: integrationSender, Endpoint: provider.URL + "/2010-04-01"},
		sdkgo.StaticCredentialProvider[messaging.Credentials]{reference: {
			AuthMethodID: messaging.AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString(integrationAuthToken),
		}},
		messaging.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := messaging.NewConnection(providerClient, reference)
	require.NoError(t, err)
	policy := DeliveryStatusPolicy{CheckInterval: time.Second, MaximumChecks: integrationMaximumChecks}
	flow := NewFlow(connection, &policy)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &twilioIntegrationHarness{
		registry: registry, cache: cache, workerAddress: workerAddress,
		serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: harness.serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(stopCtx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return provider, flow, harness
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
