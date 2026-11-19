// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

// workflowJira fakes one issue on a To Do -> In Progress -> Done workflow.
type workflowJira struct {
	*recordingJira
	mutex               sync.Mutex
	statusID            string
	transitionPosts     int
	postedTransitionIDs []string
	postResponse        func(http.ResponseWriter, int) bool
	readResponse        func(http.ResponseWriter) bool
	extraTransitions    [][3]string
	hasGlobalDoneLoop   bool
}

var workflowStatuses = map[string]struct{ name, category string }{
	"1": {"To Do", "new"}, "3": {"In Progress", "indeterminate"}, "5": {"Done", "done"},
}

// workflowTransitions maps a source status to its transitions: ID, name, destination.
var workflowTransitions = map[string][][3]string{
	"1": {{"11", "Start Progress", "3"}, {"31", "Done", "5"}},
	"3": {{"21", "Stop Progress", "1"}, {"31", "Done", "5"}, {"41", "Resolve", "5"}},
	"5": {{"51", "Reopen", "1"}},
}

func newWorkflowJira(t *testing.T, statusID string) *workflowJira {
	provider := &workflowJira{statusID: statusID}
	provider.recordingJira = newRecordingJira(t, provider.serveHTTP)
	return provider
}

func (provider *workflowJira) serveHTTP(response http.ResponseWriter, request *http.Request, _ int) {
	t := provider.t
	require.True(t, strings.HasPrefix(request.URL.Path, testSitePrefix+"/issue/OPS-42"))
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if request.Method == http.MethodGet {
		require.Equal(t, "status", request.URL.Query().Get("fields"))
		require.Equal(t, "transitions", request.URL.Query().Get("expand"))
		if provider.readResponse != nil && provider.readResponse(response) {
			return
		}
		writeJSON(t, response, http.StatusOK, provider.issueWithTransitionsJSON())
		return
	}
	var body struct {
		Transition struct {
			ID string `json:"id"`
		} `json:"transition"`
	}
	require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
	provider.transitionPosts++
	provider.postedTransitionIDs = append(provider.postedTransitionIDs, body.Transition.ID)
	if provider.postResponse != nil && provider.postResponse(response, provider.transitionPosts) {
		return
	}
	for _, transition := range provider.availableTransitions() {
		if transition[0] == body.Transition.ID {
			provider.statusID = transition[2]
			response.WriteHeader(http.StatusNoContent)
			return
		}
	}
	writeJSON(t, response, http.StatusBadRequest, `{"errorMessages":["SENTINEL Transition id '`+body.Transition.ID+`' is not valid for this issue."],"errors":{}}`)
}

func (provider *workflowJira) availableTransitions() [][3]string {
	transitions := append([][3]string(nil), workflowTransitions[provider.statusID]...)
	if provider.hasGlobalDoneLoop && provider.statusID == "5" {
		transitions = append(transitions, [3]string{"31", "Done", "5"})
	}
	return append(transitions, provider.extraTransitions...)
}

func (provider *workflowJira) issueWithTransitionsJSON() string {
	var transitions []string
	for _, transition := range provider.availableTransitions() {
		destination := workflowStatuses[transition[2]]
		transitions = append(transitions, fmt.Sprintf(`{"id":%q,"name":%q,"to":{"id":%q,"name":%q,"statusCategory":{"key":%q}}}`,
			transition[0], transition[1], transition[2], destination.name, destination.category))
	}
	status := workflowStatuses[provider.statusID]
	return fmt.Sprintf(`{"id":"10042","key":"OPS-42","fields":{"status":{"id":%q,"name":%q,"statusCategory":{"key":%q}}},"transitions":[%s]}`,
		provider.statusID, status.name, status.category, strings.Join(transitions, ","))
}

func (provider *workflowJira) posts() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.transitionPosts
}

func runTransition(t *testing.T, provider *workflowJira, input jira.TransitionIssueInput) (sdkgo.MutationResult[jira.TransitionIssueOutput], error) {
	t.Helper()
	client := newJiraClient(t, provider.URL)
	return sdkgo.RunMutation(newJiraDexContext("transition"), client.TransitionIssue(), jiraConnection, input)
}

func TestTransitionByNamePostsTheMatchedTransition(t *testing.T) {
	provider := newWorkflowJira(t, "1")
	result, err := runTransition(t, provider, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionName: "start progress"})
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch)
	require.Equal(t, "To Do", result.Value.PreviousStatus.Name)
	require.Equal(t, jira.IssueStatus{ID: "3", Name: "In Progress", CategoryKey: jira.StatusCategoryInProgress}, result.Value.Status)
	require.Equal(t, "11", result.Value.Transition.ID)
	require.False(t, result.Value.IsAlreadyInDestinationStatus)
	require.Equal(t, 1, provider.posts())
	require.JSONEq(t, `{"transition":{"id":"11"}}`, provider.request(1).body)
	require.Equal(t, testSitePrefix+"/issue/OPS-42/transitions", provider.request(1).path)
}

func TestTransitionToTheCurrentStatusSendsNothing(t *testing.T) {
	provider := newWorkflowJira(t, "5")
	result, err := runTransition(t, provider, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "31", DestinationStatusName: "done"})
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch)
	require.True(t, result.Value.IsAlreadyInDestinationStatus)
	require.Nil(t, result.Value.Transition)
	require.Zero(t, provider.posts())

	loopProvider := newWorkflowJira(t, "5")
	loopProvider.hasGlobalDoneLoop = true
	result, err = runTransition(t, loopProvider, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "31"})
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch, "a global transition into the current status is a no-op")
	require.True(t, result.Value.IsAlreadyInDestinationStatus)
	require.Zero(t, loopProvider.posts())
}

func TestUnavailableOrAmbiguousTransitionListsTheAvailableOnes(t *testing.T) {
	provider := newWorkflowJira(t, "3")
	result, err := runTransition(t, provider, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionName: "Start Progress"})
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitionUnavailable, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, "In Progress", result.Value.Status.Name)
	require.Len(t, result.Value.AvailableTransitions, 3)
	require.Zero(t, provider.posts())

	result, err = runTransition(t, provider, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", DestinationStatusName: "Done"})
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitionUnavailable, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	require.Equal(t, "2 transitions available from status 3 match the request; set transitionId", result.Failure.Message)

	result, err = runTransition(t, provider, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionName: "Resolve", DestinationStatusName: "Done", ResolutionName: "Fixed"})
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch)
	require.JSONEq(t, `{"transition":{"id":"41"},"fields":{"resolution":{"name":"Fixed"}}}`, provider.request(provider.requestCount()-1).body)
}

func TestRejectedTransitionAfterAConcurrentDuplicateIsTransitioned(t *testing.T) {
	provider := newWorkflowJira(t, "1")
	provider.postResponse = func(response http.ResponseWriter, _ int) bool {
		provider.statusID = "3"
		writeJSON(provider.t, response, http.StatusBadRequest, `{"errorMessages":["SENTINEL not valid"],"errors":{}}`)
		return true
	}
	result, err := runTransition(t, provider, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"})
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch)
	require.Equal(t, "In Progress", result.Value.Status.Name)
	require.Equal(t, 3, provider.requestCount(), "read, transition, read back")
	requireNoSentinel(t, result)
}

func TestRejectedTransitionReportsTheScreenFieldIDs(t *testing.T) {
	provider := newWorkflowJira(t, "3")
	provider.postResponse = func(response http.ResponseWriter, _ int) bool {
		writeJSON(provider.t, response, http.StatusBadRequest, `{"errorMessages":[],"errors":{"resolution":"SENTINEL Resolution is required."}}`)
		return true
	}
	result, err := runTransition(t, provider, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionName: "Resolve"})
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchProviderRejected, result.Branch)
	require.Equal(t, []string{"resolution"}, result.Value.RejectedFieldIDs)
	require.Equal(t, "Jira rejected the transition with HTTP 400 (fields: resolution)", result.Failure.Message)
	require.Equal(t, "In Progress", result.Value.Status.Name)
	requireNoSentinel(t, result)
}

func TestUnknownTransitionOutcomeRetriesAndTheNextAttemptReadsFirst(t *testing.T) {
	for _, test := range []struct {
		name          string
		status        int
		appliesAnyway bool
		kind          sdkgo.FailureKind
		posts         int
	}{
		{name: "server error before applying", status: http.StatusInternalServerError, kind: sdkgo.FailureAvailability, posts: 2},
		{name: "server error after applying", status: http.StatusInternalServerError, appliesAnyway: true, kind: sdkgo.FailureAvailability, posts: 1},
		{name: "conflicting update", status: http.StatusConflict, kind: sdkgo.FailureConflict, posts: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newWorkflowJira(t, "1")
			provider.postResponse = func(response http.ResponseWriter, post int) bool {
				if post > 1 {
					return false
				}
				if test.appliesAnyway {
					provider.statusID = "3"
				}
				writeJSON(provider.t, response, test.status, `{"errorMessages":["SENTINEL"]}`)
				return true
			}
			ctx := singledispatchtest.NewContext("jira-flow", "transition-unknown-"+test.name)
			input := jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"}
			_, err := runTransitionInContext(t, ctx, provider, input)
			requireRetry(t, err, test.kind)
			require.Equal(t, 2, provider.requestCount(), "read and transition, without a read back")
			ctx.AdvanceAttempt()
			result, err := runTransitionInContext(t, ctx, provider, input)
			require.NoError(t, err)
			require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch)
			require.Equal(t, "In Progress", result.Value.Status.Name)
			require.Equal(t, test.posts, provider.posts())
		})
	}
}

func TestMissingIssueSelectsNotFoundBeforeAnyTransition(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, `{"errorMessages":["SENTINEL"]}`)
	})
	client := newJiraClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newJiraDexContext("transition-missing"), client.TransitionIssue(), jiraConnection,
		jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionName: "Done"})
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchNotFound, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestTransitionRequiresASelector(t *testing.T) {
	provider := newWorkflowJira(t, "1")
	for _, input := range []jira.TransitionIssueInput{
		{IssueIDOrKey: "OPS-42"},
		{IssueIDOrKey: "OPS-42", TransitionID: "eleven"},
		{IssueIDOrKey: "OPS-42", TransitionName: "Done\nnow"},
	} {
		result, err := runTransition(t, provider, input)
		require.NoError(t, err)
		require.Equal(t, jira.TransitionIssueBranchDefect, result.Branch)
	}
	require.Zero(t, provider.requestCount())
}

func runTransitionInContext(t *testing.T, ctx *singledispatchtest.Context, provider *workflowJira, input jira.TransitionIssueInput) (sdkgo.MutationResult[jira.TransitionIssueOutput], error) {
	t.Helper()
	return sdkgo.RunMutation(ctx, newJiraClient(t, provider.URL).TransitionIssue(), jiraConnection, input)
}

// failFirstTransitionPost answers the first transition with an unknown outcome and later ones normally.
func failFirstTransitionPost(provider *workflowJira) {
	provider.postResponse = func(response http.ResponseWriter, post int) bool {
		if post > 1 {
			return false
		}
		writeJSON(provider.t, response, http.StatusBadGateway, `{}`)
		return true
	}
}

func TestTransitionRecordsItsCheckpointBeforeSending(t *testing.T) {
	provider := newWorkflowJira(t, "1")
	failFirstTransitionPost(provider)
	ctx := singledispatchtest.NewContext("jira-flow", "transition-checkpoint")
	_, err := runTransitionInContext(t, ctx, provider, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionName: "Start Progress"})
	requireRetry(t, err, sdkgo.FailureAvailability)
	heartbeats := ctx.Heartbeats()
	require.Len(t, heartbeats, 1)
	var checkpoint map[string]any
	require.NoError(t, json.Unmarshal(heartbeats[0], &checkpoint))
	require.NotEmpty(t, checkpoint["callId"])
	delete(checkpoint, "callId")
	require.Equal(t, map[string]any{
		"attempt": float64(1), "transitionId": "11", "transitionName": "Start Progress", "sourceStatusId": "1", "destinationStatusId": "3",
	}, checkpoint)

	failing := newWorkflowJira(t, "1")
	unwritable := singledispatchtest.NewContext("jira-flow", "transition-checkpoint-unwritable")
	unwritable.SetWriteError(errors.New("heartbeat unavailable"))
	_, err = runTransitionInContext(t, unwritable, failing, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"})
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Zero(t, failing.posts(), "nothing is sent without a checkpoint")

	unreadable := singledispatchtest.NewContext("jira-flow", "transition-checkpoint-unreadable")
	unreadable.SetReadError(errors.New("heartbeat unavailable"))
	untouched := newWorkflowJira(t, "1")
	_, err = runTransitionInContext(t, unreadable, untouched, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"})
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Zero(t, untouched.requestCount(), "an unreadable checkpoint never falls back to selecting again")
}

func TestRetriedTransitionInTheRecordedDestinationSendsNothing(t *testing.T) {
	// Stop Progress and Start Progress toggle, so a resend after a late success would undo the move.
	provider := newWorkflowJira(t, "3")
	failFirstTransitionPost(provider)
	ctx := singledispatchtest.NewContext("jira-flow", "transition-late-success")
	input := jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionName: "Stop Progress"}
	_, err := runTransitionInContext(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	// Jira applied the transition late.
	provider.statusID = "1"
	ctx.AdvanceAttempt()
	result, err := runTransitionInContext(t, ctx, provider, input)
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch)
	require.True(t, result.Value.IsAlreadyInDestinationStatus)
	require.Equal(t, &jira.TransitionReference{ID: "21", Name: "Stop Progress", DestinationStatus: result.Value.Status}, result.Value.Transition)
	require.Equal(t, "To Do", result.Value.Status.Name)
	require.Equal(t, 1, provider.posts(), "the recorded destination proves the transition")
}

func TestRetriedTransitionStillInTheSourceResendsTheRecordedTransition(t *testing.T) {
	provider := newWorkflowJira(t, "1")
	failFirstTransitionPost(provider)
	ctx := singledispatchtest.NewContext("jira-flow", "transition-resend")
	input := jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionName: "Start Progress"}
	_, err := runTransitionInContext(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	// A second transition with the same name would make selecting by name again ambiguous.
	provider.extraTransitions = [][3]string{{"12", "Start Progress", "5"}}
	ctx.AdvanceAttempt()
	result, err := runTransitionInContext(t, ctx, provider, input)
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch)
	require.Equal(t, "In Progress", result.Value.Status.Name)
	require.Equal(t, []string{"11", "11"}, provider.postedTransitionIDs, "the retry sends the recorded transition ID")
}

func TestRetriedTransitionMovedElsewhereIsUnavailableWithoutSending(t *testing.T) {
	provider := newWorkflowJira(t, "1")
	failFirstTransitionPost(provider)
	ctx := singledispatchtest.NewContext("jira-flow", "transition-moved-elsewhere")
	input := jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"}
	_, err := runTransitionInContext(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	// Someone else closed the issue.
	provider.statusID = "5"
	ctx.AdvanceAttempt()
	result, err := runTransitionInContext(t, ctx, provider, input)
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitionUnavailable, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, "Done", result.Value.Status.Name)
	require.Len(t, result.Value.AvailableTransitions, 1)
	require.Equal(t, 1, provider.posts())

	removed := newWorkflowJira(t, "1")
	failFirstTransitionPost(removed)
	removedCtx := singledispatchtest.NewContext("jira-flow", "transition-removed")
	_, err = runTransitionInContext(t, removedCtx, removed, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	removedCtx.AdvanceAttempt()
	removed.readResponse = func(response http.ResponseWriter) bool {
		writeJSON(removed.t, response, http.StatusOK, `{"id":"10042","key":"OPS-42","fields":{"status":{"id":"1","name":"To Do","statusCategory":{"key":"new"}}},"transitions":[{"id":"12","name":"Start Progress","to":{"id":"3","name":"In Progress","statusCategory":{"key":"indeterminate"}}}]}`)
		return true
	}
	result, err = runTransitionInContext(t, removedCtx, removed, input)
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitionUnavailable, result.Branch, "a recorded transition no longer offered is not replaced")
	require.Equal(t, 1, removed.posts())
}

func TestRepeatedTransitionRecognizesTheDestinationStatus(t *testing.T) {
	provider := newWorkflowJira(t, "1")
	provider.postResponse = func(response http.ResponseWriter, _ int) bool {
		response.WriteHeader(http.StatusGatewayTimeout)
		return true
	}
	ctx := singledispatchtest.NewContext("jira-flow", "transition-recognized")
	input := jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11", DestinationStatusName: "In Progress"}
	_, err := runTransitionInContext(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	// Jira applied the transition after the first attempt gave up.
	provider.statusID = "3"
	ctx.AdvanceAttempt()
	result, err := runTransitionInContext(t, ctx, provider, input)
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch)
	require.Equal(t, "In Progress", result.Value.Status.Name)
	require.Equal(t, 1, provider.posts())
}

func TestRetriedTransitionReadFailures(t *testing.T) {
	provider := newWorkflowJira(t, "1")
	failFirstTransitionPost(provider)
	ctx := singledispatchtest.NewContext("jira-flow", "transition-retried-read")
	input := jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"}
	_, err := runTransitionInContext(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	provider.readResponse = func(response http.ResponseWriter) bool {
		writeJSON(provider.t, response, http.StatusInternalServerError, `{"errorMessages":["SENTINEL"]}`)
		return true
	}
	ctx.AdvanceAttempt()
	_, err = runTransitionInContext(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, 1, provider.posts(), "a transient read failure sends nothing")

	provider.readResponse = func(response http.ResponseWriter) bool {
		writeJSON(provider.t, response, http.StatusNotFound, `{"errorMessages":["SENTINEL"]}`)
		return true
	}
	ctx.AdvanceAttempt()
	result, err := runTransitionInContext(t, ctx, provider, input)
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchNotFound, result.Branch, "a definite read outcome stays definite")
	require.Equal(t, 1, provider.posts())
}

func TestTransitionRefusalWithAnUnreadableBodyIsUnknown(t *testing.T) {
	provider := newWorkflowJira(t, "1")
	provider.postResponse = func(response http.ResponseWriter, post int) bool {
		if post > 1 {
			return false
		}
		writeTruncatedJSON(response, http.StatusBadRequest)
		return true
	}
	ctx := singledispatchtest.NewContext("jira-flow", "transition-truncated-refusal")
	_, err := runTransitionInContext(t, ctx, provider, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"})
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, 2, provider.requestCount(), "a cut refusal body proves nothing, so no read back")
	ctx.AdvanceAttempt()
	result, err := runTransitionInContext(t, ctx, provider, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"})
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch)
	require.Equal(t, 2, provider.posts(), "the issue was still in its source status, so the transition was sent again")
}

func TestTransitionRateLimitAllowsTheNextAttemptToSend(t *testing.T) {
	provider := newWorkflowJira(t, "1")
	provider.postResponse = func(response http.ResponseWriter, post int) bool {
		if post == 1 {
			writeJSON(provider.t, response, http.StatusTooManyRequests, `{}`)
			return true
		}
		return false
	}
	ctx := singledispatchtest.NewContext("jira-flow", "transition-rate-limited")
	input := jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"}
	_, err := runTransitionInContext(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureRateLimit)
	ctx.AdvanceAttempt()
	result, err := runTransitionInContext(t, ctx, provider, input)
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch)
	require.Equal(t, 2, provider.posts())
}

func TestTransitionSendsOnTheThirdAttemptAfterTwoRateLimits(t *testing.T) {
	provider := newWorkflowJira(t, "1")
	provider.postResponse = func(response http.ResponseWriter, post int) bool {
		if post <= 2 {
			writeJSON(provider.t, response, http.StatusTooManyRequests, `{}`)
			return true
		}
		return false
	}
	ctx := singledispatchtest.NewContext("jira-flow", "transition-two-rate-limits")
	input := jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"}
	for range 2 {
		_, err := runTransitionInContext(t, ctx, provider, input)
		requireRetry(t, err, sdkgo.FailureRateLimit)
		ctx.AdvanceAttempt()
	}
	result, err := runTransitionInContext(t, ctx, provider, input)
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch)
	require.Equal(t, 3, provider.posts(), "Jira processed neither rate-limited send")
}

func TestTransitionSendsOnTheThirdAttemptAfterTwoPreSendRetries(t *testing.T) {
	provider := newWorkflowJira(t, "1")
	credentials := &delayedJiraCredentials{}
	client, err := jira.New(jira.Config{CloudID: testCloudID, Endpoint: provider.URL}, credentials)
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("jira-flow", "transition-two-pre-send-retries")
	input := jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"}
	for range 2 {
		_, err = sdkgo.RunMutation(ctx, client.TransitionIssue(), jiraConnection, input)
		requireRetry(t, err, sdkgo.FailureAvailability)
		ctx.AdvanceAttempt()
	}
	credentials.isReady = true
	result, err := sdkgo.RunMutation(ctx, client.TransitionIssue(), jiraConnection, input)
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch)
	require.Equal(t, 1, provider.posts())
}

func TestTransitionCredentialFailureAfterAPossibleSendRetries(t *testing.T) {
	provider := newWorkflowJira(t, "1")
	failFirstTransitionPost(provider)
	ctx := singledispatchtest.NewContext("jira-flow", "transition-credential")
	_, err := runTransitionInContext(t, ctx, provider, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"})
	requireRetry(t, err, sdkgo.FailureAvailability)
	ctx.AdvanceAttempt()
	credentials := &delayedJiraCredentials{isReady: true, hasInvalidCredentials: true}
	client, err := jira.New(jira.Config{CloudID: testCloudID, Endpoint: provider.URL}, credentials)
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(ctx, client.TransitionIssue(), jiraConnection, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"})
	requireRetry(t, err, sdkgo.FailureAuthentication)
	require.Equal(t, 1, provider.posts())

	first := singledispatchtest.NewContext("jira-flow", "transition-credential-first")
	result, err := sdkgo.RunMutation(first, client.TransitionIssue(), jiraConnection, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"})
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchDefect, result.Branch, "a terminal credential failure before any send is a defect")

	credentials.hasInvalidCredentials, credentials.isReady = false, false
	transient := singledispatchtest.NewContext("jira-flow", "transition-credential-transient")
	_, err = sdkgo.RunMutation(transient, client.TransitionIssue(), jiraConnection, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"})
	requireRetry(t, err, sdkgo.FailureAvailability)
	heartbeats := transient.Heartbeats()
	require.Len(t, heartbeats, 1)
	require.Contains(t, string(heartbeats[0]), `"state":"notSent"`, "a transient failure before any send records notSent")
}

func TestRetryAfterALostCheckpointNeverSelectsAgain(t *testing.T) {
	// Stop Progress and Start Progress toggle, so selecting by name again would undo the move.
	provider := newWorkflowJira(t, "3")
	failFirstTransitionPost(provider)
	ctx := singledispatchtest.NewContext("jira-flow", "transition-lost-checkpoint")
	ctx.LoseNextHeartbeat()
	input := jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionName: "Stop Progress"}
	_, err := runTransitionInContext(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	// Jira applied the transition late, and Dex lost the checkpoint.
	provider.statusID = "1"
	provider.extraTransitions = [][3]string{{"22", "Stop Progress", "3"}}
	for range 2 {
		ctx.AdvanceAttempt()
		_, err = runTransitionInContext(t, ctx, provider, input)
		requireRetry(t, err, sdkgo.FailureAvailability)
	}
	require.Equal(t, 1, provider.posts(), "a possible send without a checkpoint is never followed by another")

	recognized := newWorkflowJira(t, "1")
	laterCtx := laterFenceContext("transition-lost-checkpoint-destination")
	result, err := runTransitionInContext(t, laterCtx, recognized, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", DestinationStatusName: "To Do"})
	require.NoError(t, err)
	require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch, "a known destination still proves the move")
	require.Zero(t, recognized.posts())
}

func TestRetryAfterAnEarlierReleaseMarker(t *testing.T) {
	input := jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"}
	for _, test := range []struct {
		state string
		posts int
	}{{state: "notSent", posts: 1}, {state: "canResend", posts: 1}, {state: "claimed", posts: 0}} {
		step := "transition-legacy-" + test.state
		// The first attempt's checkpoint names the Call ID the step derives.
		probe := singledispatchtest.NewContext("jira-flow", step)
		_, err := runTransitionInContext(t, probe, newWorkflowJira(t, "5"), jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "51"})
		require.NoError(t, err)
		var probeCheckpoint struct {
			CallID string `json:"callId"`
		}
		require.NoError(t, json.Unmarshal(probe.Heartbeats()[0], &probeCheckpoint))
		provider := newWorkflowJira(t, "1")
		ctx := laterFenceContext(step)
		marker := map[string]any{"callId": probeCheckpoint.CallID, "attempt": 1, "state": test.state}
		require.NoError(t, ctx.SetPersistedCheckpoint(map[string]any{"singleDispatchMarkers": map[string]any{probeCheckpoint.CallID: marker}}))
		_, _ = runTransitionInContext(t, ctx, provider, input)
		require.Equal(t, test.posts, provider.posts(), "earlier release marker %s", test.state)
	}
}
