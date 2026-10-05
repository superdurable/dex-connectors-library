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
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	escalateblocked "github.com/superdurable/dex-connectors-library/connectors/clickup/examples/escalate-blocked-task/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	defaultRequestTimeout = 5 * time.Second
	slowRequestTimeout    = 20 * time.Second
	blockedTaskName       = "Fix login for SSO customers"
)

func TestBlockedTaskIsEscalatedOnceWithRealDex(t *testing.T) {
	provider := newFakeClickUp(t)
	blockedID := provider.seedTask(blockedTaskName, "blocked", fakeEngineeringID, "customer")
	decoyID := provider.seedTask("Escalation: another task [86b2x1]", "open", fakeEscalationID, escalateblocked.EscalationTag)
	harness := newEscalationHarness(t, provider, defaultRequestTimeout, exampleSettings)

	outcome := harness.runEscalation(t, "escalated", blockedID)
	require.Equal(t, escalateblocked.OutcomeEscalated, outcome.Action)
	require.False(t, outcome.WasExistingEscalationFound)
	require.False(t, outcome.WasCreateAlreadyApplied)
	require.False(t, outcome.WasCommentAlreadyApplied)
	require.Equal(t, fakeManagerID, outcome.ManagerID)
	require.ElementsMatch(t, []string{"customer", escalateblocked.EscalatedTag}, outcome.Tags)

	escalations := provider.tasksInList(fakeEscalationID)
	require.Len(t, escalations, 2, "one escalation task beside the decoy")
	escalation := provider.task(outcome.EscalationTaskID)
	require.NotEqual(t, decoyID, escalation.id)
	require.Equal(t, "Escalation: "+blockedTaskName+" ["+blockedID+"]", escalation.name)
	require.Equal(t, []int64{fakeManagerID}, escalation.assignees)
	require.Equal(t, []string{escalateblocked.EscalationTag}, escalation.tags)
	require.Equal(t, 1, escalation.priority)
	require.Equal(t, "https://app.clickup.com/t/"+escalation.id, outcome.EscalationTaskURL)

	blocked := provider.task(blockedID)
	require.Equal(t, []int64{fakeManagerID}, blocked.assignees)
	require.Equal(t, 1, blocked.priority)
	require.Equal(t, "blocked", blocked.status, "the Flow never changes the status")
	comments := provider.commentsOf(blockedID)
	require.Len(t, comments, 1)
	require.Equal(t, "Escalated to "+fakeManagerName+": https://app.clickup.com/t/"+escalation.id, comments[0].text)
	require.Equal(t, comments[0].id, outcome.CommentID)
	require.Empty(t, provider.commentsOf(decoyID))

	require.JSONEq(t, `{"name":"Escalation: `+blockedTaskName+` [`+blockedID+`]",
		"markdown_content":"[The blocked task](https://app.clickup.com/t/`+blockedID+`) moved to the **blocked** status and needs a decision.",
		"priority":1,"assignees":[`+strconv.FormatInt(fakeManagerID, 10)+`],"tags":["escalation"]}`, provider.lastRequestBody("create"))
	require.JSONEq(t, `{"priority":1,"assignees":{"add":[`+strconv.FormatInt(fakeManagerID, 10)+`],"rem":[]}}`, provider.lastRequestBody("update"))
	require.Equal(t, 1, provider.count("create"))
	require.Equal(t, 1, provider.count("comment"))
}

func TestExistingEscalationIsReusedWithRealDex(t *testing.T) {
	provider := newFakeClickUp(t)
	blockedID := provider.seedTask(blockedTaskName, "Blocked", fakeEngineeringID)
	existingID := provider.seedTask("Escalation: an older title ["+blockedID+"]", "closed", fakeEscalationID, escalateblocked.EscalationTag)
	harness := newEscalationHarness(t, provider, defaultRequestTimeout, exampleSettings)

	outcome := harness.runEscalation(t, "existing", blockedID)
	require.Equal(t, escalateblocked.OutcomeEscalated, outcome.Action, "the status matches without regard to case")
	require.True(t, outcome.WasExistingEscalationFound)
	require.Equal(t, existingID, outcome.EscalationTaskID)
	require.Zero(t, provider.count("create"), "an escalation task filed earlier, even a closed one, is reused")
	require.Len(t, provider.tasksInList(fakeEscalationID), 1)
	require.Len(t, provider.commentsOf(blockedID), 1)
}

func TestTaskNoLongerBlockedIsSkippedWithRealDex(t *testing.T) {
	provider := newFakeClickUp(t)
	blockedID := provider.seedTask(blockedTaskName, "in progress", fakeEngineeringID)
	harness := newEscalationHarness(t, provider, defaultRequestTimeout, exampleSettings)

	outcome := harness.runEscalation(t, "not-blocked", blockedID)
	require.Equal(t, escalateblocked.OutcomeSkipped, outcome.Action)
	require.Equal(t, "notBlocked", outcome.Reason)
	require.Equal(t, 1, provider.totalRequests(), "only the task was read")
}

func TestUnknownManagerIsSkippedWithRealDex(t *testing.T) {
	provider := newFakeClickUp(t)
	blockedID := provider.seedTask(blockedTaskName, "blocked", fakeEngineeringID)
	settings := exampleSettings
	settings.ManagerEmail = "contractor@vendor.example.com"
	harness := newEscalationHarness(t, provider, defaultRequestTimeout, settings)

	outcome := harness.runEscalation(t, "no-manager", blockedID)
	require.Equal(t, escalateblocked.OutcomeSkipped, outcome.Action)
	require.Equal(t, "managerNotFound", outcome.Reason)
	require.Zero(t, provider.count("search"))
	require.Zero(t, provider.count("create"))
}

// TestSlowCreateIsSentOnceWithRealDex is the duplicate-dispatch test: sync durability means Dex never
// dispatches a second create while the first is still in flight.
func TestSlowCreateIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeClickUp(t)
	blockedID := provider.seedTask(blockedTaskName, "blocked", fakeEngineeringID)
	provider.delaysFirstCreate = true
	harness := newEscalationHarness(t, provider, slowRequestTimeout, exampleSettings)

	startedAt := time.Now()
	outcome := harness.runEscalation(t, "slow-create", blockedID)
	require.GreaterOrEqual(t, time.Since(startedAt), slowResponseDelay)
	require.Equal(t, escalateblocked.OutcomeEscalated, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("create"), "no second dispatch while the first was in flight")
	require.Len(t, provider.tasksInList(fakeEscalationID), 1)
}

func TestLostCreateResponseIsFoundInTheListWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeClickUp(t)
	blockedID := provider.seedTask(blockedTaskName, "blocked", fakeEngineeringID)
	provider.losesFirstCreateResponse = true
	harness := newEscalationHarness(t, provider, defaultRequestTimeout, exampleSettings)

	outcome := harness.runEscalation(t, "lost-create", blockedID)
	require.Equal(t, escalateblocked.OutcomeEscalated, outcome.Action)
	require.True(t, outcome.WasCreateAlreadyApplied, "the retried attempt read the List and found its task")
	require.Equal(t, 1, provider.count("create"), "the create was never resent")
	require.Equal(t, 1, provider.count("listRead"))
	escalations := provider.tasksInList(fakeEscalationID)
	require.Len(t, escalations, 1)
	require.Equal(t, escalations[0].id, outcome.EscalationTaskID)
}

func TestUnconfirmedCreateWithoutATaskNeedsReviewWithRealDex(t *testing.T) {
	provider := newFakeClickUp(t)
	blockedID := provider.seedTask(blockedTaskName, "blocked", fakeEngineeringID)
	provider.failsFirstCreateBeforeApplying = true
	harness := newEscalationHarness(t, provider, defaultRequestTimeout, exampleSettings)

	outcome := harness.runEscalation(t, "unconfirmed-create", blockedID)
	require.Equal(t, escalateblocked.OutcomeNeedsReview, outcome.Action)
	require.Equal(t, "createUncertain", outcome.Reason)
	require.Equal(t, "an earlier attempt of this Step sent the create without a confirmed outcome, and the List shows 0 tasks with the name created since, so it is not sent again",
		outcome.ReviewDetail)
	require.Equal(t, 1, provider.count("create"), "an unconfirmed create is never resent")
	require.Zero(t, provider.count("tag")+provider.count("update")+provider.count("comment"), "the blocked task is untouched")
}

// TestLostWorkerDuringCreateReconcilesWithoutResendingWithRealDex replaces the Worker while ClickUp holds
// the create; the next attempt finds the dispatch checkpoint, reads the List, and sends nothing.
func TestLostWorkerDuringCreateReconcilesWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeClickUp(t)
	blockedID := provider.seedTask(blockedTaskName, "blocked", fakeEngineeringID)
	provider.holdsFirstCreate = make(chan struct{})
	harness := newEscalationHarness(t, provider, slowRequestTimeout, exampleSettings)
	flowID := harness.startEscalation(t, "lost-worker", blockedID)
	require.Eventually(t, func() bool { return provider.count("create") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach ClickUp")
	harness.replaceWorker(t)

	result := harness.waitForFlow(t, flowID)
	close(provider.holdsFirstCreate)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome escalateblocked.EscalationOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, escalateblocked.OutcomeNeedsReview, outcome.Action, "ClickUp had not applied the held create when the new attempt read the List")
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("create"), "the attempt on the new Worker did not resend the create")
	require.GreaterOrEqual(t, provider.count("listRead"), 1, "the new attempt read the List back")
	require.Len(t, provider.tasksInList(fakeEscalationID), 1, "the held create exists, which is why the outcome is uncertain")
}

// TestSlowUpdateIsSafeToRepeatWithRealDex lets async Dex dispatch the update again; both converge on one assignment.
func TestSlowUpdateIsSafeToRepeatWithRealDex(t *testing.T) {
	provider := newFakeClickUp(t)
	blockedID := provider.seedTask(blockedTaskName, "blocked", fakeEngineeringID)
	provider.delaysFirstUpdate = true
	harness := newEscalationHarness(t, provider, slowRequestTimeout, exampleSettings)

	outcome := harness.runEscalation(t, "slow-update", blockedID)
	require.Equal(t, escalateblocked.OutcomeEscalated, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("update"), 2, "Dex dispatched the update again past its local phase")
	blocked := provider.task(blockedID)
	require.Equal(t, []int64{fakeManagerID}, blocked.assignees, "a repeated add leaves one assignment")
	require.Equal(t, 1, blocked.priority)
	require.Len(t, provider.commentsOf(blockedID), 1, "the update never repeats the comment")
	t.Logf("slow update: updates=%d", provider.count("update"))
}

func TestSlowCommentIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeClickUp(t)
	blockedID := provider.seedTask(blockedTaskName, "blocked", fakeEngineeringID)
	provider.delaysFirstComment = true
	harness := newEscalationHarness(t, provider, slowRequestTimeout, exampleSettings)

	outcome := harness.runEscalation(t, "slow-comment", blockedID)
	require.Equal(t, escalateblocked.OutcomeEscalated, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("comment"), "no second dispatch while the first was in flight")
	require.Len(t, provider.commentsOf(blockedID), 1)
}

func TestRateLimitedSearchWaitsForTheResetWithRealDex(t *testing.T) {
	provider := newFakeClickUp(t)
	blockedID := provider.seedTask(blockedTaskName, "blocked", fakeEngineeringID)
	provider.rateLimitsFirstSearch = true
	harness := newEscalationHarness(t, provider, defaultRequestTimeout, exampleSettings)

	outcome := harness.runEscalation(t, "rate-limited", blockedID)
	require.Equal(t, escalateblocked.OutcomeEscalated, outcome.Action)
	times := provider.requestTimes("search")
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), time.Second, "the retry waited for X-RateLimit-Reset")
}

func TestRejectedCreateFailsTheFlowWithoutClickUpTextWithRealDex(t *testing.T) {
	provider := newFakeClickUp(t)
	blockedID := provider.seedTask(blockedTaskName, "blocked", fakeEngineeringID)
	provider.rejectsCreate = true
	harness := newEscalationHarness(t, provider, defaultRequestTimeout, exampleSettings)
	flowID := harness.startEscalation(t, "rejected", blockedID)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.Contains(t, result.ErrorMessage, `"providerRejected"`)
	require.NotContains(t, result.ErrorMessage, providerSentinel)
	require.NotContains(t, result.ErrorMessage, fakeAPIToken)
	require.Equal(t, 1, provider.count("create"), "a conclusive rejection is not retried")
	require.Zero(t, provider.count("comment"))
	t.Logf("rejected create failure: %s", result.ErrorMessage)
}

func TestInvalidSettingsFailBeforeCallingClickUpWithRealDex(t *testing.T) {
	provider := newFakeClickUp(t)
	blockedID := provider.seedTask(blockedTaskName, "blocked", fakeEngineeringID)
	harness := newEscalationHarness(t, provider, defaultRequestTimeout, escalateblocked.EscalationSettings{})
	flowID := harness.startEscalation(t, "unconfigured", blockedID)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "CLICKUP_")
	require.Zero(t, provider.totalRequests())
}

// TestSignedBlockedStatusStartsOneFlowAndOtherDeliveriesStartNoneWithRealDex serves the example's target
// on the connection's endpoint: README steps 4 and 5.
func TestSignedBlockedStatusStartsOneFlowAndOtherDeliveriesStartNoneWithRealDex(t *testing.T) {
	provider := newFakeClickUp(t)
	blockedID := provider.seedTask(blockedTaskName, "blocked", fakeEngineeringID)
	otherID := provider.seedTask("Write release notes", "in review", fakeEngineeringID)
	harness := newEscalationHarness(t, provider, defaultRequestTimeout, exampleSettings)
	logs := newRecordedLogs(t)
	endpoint := newBlockedStatusEndpoint(t, provider, newExampleConnection(t, provider, clickup.WithLogger(logs.logger())),
		newBlockedStatusTarget(harness.client, harness.flow, logs.logger()))
	endpoint.start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	historyItemID := strconv.FormatInt(time.Now().UnixNano(), 10)
	body, signature := signedStatusEvent(blockedID, historyItemID, "blocked", fakeWebhookSecret)
	require.Equal(t, http.StatusOK, endpoint.postEvent(t, body, signature))
	flowID := escalateblocked.FlowIDPrefix + blockedID + "-" + historyItemID
	outcome := waitForOutcome(t, ctx, harness.client, flowID)
	require.Equal(t, escalateblocked.OutcomeEscalated, outcome.Action)

	// ClickUp retries the same event: the Flow start deduplicates it.
	require.Equal(t, http.StatusOK, endpoint.postEvent(t, body, signature))
	eventID := fakeWebhookID + ":taskStatusUpdated:" + historyItemID
	require.Eventually(t, func() bool {
		return len(logs.find("trigger event delivered", map[string]string{"event_id": eventID, "duplicate": "true"})) == 1
	}, 20*time.Second, 25*time.Millisecond, "the redelivery reaches Dex as a duplicate start")
	require.Equal(t, 1, provider.count("create"), "the Flow escalated once")
	require.Len(t, provider.commentsOf(blockedID), 1)

	// A forged event and another status start nothing.
	forgedBody, _ := signedStatusEvent(otherID, historyItemID+"1", "blocked", fakeWebhookSecret)
	require.Equal(t, http.StatusBadRequest, endpoint.postEvent(t, forgedBody, taskEventSignature(forgedBody, "another-secret")))
	reviewBody, reviewSignature := signedStatusEvent(otherID, historyItemID+"2", "in review", fakeWebhookSecret)
	require.Equal(t, http.StatusOK, endpoint.postEvent(t, reviewBody, reviewSignature))
	require.Eventually(t, func() bool {
		return len(logs.find("trigger event skipped: filtered", map[string]string{"event_id": fakeWebhookID + ":taskStatusUpdated:" + historyItemID + "2"})) == 1
	}, 20*time.Second, 25*time.Millisecond, "the Flow's admission rule consumes another status")
	requireNoFlow(t, ctx, harness.client, escalateblocked.FlowIDPrefix+otherID+"-"+historyItemID+"1")
	requireNoFlow(t, ctx, harness.client, escalateblocked.FlowIDPrefix+otherID+"-"+historyItemID+"2")
	require.NotContains(t, logs.text(), fakeWebhookSecret)
	require.NotContains(t, logs.text(), fakeAPIToken)
}

func dexAddress() string {
	return environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
}

// waitForOutcome waits for the Flow, which the Trigger starts asynchronously after the 200.
func waitForOutcome(t *testing.T, ctx context.Context, client *dex.Client, flowID string) escalateblocked.EscalationOutcome {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for {
		result, err := client.WaitForFlow(waitCtx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		if err == nil {
			require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
			var outcome escalateblocked.EscalationOutcome
			require.NoError(t, result.DecodeSingleOutput(&outcome))
			return outcome
		}
		var notFound *dex.FlowNotFoundError
		require.True(t, errors.As(err, &notFound) && waitCtx.Err() == nil, "the Flow must complete: %v", err)
		time.Sleep(50 * time.Millisecond) // The Flow start is asynchronous after the 200, so it may not exist yet.
	}
}

func requireNoFlow(t *testing.T, ctx context.Context, client *dex.Client, flowID string) {
	t.Helper()
	_, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	var notFound *dex.FlowNotFoundError
	require.ErrorAs(t, err, &notFound, "%s must not start a Flow", flowID)
}

// escalationHarness runs the Flow on a real Worker against the fake, with a replaceable Worker.
type escalationHarness struct {
	flow          *escalateblocked.Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newEscalationHarness(t *testing.T, provider *fakeClickUp, requestTimeout time.Duration, settings escalateblocked.EscalationSettings) *escalationHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "clickup", Name: escalateblocked.ConnectionName}
	providerClient, err := clickup.New(clickup.Config{},
		sdkgo.StaticCredentialProvider[clickup.Credentials]{reference: {APIToken: sdkgo.NewSecretString(fakeAPIToken)}},
		clickup.WithAPIBaseURL(provider.URL), clickup.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := clickup.NewConnection(providerClient, reference)
	require.NoError(t, err)
	harness := &escalationHarness{flow: escalateblocked.NewFlow(connection, settings), serverAddress: dexAddress()}
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

func (harness *escalationHarness) startWorker(t *testing.T) {
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
func (harness *escalationHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *escalationHarness) runEscalation(t *testing.T, scenario string, taskID string) escalateblocked.EscalationOutcome {
	t.Helper()
	flowID := harness.startEscalation(t, scenario, taskID)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome escalateblocked.EscalationOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

func (harness *escalationHarness) startEscalation(t *testing.T, scenario string, taskID string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "clickup-escalation-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, escalateblocked.BlockedTask{TaskID: taskID}, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *escalationHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
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
