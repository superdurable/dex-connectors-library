// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const labelsAndPriorityJSON = `{"id":"10042","key":"ITH-42","fields":{"labels":["hardware","triage"],"priority":{"id":"3","name":"Medium"}}}`

func TestUpdateTicketSendsOnlyTheLabelOperationsAndPriorityThatDiffer(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, labelsAndPriorityJSON)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	})
	result, err := sdkgo.RunMutation(newTestDexContext("update"), newTestClient(t, provider.URL).UpdateTicket(), jsmConnection, jiraservicemanagement.UpdateTicketInput{
		IssueIDOrKey: "ITH-42", AddLabels: []string{"hardware", "vip"}, RemoveLabels: []string{"triage", "absent"}, PriorityName: "high",
	})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.UpdateTicketBranchUpdated, result.Branch)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, []string{"hardware", "vip"}, result.Value.Labels)
	require.Equal(t, &jiraservicemanagement.PriorityReference{Name: "high"}, result.Value.Priority)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, testPlatformPrefix+"/issue/ITH-42", provider.request(0).path)
	require.Equal(t, "fields=labels%2Cpriority", provider.request(0).rawQuery)
	write := provider.request(1)
	require.Equal(t, http.MethodPut, write.method)
	require.Equal(t, testPlatformPrefix+"/issue/ITH-42", write.path)
	require.JSONEq(t, `{"update":{"labels":[{"remove":"triage"},{"add":"vip"}]},"fields":{"priority":{"name":"high"}}}`, write.body)
}

func TestUpdateTicketWritesNothingWhenTheChangeIsAlreadyApplied(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, labelsAndPriorityJSON)
	})
	result, err := sdkgo.RunMutation(newTestDexContext("update-applied"), newTestClient(t, provider.URL).UpdateTicket(), jsmConnection, jiraservicemanagement.UpdateTicketInput{
		IssueIDOrKey: "ITH-42", AddLabels: []string{"triage"}, RemoveLabels: []string{"vip"}, PriorityName: "MEDIUM",
	})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.UpdateTicketBranchUpdated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, "3", result.Value.Priority.ID)
	require.Equal(t, 1, provider.requestCount(), "a repeated attempt only reads")
}

func TestUpdateTicketRetriesAnUnconfirmedWriteAndRejectsConclusively(t *testing.T) {
	failing := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, labelsAndPriorityJSON)
			return
		}
		dropConnection(t, response)
	})
	_, err := sdkgo.RunMutation(newTestDexContext("update-lost"), newTestClient(t, failing.URL).UpdateTicket(), jsmConnection,
		jiraservicemanagement.UpdateTicketInput{IssueIDOrKey: "ITH-42", AddLabels: []string{"vip"}})
	requireRetry(t, err, sdkgo.FailureTransport)

	rejected := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, labelsAndPriorityJSON)
			return
		}
		writeJSON(t, response, http.StatusBadRequest, `{"errorMessages":[],"errors":{"priority":"SENTINEL Priority name 'Urgentest' is not valid"}}`)
	})
	result, err := sdkgo.RunMutation(newTestDexContext("update-rejected"), newTestClient(t, rejected.URL).UpdateTicket(), jsmConnection,
		jiraservicemanagement.UpdateTicketInput{IssueIDOrKey: "ITH-42", PriorityName: "Urgentest"})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.UpdateTicketBranchProviderRejected, result.Branch)
	require.Equal(t, []string{"priority"}, result.Value.RejectedFieldIDs)
	requireNoSentinel(t, result)
}

func TestUpdateTicketRejectsAnEmptyOrContradictoryChange(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for _, input := range []jiraservicemanagement.UpdateTicketInput{
		{IssueIDOrKey: "ITH-42"},
		{IssueIDOrKey: "ITH-42", AddLabels: []string{"vip"}, RemoveLabels: []string{"vip"}},
		{IssueIDOrKey: "ITH-42", AddLabels: []string{"two words"}},
	} {
		result, err := sdkgo.RunMutation(newTestDexContext("update-invalid"), newTestClient(t, provider.URL).UpdateTicket(), jsmConnection, input)
		require.NoError(t, err)
		require.Equal(t, jiraservicemanagement.UpdateTicketBranchDefect, result.Branch)
	}
}
