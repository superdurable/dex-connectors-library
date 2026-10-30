// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// workflowJira fakes one issue on a To Do -> In Progress -> Done workflow.
type workflowJira struct {
	*recordingJira
	mutex             sync.Mutex
	statusID          string
	transitionPosts   int
	postResponse      func(http.ResponseWriter, int) bool
	hasGlobalDoneLoop bool
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
		writeJSON(t, response, http.StatusOK, provider.issueWithTransitionsJSON())
		return
	}
	provider.transitionPosts++
	if provider.postResponse != nil && provider.postResponse(response, provider.transitionPosts) {
		return
	}
	var body struct {
		Transition struct {
			ID string `json:"id"`
		} `json:"transition"`
	}
	require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
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
	transitions := workflowTransitions[provider.statusID]
	if provider.hasGlobalDoneLoop && provider.statusID == "5" {
		transitions = append(transitions, [3]string{"31", "Done", "5"})
	}
	return transitions
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

func TestAmbiguousTransitionFailureRereadsBeforeRetrying(t *testing.T) {
	for _, test := range []struct {
		name          string
		status        int
		appliesAnyway bool
		kind          sdkgo.FailureKind
	}{
		{name: "server error before applying", status: http.StatusInternalServerError, kind: sdkgo.FailureAvailability},
		{name: "server error after applying", status: http.StatusInternalServerError, appliesAnyway: true},
		{name: "conflicting update", status: http.StatusConflict, kind: sdkgo.FailureConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newWorkflowJira(t, "1")
			provider.postResponse = func(response http.ResponseWriter, _ int) bool {
				if test.appliesAnyway {
					provider.statusID = "3"
				}
				writeJSON(provider.t, response, test.status, `{"errorMessages":["SENTINEL"]}`)
				return true
			}
			result, err := runTransition(t, provider, jira.TransitionIssueInput{IssueIDOrKey: "OPS-42", TransitionID: "11"})
			require.Equal(t, 3, provider.requestCount(), "read, transition, read back")
			if test.appliesAnyway {
				require.NoError(t, err)
				require.Equal(t, jira.TransitionIssueBranchTransitioned, result.Branch)
				return
			}
			requireRetry(t, err, test.kind)
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
