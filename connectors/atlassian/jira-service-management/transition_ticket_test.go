// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management"
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
