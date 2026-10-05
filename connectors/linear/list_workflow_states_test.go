// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
)

func workflowStateJSON(id string, name string, stateType string, position float64, teamID string) map[string]any {
	return map[string]any{"id": id, "name": name, "type": stateType, "position": position, "color": "#bec2c8", "team": map[string]any{"id": teamID}}
}

func TestListWorkflowStatesOrdersStatesFromIntakeToClosure(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"workflowStates": map[string]any{"nodes": []any{
			workflowStateJSON("11111111-1111-4111-8111-111111111111", "Done", "completed", 0, testTeamID),
			workflowStateJSON("22222222-2222-4222-8222-222222222222", "In Review", "started", 2, testTeamID),
			workflowStateJSON("33333333-3333-4333-8333-333333333333", "In Progress", "started", 1, testTeamID),
			workflowStateJSON("44444444-4444-4444-8444-444444444444", "Backlog", "backlog", 0, testTeamID),
			workflowStateJSON("55555555-5555-4555-8555-555555555555", "Parked", "someday", 0, testTeamID),
		}}})
	})
	result, err := runQuery("states", newAPIKeyClient(t, provider.URL).ListWorkflowStates(), linear.ListWorkflowStatesInput{TeamID: testTeamID})
	require.NoError(t, err)
	require.Equal(t, linear.ListWorkflowStatesBranchListed, result.Branch)
	names := []string{}
	for _, state := range result.Value.States {
		names = append(names, state.Name)
	}
	require.Equal(t, []string{"Backlog", "In Progress", "In Review", "Done", "Parked"}, names, "an unknown type is passed through last")
	inReview, isFound := result.Value.StateNamed("in review")
	require.True(t, isFound)
	require.Equal(t, "22222222-2222-4222-8222-222222222222", inReview.ID)
	started, isFound := result.Value.FirstStateOfType(linear.WorkflowStateTypeStarted)
	require.True(t, isFound)
	require.Equal(t, "In Progress", started.Name)
	_, isFound = result.Value.FirstStateOfType(linear.WorkflowStateTypeTriage)
	require.False(t, isFound)
	require.Equal(t, map[string]any{"teamId": testTeamID}, provider.request(0).variables)
}

func TestListWorkflowStatesSelectsNotFoundForAnUnknownTeamAndRejectsAnotherTeam(t *testing.T) {
	answers := []any{
		map[string]any{"workflowStates": map[string]any{"nodes": []any{}}},
		map[string]any{"workflowStates": map[string]any{"nodes": []any{
			workflowStateJSON(testStateID, "Todo", "unstarted", 0, "99999999-9999-4999-8999-999999999999"),
		}}},
	}
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		writeData(t, response, answers[index])
	})
	client := newAPIKeyClient(t, provider.URL)
	missing, err := runQuery("missing", client.ListWorkflowStates(), linear.ListWorkflowStatesInput{TeamID: testTeamID})
	require.NoError(t, err)
	require.Equal(t, linear.ListWorkflowStatesBranchNotFound, missing.Branch)
	other, err := runQuery("other", client.ListWorkflowStates(), linear.ListWorkflowStatesInput{TeamID: testTeamID})
	require.NoError(t, err)
	require.Equal(t, linear.ListWorkflowStatesBranchInvalidResponse, other.Branch)
	invalid, err := runQuery("invalid", client.ListWorkflowStates(), linear.ListWorkflowStatesInput{TeamID: "ENG"})
	require.NoError(t, err)
	require.Equal(t, linear.ListWorkflowStatesBranchDefect, invalid.Branch)
	require.Equal(t, 2, provider.requestCount())
}
