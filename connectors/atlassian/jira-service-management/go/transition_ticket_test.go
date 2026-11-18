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
	mutex           sync.Mutex
	statusID        string
	transitionPosts int
	postReply       func(http.ResponseWriter) bool
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
	if request.Method == http.MethodGet {
		var transitions []string
		for _, transition := range workflowTransitions[provider.statusID] {
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
	for _, transition := range workflowTransitions[provider.statusID] {
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

func TestTransitionTicketReadsBackAnAmbiguousResponse(t *testing.T) {
	applied := newWorkflowProvider(t, "1")
	applied.postReply = func(response http.ResponseWriter) bool {
		writeJSON(t, response, http.StatusInternalServerError, `{"errorMessages":["SENTINEL internal"]}`)
		return true
	}
	result, err := sdkgo.RunMutation(newTestDexContext("transition-ambiguous"), newTestClient(t, applied.URL).TransitionTicket(), jsmConnection,
		jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionName: "start work"})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.TransitionTicketBranchTransitioned, result.Branch, "the read-back found the move applied")
	require.Equal(t, 3, applied.requestCount())

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

func TestTransitionTicketAfterAnUnknownOutcomeIsNeverSentAgain(t *testing.T) {
	provider := newWorkflowProvider(t, "1")
	provider.postReply = unappliedReply(provider, http.StatusBadGateway)
	input := jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionName: "Start work"}
	ctx := newTestDexContext("transition-unknown")
	_, err := runTransitionTicket(t, ctx, provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	for range 2 {
		ctx = ctx.nextAttempt()
		_, err = runTransitionTicket(t, ctx, provider, input)
		requireRetry(t, err, sdkgo.FailureAvailability)
	}
	require.Equal(t, 1, provider.transitionPosts, "a possible earlier transition is never sent again")
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

func TestRepeatedTransitionTicketNeverSelectsAFailureBranchAfterAPossibleSend(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden} {
		provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) { writeJSON(t, response, status, `{}`) })
		// A later attempt without a checkpoint assumes the earlier one sent.
		ctx := newTestDexContext("transition-repeat").nextAttempt()
		_, err := sdkgo.RunMutation(ctx, newTestClient(t, provider.URL).TransitionTicket(), jsmConnection,
			jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", TransitionID: "11"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "HTTP %d", status)
		require.Equal(t, 1, provider.requestCount())
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
	_, err = runTransitionTicket(t, ctx.nextAttempt(), provider, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, 1, provider.transitionPosts, "a cut refusal body cannot prove that nothing moved")
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
	later, err := sdkgo.RunMutation(newTestDexContext("credential-later").nextAttempt(), newClient(true).TransitionTicket(), jsmConnection, input)
	require.Empty(t, later.Branch, "a terminal failure after a possible send never selects a branch")
	requireRetry(t, err, sdkgo.FailureAuthentication)
	transient := newTestDexContext("credential-transient")
	retried, err := sdkgo.RunMutation(transient, newClient(false).TransitionTicket(), jsmConnection, input)
	require.Empty(t, retried.Branch, retried.Failure)
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Contains(t, string(transient.recordedHeartbeat), `"notSent"`, "a transient failure before any send records notSent")
}
