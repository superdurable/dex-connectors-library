// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// workflowProvider fakes one request on a service desk workflow; postReply may replace a transition's reply.
type workflowProvider struct {
	*recordingProvider
	mutex               sync.Mutex
	statusID            string
	transitionPosts     int
	postedTransitionIDs []string
	postReply           func(http.ResponseWriter) bool
	readReply           func(http.ResponseWriter) bool
	extraTransitions    [][3]string
}

var workflowStatuses = map[string][2]string{
	"1": {"Waiting for support", "new"}, "3": {"In progress", "indeterminate"}, "5": {"Resolved", "done"},
}

// workflowTransitions maps a source status to its transitions: ID, name, destination.
var workflowTransitions = map[string][][3]string{
	"1": {{"11", "Start work", "3"}, {"31", "Resolve this issue", "5"}},
	"3": {{"21", "Pending", "1"}, {"31", "Resolve this issue", "5"}},
	"5": {{"51", "Reopen", "1"}},
}

func newWorkflowProvider(t *testing.T, statusID string) *workflowProvider {
	provider := &workflowProvider{statusID: statusID}
	provider.recordingProvider = newRecordingProvider(t, provider.serveHTTP)
	return provider
}

func (provider *workflowProvider) serveHTTP(response http.ResponseWriter, request *http.Request, _ int) {
	t := provider.t
	require.True(t, strings.HasPrefix(request.URL.Path, testPlatformPrefix+"/issue/ITH-42"))
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	available := append(append([][3]string(nil), workflowTransitions[provider.statusID]...), provider.extraTransitions...)
	if request.Method == http.MethodGet {
		if provider.readReply != nil && provider.readReply(response) {
			return
		}
		var transitions []string
		for _, transition := range available {
			destination := workflowStatuses[transition[2]]
			transitions = append(transitions, fmt.Sprintf(`{"id":%q,"name":%q,"to":{"id":%q,"name":%q,"statusCategory":{"key":%q}}}`,
				transition[0], transition[1], transition[2], destination[0], destination[1]))
		}
		status := workflowStatuses[provider.statusID]
		writeJSON(t, response, http.StatusOK, fmt.Sprintf(`{"id":"10042","key":"ITH-42","fields":{"status":{"id":%q,"name":%q,"statusCategory":{"key":%q}}},"transitions":[%s]}`,
			provider.statusID, status[0], status[1], strings.Join(transitions, ",")))
		return
	}
	provider.transitionPosts++
	var body struct {
		Transition struct {
			ID string `json:"id"`
		} `json:"transition"`
	}
	require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
	provider.postedTransitionIDs = append(provider.postedTransitionIDs, body.Transition.ID)
	for _, transition := range available {
		if transition[0] == body.Transition.ID {
			provider.statusID = transition[2]
		}
	}
	if provider.postReply != nil && provider.postReply(response) {
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func TestTransitionTicketMovesTheRequestOnceToItsDestination(t *testing.T) {
	provider := newWorkflowProvider(t, "1")
	input := jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", DestinationStatusName: "resolved", ResolutionName: "Done"}
	result, err := sdkgo.RunMutation(newTestDexContext("transition"), newTestClient(t, provider.URL).TransitionTicket(), jsmConnection, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchTransitioned, result.Branch)
	require.Equal(t, "Resolved", result.Value.Status.Name)
	require.Equal(t, "Waiting for support", result.Value.PreviousStatus.Name)
	require.JSONEq(t, `{"transition":{"id":"31"},"fields":{"resolution":{"name":"Done"}}}`, provider.request(1).body)

	repeated, err := sdkgo.RunMutation(newTestDexContext("transition-repeat"), newTestClient(t, provider.URL).TransitionTicket(), jsmConnection, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchTransitioned, repeated.Branch)
	require.True(t, repeated.Value.IsAlreadyInDestinationStatus)
	require.Equal(t, 1, provider.transitionPosts, "a repeated Step finds the move already made")
}

func TestTransitionTicketListsTheAvailableTransitionsWhenNoneMatches(t *testing.T) {
	provider := newWorkflowProvider(t, "5")
	result, err := sdkgo.RunMutation(newTestDexContext("transition-unavailable"), newTestClient(t, provider.URL).TransitionTicket(), jsmConnection,
		jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", DestinationStatusName: "In progress"})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchTransitionUnavailable, result.Branch)
	require.Equal(t, []jiraservicemanagement.TransitionReference{{ID: "51", Name: "Reopen", DestinationStatus: jiraservicemanagement.TicketStatus{
		ID: "1", Name: "Waiting for support", CategoryKey: jiraservicemanagement.StatusCategoryToDo,
	}}}, result.Value.AvailableTransitions)
	require.Zero(t, provider.transitionPosts)
}

func TestTransitionTicketRetriesAnAmbiguousResponseAndRereadsFirst(t *testing.T) {
	applied := newWorkflowProvider(t, "1")
	applied.postReply = func(response http.ResponseWriter) bool {
		writeJSON(t, response, http.StatusInternalServerError, `{"errorMessages":["SENTINEL internal"]}`)
		return true
	}
	ctx := newTestDexContext("transition-ambiguous")
	input := jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionName: "start work"}
	_, err := runTransitionTicket(t, ctx, applied, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, 2, applied.requestCount(), "read and transition, without a read back")
	result, err := runTransitionTicket(t, ctx.nextAttempt(), applied, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchTransitioned, result.Branch, "the next attempt found the move applied")
	require.True(t, result.Value.IsAlreadyInDestinationStatus)
	require.Equal(t, 1, applied.transitionPosts)

	rejected := newWorkflowProvider(t, "1")
	rejected.postReply = func(response http.ResponseWriter) bool {
		rejected.statusID = "1"
		writeJSON(t, response, http.StatusBadRequest, `{"errorMessages":["SENTINEL screen"],"errors":{"resolution":"SENTINEL required"}}`)
		return true
	}
	result, err = sdkgo.RunMutation(newTestDexContext("transition-rejected"), newTestClient(t, rejected.URL).TransitionTicket(), jsmConnection,
		jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", DestinationStatusName: "Resolved"})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchProviderRejected, result.Branch)
	require.Equal(t, []string{"resolution"}, result.Value.RejectedFieldIDs)
	requireNoSentinel(t, result)
}

func runTransitionTicket(t *testing.T, ctx *testDexContext, provider *workflowProvider, input jiraservicemanagement.TransitionTicketInput) (sdkgo.MutationResult[jiraservicemanagement.TransitionTicketOutput], error) {
	t.Helper()
	return sdkgo.RunMutation(ctx, newTestClient(t, provider.URL).TransitionTicket(), jsmConnection, input)
}

// unappliedReply answers a transition with status after undoing the move the fake made.
func unappliedReply(provider *workflowProvider, status int) func(http.ResponseWriter) bool {
	before := provider.statusID
	return func(response http.ResponseWriter) bool {
		provider.statusID = before
		writeJSON(provider.t, response, status, `{}`)
		return true
	}
}

func TestTransitionTicketRecordsItsCheckpointBeforeSending(t *testing.T) {
	provider := newWorkflowProvider(t, "1")
	provider.postReply = unappliedReply(provider, http.StatusBadGateway)
	ctx := newTestDexContext("transition-checkpoint")
	_, err := runTransitionTicket(t, ctx, provider, jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionName: "Start work"})
	requireRetry(t, err, sdkgo.FailureAvailability)
	var checkpoint map[string]any
	require.NoError(t, json.Unmarshal(ctx.recordedHeartbeat, &checkpoint))
	require.NotEmpty(t, checkpoint["callId"])
	delete(checkpoint, "callId")
	require.Equal(t, map[string]any{
		"attempt": float64(1), "transitionId": "11", "transitionName": "Start work", "sourceStatusId": "1", "destinationStatusId": "3",
	}, checkpoint)

	unwritten := newWorkflowProvider(t, "1")
	rejecting := newTestDexContext("transition-checkpoint-unwritable")
	rejecting.rejectsHeartbeat = true
	_, err = runTransitionTicket(t, rejecting, unwritten, jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionID: "11"})
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Zero(t, unwritten.transitionPosts, "nothing is sent without a checkpoint")
}

func TestRetriedTransitionTicketStillInTheSourceResendsTheRecordedTransition(t *testing.T) {
	provider := newWorkflowProvider(t, "1")
	provider.postReply = unappliedReply(provider, http.StatusBadGateway)
	input := jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionName: "Start work"}
	ctx := newTestDexContext("transition-resend")
	_, err := runTransitionTicket(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	// A second transition with the same name would make selecting by name again ambiguous.
	provider.extraTransitions = [][3]string{{"12", "Start work", "5"}}
	provider.postReply = nil
	result, err := runTransitionTicket(t, ctx.nextAttempt(), provider, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchTransitioned, result.Branch)
	require.Equal(t, "In progress", result.Value.Status.Name)
	require.Equal(t, []string{"11", "11"}, provider.postedTransitionIDs, "the retry sends the recorded transition ID")
}

func TestRetriedTransitionTicketMovedElsewhereIsUnavailableWithoutSending(t *testing.T) {
	provider := newWorkflowProvider(t, "1")
	provider.postReply = unappliedReply(provider, http.StatusBadGateway)
	input := jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionID: "11"}
	ctx := newTestDexContext("transition-moved-elsewhere")
	_, err := runTransitionTicket(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	// An agent resolved the request meanwhile.
	provider.statusID = "5"
	result, err := runTransitionTicket(t, ctx.nextAttempt(), provider, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchTransitionUnavailable, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, "Resolved", result.Value.Status.Name)
	require.Equal(t, 1, provider.transitionPosts)
}

func TestRepeatedTransitionTicketRecognizesTheDestinationStatus(t *testing.T) {
	provider := newWorkflowProvider(t, "1")
	provider.postReply = unappliedReply(provider, http.StatusGatewayTimeout)
	input := jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionID: "11", DestinationStatusName: "In progress"}
	ctx := newTestDexContext("transition-recognized")
	_, err := runTransitionTicket(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	// Jira applied the transition after the first attempt gave up.
	provider.statusID = "3"
	result, err := runTransitionTicket(t, ctx.nextAttempt(), provider, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchTransitioned, result.Branch)
	require.Equal(t, 1, provider.transitionPosts)
}

func TestRetriedTransitionTicketReadFailures(t *testing.T) {
	for _, test := range []struct {
		status int
		branch sdkgo.BranchID
	}{
		{status: http.StatusInternalServerError},
		{status: http.StatusNotFound, branch: jiraservicemanagement.TransitionTicketBranchNotFound},
	} {
		provider := newWorkflowProvider(t, "1")
		provider.postReply = unappliedReply(provider, http.StatusBadGateway)
		input := jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionID: "11"}
		ctx := newTestDexContext("transition-retried-read")
		_, err := runTransitionTicket(t, ctx, provider, input)
		requireRetry(t, err, sdkgo.FailureAvailability)
		provider.readReply = func(response http.ResponseWriter) bool {
			writeJSON(t, response, test.status, `{"errorMessages":["SENTINEL"]}`)
			return true
		}
		result, err := runTransitionTicket(t, ctx.nextAttempt(), provider, input)
		if test.branch == "" {
			requireRetry(t, err, sdkgo.FailureAvailability)
		} else {
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch, "a definite read outcome stays definite")
		}
		require.Equal(t, 1, provider.transitionPosts, "HTTP %d sends nothing", test.status)
	}
}

func TestTransitionTicketRefusalWithAnUnreadableBodyIsUnknown(t *testing.T) {
	provider := newWorkflowProvider(t, "1")
	provider.postReply = func(response http.ResponseWriter) bool {
		provider.statusID = "1"
		hijacker, isHijacker := response.(http.Hijacker)
		require.True(t, isHijacker)
		connection, buffered, err := hijacker.Hijack()
		require.NoError(t, err)
		_, err = buffered.WriteString("HTTP/1.1 400 Bad Request\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"errors\":")
		require.NoError(t, err)
		require.NoError(t, buffered.Flush())
		require.NoError(t, connection.Close())
		return true
	}
	input := jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionID: "11"}
	ctx := newTestDexContext("transition-cut-refusal")
	_, err := runTransitionTicket(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	provider.postReply = nil
	result, err := runTransitionTicket(t, ctx.nextAttempt(), provider, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchTransitioned, result.Branch)
	require.Equal(t, 2, provider.transitionPosts, "the request was still in its source status, so the transition was sent again")
}

func TestTransitionTicketRateLimitAllowsTheNextAttemptToSend(t *testing.T) {
	provider := newWorkflowProvider(t, "1")
	provider.postReply = unappliedReply(provider, http.StatusTooManyRequests)
	input := jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionID: "11"}
	ctx := newTestDexContext("transition-rate-limited")
	_, err := runTransitionTicket(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureRateLimit)
	provider.postReply = nil
	result, err := runTransitionTicket(t, ctx.nextAttempt(), provider, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchTransitioned, result.Branch)
	require.Equal(t, 2, provider.transitionPosts)
}

func TestTransitionTicketSendsOnTheThirdAttemptAfterTwoRateLimits(t *testing.T) {
	provider := newWorkflowProvider(t, "1")
	provider.postReply = unappliedReply(provider, http.StatusTooManyRequests)
	input := jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionID: "11"}
	ctx := newTestDexContext("transition-two-rate-limits")
	for range 2 {
		_, err := runTransitionTicket(t, ctx, provider, input)
		requireRetry(t, err, sdkgo.FailureRateLimit)
		ctx = ctx.nextAttempt()
	}
	provider.postReply = nil
	result, err := runTransitionTicket(t, ctx, provider, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchTransitioned, result.Branch)
	require.Equal(t, 3, provider.transitionPosts, "Jira processed neither rate-limited send")
}

func TestTransitionTicketSendsOnTheThirdAttemptAfterTwoPreSendRetries(t *testing.T) {
	provider := newWorkflowProvider(t, "1")
	unavailable, err := jiraservicemanagement.New(jiraservicemanagement.Config{CloudID: testCloudID, Endpoint: provider.URL}, unavailableCredentials{})
	require.NoError(t, err)
	input := jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionID: "11"}
	ctx := newTestDexContext("transition-two-pre-send-retries")
	for range 2 {
		_, err = sdkgo.RunMutation(ctx, unavailable.TransitionTicket(), jsmConnection, input)
		requireRetry(t, err, sdkgo.FailureAvailability)
		ctx = ctx.nextAttempt()
	}
	result, err := runTransitionTicket(t, ctx, provider, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchTransitioned, result.Branch)
	require.Equal(t, 1, provider.transitionPosts)
}

// unavailableCredentials fails every lookup, transiently or terminally.
type unavailableCredentials struct{ isTerminal bool }

func (credentials unavailableCredentials) Resolve(sdkgo.Call) (jiraservicemanagement.Credentials, error) {
	if credentials.isTerminal {
		return jiraservicemanagement.Credentials{}, nil
	}
	return jiraservicemanagement.Credentials{}, errors.New("credential broker temporarily unavailable")
}

func (credentials unavailableCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[jiraservicemanagement.Credentials]) (jiraservicemanagement.Credentials, error) {
	return credentials.Resolve(call)
}

func TestTransitionTicketCredentialFailuresFollowTheSendHistory(t *testing.T) {
	newClient := func(isTerminal bool) *jiraservicemanagement.Client {
		client, err := jiraservicemanagement.New(jiraservicemanagement.Config{CloudID: testCloudID, Endpoint: "http://127.0.0.1:1"}, unavailableCredentials{isTerminal: isTerminal})
		require.NoError(t, err)
		return client
	}
	input := jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionID: "11"}
	result, err := sdkgo.RunMutation(newTestDexContext("credential-first"), newClient(true).TransitionTicket(), jsmConnection, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchDefect, result.Branch, "a terminal failure before any send is a defect")
	provider := newWorkflowProvider(t, "1")
	provider.postReply = unappliedReply(provider, http.StatusBadGateway)
	sent := newTestDexContext("credential-later")
	_, err = runTransitionTicket(t, sent, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	later, err := sdkgo.RunMutation(sent.nextAttempt(), newClient(true).TransitionTicket(), jsmConnection, input)
	require.Empty(t, later.Branch, "a terminal failure after a possible send never selects a branch")
	requireRetry(t, err, sdkgo.FailureAuthentication)
	transient := newTestDexContext("credential-transient")
	retried, err := sdkgo.RunMutation(transient, newClient(false).TransitionTicket(), jsmConnection, input)
	require.Empty(t, retried.Branch, retried.Failure)
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Contains(t, string(transient.recordedHeartbeat), `"state":"notSent"`, "a transient failure before any send records notSent")
}

func TestRetriedTransitionTicketAfterALostCheckpointNeverSelectsAgain(t *testing.T) {
	// Pending and Start work toggle, so selecting by name again would undo the move.
	provider := newWorkflowProvider(t, "3")
	provider.postReply = func(response http.ResponseWriter) bool {
		writeJSON(provider.t, response, http.StatusBadGateway, `{}`)
		return true
	}
	input := jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionName: "Pending"}
	ctx := newTestDexContext("transition-lost-checkpoint")
	_, err := runTransitionTicket(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	// Jira applied the transition, and Dex lost the checkpoint.
	provider.extraTransitions = [][3]string{{"22", "Pending", "3"}}
	later := newTestDexContext("transition-lost-checkpoint")
	later.attempt = 2
	_, err = runTransitionTicket(t, later, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, 1, provider.transitionPosts, "a possible send without a checkpoint is never followed by another")

	legacy := newTestDexContext("transition-legacy-not-sent")
	legacy.attempt = 2
	notSent := newWorkflowProvider(t, "1")
	probe := newTestDexContext("transition-legacy-not-sent")
	_, err = runTransitionTicket(t, probe, newWorkflowProvider(t, "5"), jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionID: "51"})
	require.NoError(t, err)
	var probeCheckpoint struct {
		CallID string `json:"callId"`
	}
	require.NoError(t, json.Unmarshal(probe.recordedHeartbeat, &probeCheckpoint))
	legacy.previousHeartbeat = json.RawMessage(fmt.Sprintf(`{"singleDispatchMarkers":{%q:{"callId":%q,"attempt":1,"state":"canResend","sendCount":0}}}`, probeCheckpoint.CallID, probeCheckpoint.CallID))
	result, err := runTransitionTicket(t, legacy, notSent, jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionID: "11"})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchTransitioned, result.Branch, "an earlier release's canResend marker allows a send")
	require.Equal(t, 1, notSent.transitionPosts)
}
