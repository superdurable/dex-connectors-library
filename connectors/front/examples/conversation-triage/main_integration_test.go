//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/front"
	conversationtriage "github.com/superdurable/dex-connectors-library/connectors/front/examples/conversation-triage/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	requesterEmail        = "jane@acme.example.com"
	defaultRequestTimeout = 5 * time.Second
	slowRequestTimeout    = 20 * time.Second
)

// triageScenario seeds the triaged conversation, one other open conversation, and three decoys the search must skip.
type triageScenario struct {
	contactID, triagedID, otherOpenID string
	archivedID, otherInboxID, decoyID string
}

func seedTriageScenario(provider *fakeFront) triageScenario {
	scenario := triageScenario{contactID: provider.seedContact(requesterEmail)}
	scenario.archivedID = provider.seedConversation(requesterEmail, fakeInboxID, "archived", 30*24*time.Hour, "An old question that was answered.")
	scenario.otherOpenID = provider.seedConversation(requesterEmail, fakeInboxID, "unassigned", 2*time.Hour, "I was charged twice for order 88213.")
	scenario.otherInboxID = provider.seedConversation(requesterEmail, "inb_sales", "unassigned", 3*time.Hour, "Pricing question.")
	scenario.decoyID = provider.seedConversation("ben@meridian.example.com", fakeInboxID, "unassigned", time.Hour, "Password reset, please.")
	scenario.triagedID = provider.seedConversation(requesterEmail, fakeInboxID, "unassigned", time.Minute, "Any update on my double charge?")
	return scenario
}

func TestConversationIsTriagedOnceWithRealDex(t *testing.T) {
	provider := newFakeFront(t)
	scenario := seedTriageScenario(provider)
	harness := newTriageHarness(t, provider, defaultRequestTimeout, conversationtriage.RoutingConfiguration{AssigneeID: fakeTeammateID, TagID: fakeTriageTagID})

	triage := harness.runTriage(t, "triaged", scenario.triagedID)
	require.Equal(t, "completed", triage.Stage)
	require.Equal(t, requesterEmail, triage.RequesterEmail)
	require.Equal(t, scenario.contactID, triage.ContactID)
	require.Equal(t, []string{scenario.otherOpenID}, triage.OtherOpenConversationIDs)
	require.Equal(t, fakeTeammateID, triage.AssigneeID)
	require.Equal(t, []string{fakeTriageTagID}, triage.TagIDs)
	require.Equal(t, front.ConversationStatusAssigned, triage.Status)

	triaged := provider.conversation(scenario.triagedID)
	require.Len(t, triaged.comments, 1, "exactly one triage comment")
	require.Equal(t, triaged.comments[0].id, triage.CommentID)
	require.Equal(t, conversationtriage.BuildTriageComment(requesterEmail, scenario.contactID, []string{scenario.otherOpenID}, false), triaged.comments[0].body)
	require.Equal(t, fakeTeammateID, triaged.assigneeID)
	require.Equal(t, []string{fakeTriageTagID}, triaged.tagIDs)
	for _, untouched := range []string{scenario.otherOpenID, scenario.archivedID, scenario.otherInboxID, scenario.decoyID} {
		conversation := provider.conversation(untouched)
		require.Empty(t, conversation.comments, "conversation %s must not be touched", untouched)
		require.Empty(t, conversation.tagIDs, "conversation %s must not be touched", untouched)
		require.Empty(t, conversation.assigneeID, "conversation %s must not be touched", untouched)
	}
	require.Equal(t, "/conversations/search/inbox%3Ainb_support%20is%3Aopen%20recipient%3Ajane%40acme.example.com?limit=11",
		provider.lastRequest("search").path)
	require.Equal(t, "/contacts/alt:email:jane@acme.example.com", provider.lastRequest("contact").path)
	require.Equal(t, "/conversations/"+scenario.triagedID+"/messages?limit=5", provider.lastRequest("messages").path)
	require.JSONEq(t, `{"assignee_id":"`+fakeTeammateID+`"}`, provider.lastRequest("patch").body)
	require.JSONEq(t, `{"tag_ids":["`+fakeTriageTagID+`"]}`, provider.lastRequest("tagAdd").body)
	require.Zero(t, provider.count("reply"), "the Flow never writes to the customer")
}

func TestTriagedConversationIsSkippedBeforeAnyLookupWithRealDex(t *testing.T) {
	provider := newFakeFront(t)
	scenario := seedTriageScenario(provider)
	provider.seedComment(scenario.triagedID, conversationtriage.TriageCommentPrefix+" an earlier Flow triaged this conversation.")
	harness := newTriageHarness(t, provider, defaultRequestTimeout, conversationtriage.RoutingConfiguration{TagID: fakeTriageTagID})

	triage := harness.runTriage(t, "skipped", scenario.triagedID)
	require.Equal(t, "skipped", triage.Stage)
	require.Zero(t, provider.count("contact"))
	require.Zero(t, provider.count("comment"))
	require.Zero(t, provider.count("tagAdd"))
}

// TestSlowTriageCommentIsSentOnceWithRealDex shows sync durability: no second dispatch while the first is in flight.
func TestSlowTriageCommentIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeFront(t)
	scenario := seedTriageScenario(provider)
	provider.shouldDelayFirstComment = true
	harness := newTriageHarness(t, provider, slowRequestTimeout, conversationtriage.RoutingConfiguration{TagID: fakeTriageTagID})

	startedAt := time.Now()
	triage := harness.runTriage(t, "slow-comment", scenario.triagedID)
	require.GreaterOrEqual(t, time.Since(startedAt), slowResponseDelay)
	require.Equal(t, "completed", triage.Stage)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("comment"), "no second dispatch while the first was in flight")
	require.Len(t, provider.conversation(scenario.triagedID).comments, 1)
}

// TestLostWorkerDuringCommentNeedsReviewWithoutResendingWithRealDex replaces the Worker after Front received the comment.
func TestLostWorkerDuringCommentNeedsReviewWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeFront(t)
	scenario := seedTriageScenario(provider)
	provider.holdsFirstComment = make(chan struct{})
	harness := newTriageHarness(t, provider, slowRequestTimeout, conversationtriage.RoutingConfiguration{TagID: fakeTriageTagID})
	flowID := harness.startTriage(t, "lost-worker", scenario.triagedID)
	require.Eventually(t, func() bool { return provider.count("comment") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach Front")
	harness.replaceWorker(t)

	result := harness.waitForFlow(t, flowID)
	close(provider.holdsFirstComment)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var triage conversationtriage.Triage
	require.NoError(t, result.DecodeSingleOutput(&triage))
	require.Equal(t, "needsReview", triage.Stage)
	require.Equal(t, sdkgo.UncertainBranchID, triage.FailedBranch)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("comment"), "the attempt on the new Worker did not send the comment again")
	require.Len(t, provider.conversation(scenario.triagedID).comments, 1, "the held comment exists, which is why the outcome needs review")
	require.Zero(t, provider.count("tagAdd"), "an unconfirmed comment is not followed by routing")
}

// TestSlowTagWriteIsSafeToRepeatWithRealDex lets async Dex dispatch the update again; both attempts converge.
func TestSlowTagWriteIsSafeToRepeatWithRealDex(t *testing.T) {
	provider := newFakeFront(t)
	scenario := seedTriageScenario(provider)
	provider.shouldDelayFirstTagWrite = true
	harness := newTriageHarness(t, provider, slowRequestTimeout, conversationtriage.RoutingConfiguration{AssigneeID: fakeTeammateID, TagID: fakeTriageTagID})

	triage := harness.runTriage(t, "slow-tag", scenario.triagedID)
	require.Equal(t, "completed", triage.Stage)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("tagAdd"), 2, "Dex dispatched the update again past its local phase")
	triaged := provider.conversation(scenario.triagedID)
	require.Equal(t, []string{fakeTriageTagID}, triaged.tagIDs, "the tag is applied once")
	require.Equal(t, fakeTeammateID, triaged.assigneeID)
	require.Len(t, triaged.comments, 1, "the update never repeats the comment")
	t.Logf("slow tag write: tagAdd=%d patch=%d reads=%d", provider.count("tagAdd"), provider.count("patch"), provider.count("read"))
}

func TestRateLimitedSearchWaitsForRetryAfterWithRealDex(t *testing.T) {
	provider := newFakeFront(t)
	scenario := seedTriageScenario(provider)
	provider.shouldRateLimitFirstSearch = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout, conversationtriage.RoutingConfiguration{TagID: fakeTriageTagID})

	triage := harness.runTriage(t, "rate-limited", scenario.triagedID)
	require.Equal(t, "completed", triage.Stage)
	times := provider.requestTimes("search")
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), time.Second, "the retry waited for Retry-After")
}

func TestRejectedCommentFailsTheFlowWithoutFrontTextWithRealDex(t *testing.T) {
	provider := newFakeFront(t)
	scenario := seedTriageScenario(provider)
	provider.shouldRejectComment = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout, conversationtriage.RoutingConfiguration{TagID: fakeTriageTagID})
	flowID := harness.startTriage(t, "rejected", scenario.triagedID)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.Contains(t, result.ErrorMessage, `"providerRejected"`)
	require.NotContains(t, result.ErrorMessage, providerSentinel)
	require.NotContains(t, result.ErrorMessage, fakeAPIToken)
	require.Equal(t, 1, provider.count("comment"), "a conclusive rejection is not retried")
	require.Zero(t, provider.count("tagAdd"))
	t.Logf("rejected comment failure: %s", result.ErrorMessage)
}

func TestUnconfiguredTagFailsBeforeCallingFrontWithRealDex(t *testing.T) {
	provider := newFakeFront(t)
	scenario := seedTriageScenario(provider)
	harness := newTriageHarness(t, provider, defaultRequestTimeout, conversationtriage.RoutingConfiguration{})
	flowID := harness.startTriage(t, "unconfigured", scenario.triagedID)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "tagPicker")
	require.Zero(t, provider.totalRequests())
}

func dexAddress() string {
	return environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
}

// triageHarness runs the Flow on a real Worker against the fake, with a replaceable Worker.
type triageHarness struct {
	flow          *conversationtriage.Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

// newTriageHarness builds the connection Dex Web saves against the fake; a static credential replaces project storage.
func newTriageHarness(
	t *testing.T, provider *fakeFront, requestTimeout time.Duration, routing conversationtriage.RoutingConfiguration,
) *triageHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "front", Name: conversationtriage.ConnectionName}
	providerClient, err := front.New(front.Config{},
		sdkgo.StaticCredentialProvider[front.Credentials]{reference: {APIToken: sdkgo.NewSecretString(fakeAPIToken)}},
		front.WithAPIBaseURL(provider.URL), front.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := front.NewConnection(providerClient, reference)
	require.NoError(t, err)
	search := sdkgo.ConnectorLoadedConfiguration[conversationtriage.SearchConfiguration]{
		Reference: conversationtriage.SearchConfigurationRef(), Value: conversationtriage.SearchConfiguration{InboxID: fakeInboxID},
	}
	loadedRouting := sdkgo.ConnectorLoadedConfiguration[conversationtriage.RoutingConfiguration]{
		Reference: conversationtriage.RoutingConfigurationRef(), Value: routing,
	}
	harness := &triageHarness{flow: conversationtriage.NewFlow(connection, search, loadedRouting), serverAddress: dexAddress()}
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

func (harness *triageHarness) startWorker(t *testing.T) {
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
func (harness *triageHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *triageHarness) runTriage(t *testing.T, scenario string, conversationID string) conversationtriage.Triage {
	t.Helper()
	flowID := harness.startTriage(t, scenario, conversationID)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var triage conversationtriage.Triage
	require.NoError(t, result.DecodeSingleOutput(&triage))
	return triage
}

func (harness *triageHarness) startTriage(t *testing.T, scenario string, conversationID string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "front-triage-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, conversationtriage.TriageRequest{ConversationID: conversationID}, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *triageHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
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

func unusedPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	require.NoError(t, listener.Close())
	return port
}
