//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"html"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	answerduplicate "github.com/superdurable/dex-connectors-library/connectors/intercom/examples/answer-duplicate-conversation/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationCustomerEmail = "jane@acme.example.com"
	defaultRequestTimeout    = 5 * time.Second
	slowRequestTimeout       = 20 * time.Second
)

// duplicateScenario is one customer with a lead and a user record, an earlier open conversation
// started as the lead, the new inbound conversation started as the user, and two decoys.
type duplicateScenario struct {
	leadID, userID                string
	earlierID, inboundID          string
	closedID, otherCustomerOpenID string
}

func seedDuplicateScenario(provider *fakeIntercom) duplicateScenario {
	scenario := duplicateScenario{
		leadID: provider.seedContact(integrationCustomerEmail, "lead"),
		userID: provider.seedContact(integrationCustomerEmail, "user"),
	}
	otherCustomerID := provider.seedContact("ben@meridian.example.com", "user")
	scenario.closedID = provider.seedConversation(scenario.userID, "closed", 30*24*time.Hour, "An old question that was answered.")
	scenario.earlierID = provider.seedConversation(scenario.leadID, "open", 2*time.Hour, "I was charged twice for order 88213.")
	scenario.otherCustomerOpenID = provider.seedConversation(otherCustomerID, "open", time.Hour, "Password reset, please.")
	scenario.inboundID = provider.seedConversation(scenario.userID, "open", time.Minute, "Any update on my double charge?")
	return scenario
}

func TestDuplicateConversationIsAnsweredOnceAndClosedWithRealDex(t *testing.T) {
	provider := newFakeIntercom(t)
	scenario := seedDuplicateScenario(provider)
	harness := newDuplicateHarness(t, provider, defaultRequestTimeout, fakeAdminID)

	outcome := harness.runDuplicate(t, "answered", scenario.inboundID)
	require.Equal(t, answerduplicate.OutcomeAnsweredAndClosed, outcome.Action)
	require.Equal(t, scenario.earlierID, outcome.EarlierConversationID)
	require.Equal(t, intercom.ConversationStateClosed, outcome.ConversationState)
	require.False(t, outcome.WasReplyAlreadyApplied)
	require.False(t, outcome.WasCloseAlreadyApplied)

	inbound := provider.conversation(scenario.inboundID)
	require.Equal(t, "closed", inbound.state)
	replies := inbound.partsOfType("comment")
	require.Len(t, replies, 1, "exactly one reply reached the customer")
	require.Equal(t, replies[0].id, outcome.ReplyPartID)
	require.Equal(t, "<p>"+answerduplicateReplyHTML()+"</p>", replies[0].htmlBody)
	require.Len(t, inbound.partsOfType("close"), 1)
	for _, untouched := range []string{scenario.earlierID, scenario.closedID, scenario.otherCustomerOpenID} {
		require.Empty(t, provider.conversation(untouched).parts, "conversation %s must not be touched", untouched)
	}
	require.Equal(t, "open", provider.conversation(scenario.earlierID).state)

	require.JSONEq(t, `{"query":{"field":"email","operator":"=","value":"`+integrationCustomerEmail+`"},"pagination":{"per_page":15}}`,
		provider.lastRequest("contactSearch").body)
	require.JSONEq(t, `{"query":{"operator":"AND","value":[
		{"operator":"OR","value":[{"field":"state","operator":"=","value":"open"},{"field":"state","operator":"=","value":"snoozed"}]},
		{"operator":"OR","value":[{"field":"contact_ids","operator":"=","value":"`+scenario.leadID+`"},{"field":"contact_ids","operator":"=","value":"`+scenario.userID+`"}]}
	]},"pagination":{"per_page":20}}`, provider.lastRequest("conversationSearch").body)
	require.JSONEq(t, `{"message_type":"comment","type":"admin","admin_id":"`+fakeAdminID+`","body":"<p>`+answerduplicateReplyHTML()+`</p>"}`,
		provider.lastRequest("reply").body)
	require.JSONEq(t, `{"message_type":"close","type":"admin","admin_id":"`+fakeAdminID+`"}`, provider.lastRequest("manage").body)
	require.Equal(t, "/conversations/"+scenario.inboundID+"?display_as=plaintext", provider.lastRequest("read").path)
}

func TestConversationWithoutAnEarlierOneIsLeftOpenWithRealDex(t *testing.T) {
	provider := newFakeIntercom(t)
	userID := provider.seedContact(integrationCustomerEmail, "user")
	provider.seedConversation(userID, "closed", 30*24*time.Hour, "An old question that was answered.")
	inboundID := provider.seedConversation(userID, "open", time.Minute, "Hello, I have a question.")
	harness := newDuplicateHarness(t, provider, defaultRequestTimeout, fakeAdminID)

	outcome := harness.runDuplicate(t, "left-open", inboundID)
	require.Equal(t, answerduplicate.OutcomeLeftOpen, outcome.Action)
	require.Equal(t, "noEarlierConversation", outcome.Reason)
	require.Zero(t, provider.count("reply"))
	require.Zero(t, provider.count("manage"))
	require.Equal(t, "open", provider.conversation(inboundID).state)
}

func TestAnsweredConversationIsSkippedBeforeAnyLookupWithRealDex(t *testing.T) {
	provider := newFakeIntercom(t)
	scenario := seedDuplicateScenario(provider)
	provider.addAdminComment(scenario.inboundID, "A teammate already answered this.")
	harness := newDuplicateHarness(t, provider, defaultRequestTimeout, fakeAdminID)

	outcome := harness.runDuplicate(t, "already-answered", scenario.inboundID)
	require.Equal(t, answerduplicate.OutcomeSkipped, outcome.Action)
	require.Equal(t, "alreadyAnswered", outcome.Reason)
	require.Zero(t, provider.count("contactSearch"))
	require.Zero(t, provider.count("reply"))
}

// TestSlowReplyIsSentOnceWithRealDex is the duplicate-dispatch test: sync durability means Dex never
// dispatches a second reply attempt while the first is still in flight.
func TestSlowReplyIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeIntercom(t)
	scenario := seedDuplicateScenario(provider)
	provider.delaysFirstReply = true
	harness := newDuplicateHarness(t, provider, slowRequestTimeout, fakeAdminID)

	startedAt := time.Now()
	outcome := harness.runDuplicate(t, "slow-reply", scenario.inboundID)
	require.GreaterOrEqual(t, time.Since(startedAt), slowResponseDelay)
	require.Equal(t, answerduplicate.OutcomeAnsweredAndClosed, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("reply"), "no second dispatch while the first was in flight")
	require.Len(t, provider.conversation(scenario.inboundID).partsOfType("comment"), 1)
}

func TestLostReplyResponseIsFoundInTheConversationWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeIntercom(t)
	scenario := seedDuplicateScenario(provider)
	provider.losesFirstReplyResponse = true
	harness := newDuplicateHarness(t, provider, defaultRequestTimeout, fakeAdminID)

	outcome := harness.runDuplicate(t, "lost-reply", scenario.inboundID)
	require.Equal(t, answerduplicate.OutcomeAnsweredAndClosed, outcome.Action)
	require.True(t, outcome.WasReplyAlreadyApplied, "the retried attempt read the conversation and found its reply")
	replies := provider.conversation(scenario.inboundID).partsOfType("comment")
	require.Len(t, replies, 1)
	require.Equal(t, replies[0].id, outcome.ReplyPartID)
	require.Equal(t, 1, provider.count("reply"), "the reply was never resent")
}

func TestUnconfirmedReplyWithoutAMatchingPartNeedsReviewWithRealDex(t *testing.T) {
	provider := newFakeIntercom(t)
	scenario := seedDuplicateScenario(provider)
	provider.failsFirstReplyBeforeApplying = true
	harness := newDuplicateHarness(t, provider, defaultRequestTimeout, fakeAdminID)

	outcome := harness.runDuplicate(t, "unconfirmed-reply", scenario.inboundID)
	require.Equal(t, answerduplicate.OutcomeNeedsReview, outcome.Action)
	require.Equal(t, "replyUncertain", outcome.Reason)
	require.Equal(t, "an earlier attempt of this Step sent the reply without a confirmed outcome, and the conversation shows no matching part, so it is not sent again",
		outcome.ReviewDetail)
	require.Equal(t, 1, provider.count("reply"), "an unconfirmed reply is never resent")
	require.Zero(t, provider.count("manage"), "an unanswered duplicate is not closed")
	require.Equal(t, "open", provider.conversation(scenario.inboundID).state)
}

// TestLostWorkerDuringReplyReconcilesWithoutResendingWithRealDex replaces the Worker while Intercom holds
// the reply; the next attempt finds the dispatch checkpoint, reads the conversation, and sends nothing.
func TestLostWorkerDuringReplyReconcilesWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeIntercom(t)
	scenario := seedDuplicateScenario(provider)
	provider.holdsFirstReply = make(chan struct{})
	harness := newDuplicateHarness(t, provider, slowRequestTimeout, fakeAdminID)
	flowID := harness.startDuplicate(t, "lost-worker", scenario.inboundID)
	require.Eventually(t, func() bool { return provider.count("reply") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach Intercom")
	harness.replaceWorker(t)

	result := harness.waitForFlow(t, flowID)
	close(provider.holdsFirstReply)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome answerduplicate.DuplicateConversationOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, answerduplicate.OutcomeNeedsReview, outcome.Action, "Intercom had not applied the held reply when the new attempt read the conversation")
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("reply"), "the attempt on the new Worker did not resend the reply")
	require.GreaterOrEqual(t, provider.count("read"), 2, "the new attempt read the conversation back")
	require.Len(t, provider.conversation(scenario.inboundID).partsOfType("comment"), 1, "the held reply exists, which is why the outcome is uncertain")
}

// TestSlowCloseIsSafeToRepeatWithRealDex lets async Dex dispatch the close again; every attempt converges on closed.
func TestSlowCloseIsSafeToRepeatWithRealDex(t *testing.T) {
	for _, isRedundantCloseRejected := range []bool{false, true} {
		t.Run("redundantCloseRejected="+strconv.FormatBool(isRedundantCloseRejected), func(t *testing.T) {
			provider := newFakeIntercom(t)
			scenario := seedDuplicateScenario(provider)
			provider.delaysFirstClose, provider.rejectsRedundantClose = true, isRedundantCloseRejected
			harness := newDuplicateHarness(t, provider, slowRequestTimeout, fakeAdminID)

			outcome := harness.runDuplicate(t, "slow-close", scenario.inboundID)
			require.Equal(t, answerduplicate.OutcomeAnsweredAndClosed, outcome.Action)
			provider.waitForDelayedRequests(t)
			require.GreaterOrEqual(t, provider.count("manage"), 2, "Dex dispatched the close again past its local phase")
			inbound := provider.conversation(scenario.inboundID)
			require.Equal(t, "closed", inbound.state)
			require.Len(t, inbound.partsOfType("comment"), 1, "the close never repeats the reply")
			if isRedundantCloseRejected {
				require.Len(t, inbound.partsOfType("close"), 1)
				require.Equal(t, 1, provider.count("redundantManage"), "the delayed first close found the conversation closed")
			}
			t.Logf("slow close: manage=%d applied=%d redundant=%d reads=%d",
				provider.count("manage"), provider.count("appliedManage"), provider.count("redundantManage"), provider.count("read"))
		})
	}
}

func TestRateLimitedSearchWaitsForTheResetWithRealDex(t *testing.T) {
	provider := newFakeIntercom(t)
	scenario := seedDuplicateScenario(provider)
	provider.rateLimitsFirstSearch = true
	harness := newDuplicateHarness(t, provider, defaultRequestTimeout, fakeAdminID)

	outcome := harness.runDuplicate(t, "rate-limited", scenario.inboundID)
	require.Equal(t, answerduplicate.OutcomeAnsweredAndClosed, outcome.Action)
	times := provider.requestTimes("conversationSearch")
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), time.Second, "the retry waited for X-RateLimit-Reset")
}

func TestRejectedReplyFailsTheFlowWithoutIntercomTextWithRealDex(t *testing.T) {
	provider := newFakeIntercom(t)
	scenario := seedDuplicateScenario(provider)
	provider.rejectsReply = true
	harness := newDuplicateHarness(t, provider, defaultRequestTimeout, fakeAdminID)
	flowID := harness.startDuplicate(t, "rejected", scenario.inboundID)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.Contains(t, result.ErrorMessage, `"providerRejected"`)
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	require.NotContains(t, result.ErrorMessage, fakeAccessToken)
	require.Equal(t, 1, provider.count("reply"), "a conclusive rejection is not retried")
	require.Zero(t, provider.count("manage"))
	t.Logf("rejected reply failure: %s", result.ErrorMessage)
}

func TestUnconfiguredAdminFailsBeforeCallingIntercomWithRealDex(t *testing.T) {
	provider := newFakeIntercom(t)
	scenario := seedDuplicateScenario(provider)
	harness := newDuplicateHarness(t, provider, defaultRequestTimeout, "")
	flowID := harness.startDuplicate(t, "unconfigured", scenario.inboundID)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "adminPicker")
	require.Zero(t, provider.totalRequests())
}

// TestSignedNewConversationStartsOneFlowAndOtherDeliveriesStartNoneWithRealDex serves the example's inbound
// target on the connection's endpoint: README steps 4 and 5.
func TestSignedNewConversationStartsOneFlowAndOtherDeliveriesStartNoneWithRealDex(t *testing.T) {
	provider := newFakeIntercom(t)
	scenario := seedDuplicateScenario(provider)
	harness := newDuplicateHarness(t, provider, defaultRequestTimeout, fakeAdminID)
	logs := newRecordedLogs(t)
	endpoint := newInboundEndpoint(t, provider, newExampleConnection(t, provider, intercom.WithLogger(logs.logger())),
		newInboundTarget(harness.client, harness.flow, logs.logger()))
	endpoint.start(t)
	client := newInspectionClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	require.Equal(t, http.StatusOK, endpoint.postNotification(t, http.MethodHead, nil, ""), "Intercom validates the URL with HEAD")
	notificationID := "notif_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	body, signature := provider.signedNotification(notificationID, intercom.TopicConversationUserCreated, scenario.inboundID, fakeClientSecret)
	require.Equal(t, http.StatusOK, endpoint.postNotification(t, http.MethodPost, body, signature))
	outcome := waitForOutcome(t, ctx, client, scenario.inboundID)
	require.Equal(t, answerduplicate.OutcomeAnsweredAndClosed, outcome.Action)
	require.Equal(t, scenario.earlierID, outcome.EarlierConversationID)

	// Intercom redelivers the same notification: the Flow start deduplicates it.
	require.Equal(t, http.StatusOK, endpoint.postNotification(t, http.MethodPost, body, signature))
	require.Eventually(t, func() bool {
		return len(logs.find("trigger event delivered", map[string]string{"event_id": notificationID, "duplicate": "true"})) == 1
	}, 20*time.Second, 25*time.Millisecond, "the redelivery reaches Dex as a duplicate start")
	require.Equal(t, 1, provider.count("reply"), "the Flow replied once")

	// A forged notification, a ping, and a filtered topic start nothing.
	forgedBody, _ := provider.signedNotification(notificationID+"-forged", intercom.TopicConversationUserCreated, scenario.otherCustomerOpenID, fakeClientSecret)
	require.Equal(t, http.StatusBadRequest, endpoint.postNotification(t, http.MethodPost, forgedBody, hubSignature(forgedBody, "another-secret")))
	ping := []byte(`{"type":"notification_event","app_id":"` + fakeWorkspaceID + `","id":"` + notificationID + `-ping","topic":"ping","data":{"type":"notification_event_data","item":{"type":"ping","message":"something"}},"created_at":1}`)
	require.Equal(t, http.StatusOK, endpoint.postNotification(t, http.MethodPost, ping, hubSignature(ping, fakeClientSecret)))
	repliedBody, repliedSignature := provider.signedNotification(notificationID+"-replied", intercom.TopicConversationAdminReplied, scenario.otherCustomerOpenID, fakeClientSecret)
	require.Equal(t, http.StatusOK, endpoint.postNotification(t, http.MethodPost, repliedBody, repliedSignature))
	requireNoFlow(t, ctx, client, scenario.otherCustomerOpenID)
	require.NotContains(t, logs.text(), fakeClientSecret)
	require.NotContains(t, logs.text(), fakeAccessToken)
}

// answerduplicateReplyHTML is the reply as the connector sends it: one escaped paragraph.
func answerduplicateReplyHTML() string {
	return html.EscapeString(answerduplicate.DuplicateReply)
}

func dexAddress() string {
	return environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
}

// waitForOutcome waits for the conversation's Flow, which the Trigger starts asynchronously after the 200.
func waitForOutcome(t *testing.T, ctx context.Context, client *dex.Client, conversationID string) answerduplicate.DuplicateConversationOutcome {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for {
		result, err := client.WaitForFlow(waitCtx, answerduplicate.FlowIDPrefix+conversationID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		if err == nil {
			require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
			var outcome answerduplicate.DuplicateConversationOutcome
			require.NoError(t, result.DecodeSingleOutput(&outcome))
			return outcome
		}
		var notFound *dex.FlowNotFoundError
		require.True(t, errors.As(err, &notFound) && waitCtx.Err() == nil, "the conversation's Flow must complete: %v", err)
		time.Sleep(50 * time.Millisecond) // The Flow start is asynchronous after the 200, so it may not exist yet.
	}
}

func requireNoFlow(t *testing.T, ctx context.Context, client *dex.Client, conversationID string) {
	t.Helper()
	_, err := client.WaitForFlow(ctx, answerduplicate.FlowIDPrefix+conversationID, dex.WaitForFlowOptions{})
	var notFound *dex.FlowNotFoundError
	require.ErrorAs(t, err, &notFound, "conversation %s must not start a Flow", conversationID)
}

// newInspectionClient waits for and inspects Flows; it registers no Worker.
func newInspectionClient(t *testing.T) *dex.Client {
	t.Helper()
	providerClient, err := intercom.New(intercom.Config{}, sdkgo.StaticCredentialProvider[intercom.Credentials]{})
	require.NoError(t, err)
	connection, err := intercom.NewConnection(providerClient, sdkgo.ConnectionRef{Provider: "intercom", Name: answerduplicate.ConnectionName})
	require.NoError(t, err)
	registry, err := dex.NewRegistry([]dex.Flow{answerduplicate.NewFlow(connection, sdkgo.ConnectorLoadedConfiguration[answerduplicate.ReplyConfiguration]{})})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "inspection-blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: dexAddress()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, errors.Join(client.Close(), cache.Close())) })
	return client
}

// duplicateHarness runs the Flow on a real Worker against the fake, with a replaceable Worker.
type duplicateHarness struct {
	flow          *answerduplicate.Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newDuplicateHarness(t *testing.T, provider *fakeIntercom, requestTimeout time.Duration, adminID string) *duplicateHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "intercom", Name: answerduplicate.ConnectionName}
	providerClient, err := intercom.New(intercom.Config{},
		sdkgo.StaticCredentialProvider[intercom.Credentials]{reference: {AccessToken: sdkgo.NewSecretString(fakeAccessToken)}},
		intercom.WithAPIBaseURL(provider.URL), intercom.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := intercom.NewConnection(providerClient, reference)
	require.NoError(t, err)
	configuration := sdkgo.ConnectorLoadedConfiguration[answerduplicate.ReplyConfiguration]{
		Reference: answerduplicate.ReplyConfigurationRef(), Value: answerduplicate.ReplyConfiguration{AdminID: adminID},
	}
	harness := &duplicateHarness{flow: answerduplicate.NewFlow(connection, configuration), serverAddress: dexAddress()}
	harness.registry, err = dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.workerAddress = net.JoinHostPort("127.0.0.1", unusedPort(t))
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

func (harness *duplicateHarness) startWorker(t *testing.T) {
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
func (harness *duplicateHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *duplicateHarness) runDuplicate(t *testing.T, scenario string, conversationID string) answerduplicate.DuplicateConversationOutcome {
	t.Helper()
	flowID := harness.startDuplicate(t, scenario, conversationID)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome answerduplicate.DuplicateConversationOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

func (harness *duplicateHarness) startDuplicate(t *testing.T, scenario string, conversationID string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "intercom-duplicate-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, answerduplicate.InboundConversation{ConversationID: conversationID}, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *duplicateHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
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
