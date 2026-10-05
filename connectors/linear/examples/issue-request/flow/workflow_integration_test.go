//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package issuerequest

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
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationTitle      = "Monthly Fire Drill Checklist - February"
	defaultRequestTimeout = 5 * time.Second
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowResponseDelay  = 9 * time.Second
	slowRequestTimeout = 12 * time.Second
)

func integrationInput() Input {
	return Input{
		Title: integrationTitle, Description: "Check every panel on **all** floors.", AssigneeEmail: "Alice@Example.com",
		StateName: "In Progress", Comment: "Scheduled from the facilities request.",
	}
}

func TestNewRequestCreatesAssignsMovesAndCommentsWithRealDex(t *testing.T) {
	provider := newFakeLinear(t)
	harness := newRequestHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runIssueRequest(t, "new-request", integrationInput())
	require.Equal(t, IssueCreated, outcome.Action)
	require.False(t, outcome.WasCreateReplayed)
	require.Equal(t, aliceUserID, outcome.AssigneeID)
	require.Equal(t, "In Progress", outcome.StateName)
	require.Equal(t, linear.WorkflowStateTypeStarted, outcome.ReadBackStateType)
	require.Equal(t, 1, provider.issueCount())
	issue := provider.issue(outcome.IssueID)
	require.Equal(t, integrationTitle, issue.title)
	require.Equal(t, "Check every panel on **all** floors.", issue.description)
	require.Equal(t, inProgressStateID, issue.stateID)
	require.Equal(t, aliceUserID, issue.assigneeID)
	comments := provider.commentsOn(outcome.IssueID)
	require.Len(t, comments, 1)
	require.Equal(t, "Scheduled from the facilities request.", comments[0].body)
	require.Equal(t, comments[0].id, outcome.CommentID)
	require.Equal(t, map[string]any{
		"team":  map[string]any{"id": map[string]any{"eq": integrationTeamID}},
		"title": map[string]any{"eq": integrationTitle},
		"state": map[string]any{"type": map[string]any{"in": []any{"triage", "backlog", "unstarted", "started"}}},
	}, provider.lastVariables("LinearSearchIssues")["filter"])
	require.Equal(t, 1, provider.count("LinearCreateIssue"))
}

func TestOpenIssueWithTheTitleIsReusedAndDecoysAreUntouchedWithRealDex(t *testing.T) {
	provider := newFakeLinear(t)
	open := provider.seedIssue(integrationTeamID, integrationTitle, backlogStateID)
	completed := provider.seedIssue(integrationTeamID, integrationTitle, doneStateID)
	otherTeam := provider.seedIssue(otherTeamID, integrationTitle, backlogStateID)
	lookalike := provider.seedIssue(integrationTeamID, integrationTitle+" (2025)", backlogStateID)
	harness := newRequestHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runIssueRequest(t, "existing-issue", integrationInput())
	require.Equal(t, IssueReused, outcome.Action)
	require.Equal(t, open, outcome.IssueID, "the open issue with the title is reused, not duplicated")
	require.Equal(t, inProgressStateID, provider.issue(open).stateID)
	require.Len(t, provider.commentsOn(open), 1)
	require.Equal(t, 4, provider.issueCount(), "no issue was created")
	require.Zero(t, provider.count("LinearCreateIssue"))
	for _, decoy := range []string{completed, otherTeam, lookalike} {
		snapshot := provider.issue(decoy)
		require.Equal(t, snapshot.seededAt, snapshot.updatedAt, "issue %s must not be touched", decoy)
		require.Empty(t, provider.commentsOn(decoy))
	}
}

func TestUnknownAssigneeLeavesTheIssueUnassignedWithRealDex(t *testing.T) {
	provider := newFakeLinear(t)
	harness := newRequestHarness(t, provider, defaultRequestTimeout)
	input := integrationInput()
	input.AssigneeEmail, input.StateName = "contractor@vendor.example.com", ""

	outcome := harness.runIssueRequest(t, "unknown-assignee", input)
	require.Equal(t, IssueCreated, outcome.Action)
	require.True(t, outcome.IsAssigneeUnknown)
	require.Empty(t, provider.issue(outcome.IssueID).assigneeID)
	require.Equal(t, "Todo", outcome.StateName, "a blank state name uses the team's first unstarted state")
	require.Equal(t, todoStateID, provider.issue(outcome.IssueID).stateID)
}

// TestSlowCreateIsDispatchedAgainAndCreatesOneIssueWithRealDex is the duplicate-dispatch proof: Linear
// already stored the issue when Dex dispatches the async Step again, and the repeated client UUID reads it back.
func TestSlowCreateIsDispatchedAgainAndCreatesOneIssueWithRealDex(t *testing.T) {
	provider := newFakeLinear(t)
	provider.delaysFirstCreate = true
	harness := newRequestHarness(t, provider, slowRequestTimeout)

	startedAt := time.Now()
	outcome := harness.runIssueRequest(t, "slow-create", integrationInput())
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, time.Since(startedAt), slowResponseDelay)
	require.Equal(t, IssueCreated, outcome.Action)
	require.GreaterOrEqual(t, provider.count("LinearCreateIssue"), 2, "Dex dispatched the create again past its local phase")
	require.Equal(t, 1, provider.count("appliedCreate"), "only the first request stored an issue")
	require.Equal(t, 1, provider.issueCount(), "the repeated dispatch did not create a second issue")
	require.Len(t, provider.distinctInputIDs("LinearCreateIssue"), 1, "every attempt sent the same client UUID")
	require.Len(t, provider.commentsOn(outcome.IssueID), 1)
	t.Logf("slow create: creates=%d duplicates=%d read-backs=%d replayedOutcome=%v", provider.count("LinearCreateIssue"),
		provider.count("duplicateCreate"), provider.count("LinearReadIssueSummary"), outcome.WasCreateReplayed)
}

func TestSlowCommentIsDispatchedAgainAndAddsOneCommentWithRealDex(t *testing.T) {
	provider := newFakeLinear(t)
	provider.delaysFirstComment = true
	harness := newRequestHarness(t, provider, slowRequestTimeout)

	outcome := harness.runIssueRequest(t, "slow-comment", integrationInput())
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("LinearAddComment"), 2, "Dex dispatched the comment again past its local phase")
	require.Len(t, provider.commentsOn(outcome.IssueID), 1, "the repeated dispatch did not post a second comment")
	require.Len(t, provider.distinctInputIDs("LinearAddComment"), 1)
}

func TestSlowMoveIsSafeToRepeatWithRealDex(t *testing.T) {
	provider := newFakeLinear(t)
	provider.delaysFirstUpdate = true
	harness := newRequestHarness(t, provider, slowRequestTimeout)

	outcome := harness.runIssueRequest(t, "slow-move", integrationInput())
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("LinearUpdateIssue"), 2, "Dex dispatched the update again past its local phase")
	require.Equal(t, inProgressStateID, provider.issue(outcome.IssueID).stateID, "absolute values leave the same state")
	require.Len(t, provider.commentsOn(outcome.IssueID), 1)
}

// TestLostWorkerDuringCreateCreatesOneIssueWithRealDex shows the client UUID surviving a Worker lost mid-create.
func TestLostWorkerDuringCreateCreatesOneIssueWithRealDex(t *testing.T) {
	provider := newFakeLinear(t)
	provider.holdsFirstCreate = make(chan struct{})
	harness := newRequestHarness(t, provider, slowRequestTimeout)
	flowID := harness.startIssueRequest(t, "lost-worker", integrationInput())
	require.Eventually(t, func() bool { return provider.count("LinearCreateIssue") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach Linear")
	harness.replaceWorker(t)
	require.Eventually(t, func() bool { return provider.count("LinearCreateIssue") >= 2 }, time.Minute, 50*time.Millisecond,
		"an attempt on the new Worker must reach Linear")
	close(provider.holdsFirstCreate)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome IssueRequestOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, IssueCreated, outcome.Action)
	require.True(t, outcome.WasCreateReplayed, "the new Worker read back the issue the lost attempt created")
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.issueCount(), "the attempt on the new Worker did not create a second issue")
	require.Len(t, provider.distinctInputIDs("LinearCreateIssue"), 1, "the UUID survives Worker replacement")
}

func TestRateLimitedSearchIsRetriedAfterLinearsDelayWithRealDex(t *testing.T) {
	provider := newFakeLinear(t)
	provider.rateLimitsFirstSearch = true
	harness := newRequestHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runIssueRequest(t, "rate-limited", integrationInput())
	require.Equal(t, IssueCreated, outcome.Action)
	require.Equal(t, 2, provider.count("LinearSearchIssues"), "Linear's HTTP 400 RATELIMITED is retried, not rejected")
	require.Equal(t, 1, provider.issueCount())
}

func TestRejectedCreateFailsTheFlowWithoutLinearTextWithRealDex(t *testing.T) {
	provider := newFakeLinear(t)
	provider.rejectsCreate = true
	harness := newRequestHarness(t, provider, defaultRequestTimeout)
	flowID := harness.startIssueRequest(t, "rejected", integrationInput())

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "Linear rejected the issue: Linear rejected the input (HTTP 400) [INPUT_ERROR; invalid input]")
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	require.NotContains(t, result.ErrorMessage, integrationAPIKey)
	require.Equal(t, 1, provider.count("LinearCreateIssue"), "a conclusive rejection is not retried")
	require.Equal(t, 1, provider.count("LinearReadIssueSummary"), "the read-back proved no issue has the Step's UUID")
	require.Zero(t, provider.issueCount())
}

func TestInvalidRequestFailsBeforeCallingLinearWithRealDex(t *testing.T) {
	provider := newFakeLinear(t)
	harness := newRequestHarnessWithSelection(t, provider, defaultRequestTimeout, TeamSelection{})
	flowID := harness.startIssueRequest(t, "invalid", Input{Title: integrationTitle})

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "teamId")
	require.Zero(t, provider.totalRequests())
}

// requestHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type requestHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

// newRequestHarness picks the integration team with the team picker, as Dex Web would save it.
func newRequestHarness(t *testing.T, provider *fakeLinear, requestTimeout time.Duration) *requestHarness {
	t.Helper()
	return newRequestHarnessWithSelection(t, provider, requestTimeout, TeamSelection{TeamID: integrationTeamID, TeamKey: "ENG", TeamName: "Engineering"})
}

func newRequestHarnessWithSelection(t *testing.T, provider *fakeLinear, requestTimeout time.Duration, selection TeamSelection) *requestHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "linear", Name: ConnectionName}
	providerClient, err := linear.New(linear.Config{}, sdkgo.StaticCredentialProvider[linear.Credentials]{reference: {
		AuthMethodID: linear.PersonalAPIKeyAuthMethodID, APIKey: sdkgo.NewSecretString(integrationAPIKey),
	}}, linear.WithAPIURL(provider.URL+"/graphql"), linear.WithHTTPClient(&http.Client{Timeout: requestTimeout}))
	require.NoError(t, err)
	connection, err := linear.NewConnection(providerClient, reference)
	require.NoError(t, err)
	harness := &requestHarness{flow: NewFlow(connection, selection), serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
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

func (harness *requestHarness) startWorker(t *testing.T) {
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
func (harness *requestHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *requestHarness) runIssueRequest(t *testing.T, scenario string, input Input) IssueRequestOutcome {
	t.Helper()
	flowID := harness.startIssueRequest(t, scenario, input)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome IssueRequestOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

func (harness *requestHarness) startIssueRequest(t *testing.T, scenario string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "linear-issue-request-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *requestHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
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
