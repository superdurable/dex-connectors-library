// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type createdIssueTarget struct {
	dex.StepDefaultsNoWaitFor[jira.CreateIssueResult]
}

func (createdIssueTarget) Execute(dex.Context, jira.CreateIssueResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestCreateIssueFactoryRequiresOnlyTheHappyPath(t *testing.T) {
	connection, err := jira.NewConnection(newJiraClient(t, "http://127.0.0.1:1"), jiraConnection)
	require.NoError(t, err)
	annotations := sdkgo.StepAnnotations{GroupID: "jira", GroupLabel: "Jira", Explanation: "Create an issue."}
	mapToInput := func(string) jira.CreateIssueInput { return jira.CreateIssueInput{} }
	require.NotPanics(t, func() {
		jira.NewCreateIssueStep(jira.CreateIssueStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: jiraConnection.Name,
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdIssueTarget{}),
		})
	})
	require.Panics(t, func() {
		jira.NewCreateIssueStep(jira.CreateIssueStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection,
			MapToOperationInput: mapToInput, Uncertain: sdkgo.GoTo(createdIssueTarget{}),
		})
	})
	require.Panics(t, func() {
		jira.NewCreateIssueStep(jira.CreateIssueStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdIssueTarget{}),
		})
	})
}

// TestOnlyUnkeyedWritesAreUncertainAndSync records the duplicate-dispatch decision per operation.
func TestOnlyUnkeyedWritesAreUncertainAndSync(t *testing.T) {
	for _, test := range []struct {
		operation    string
		branches     []sdkgo.BranchDefinition
		defaults     sdkgo.StepDefaults
		hasUncertain bool
	}{
		{"searchIssues", jira.SearchIssuesDefinition.Branches, jira.SearchIssuesDefinition.StepDefaults, false},
		{"getIssue", jira.GetIssueDefinition.Branches, jira.GetIssueDefinition.StepDefaults, false},
		{"createIssue", jira.CreateIssueDefinition.Branches, jira.CreateIssueDefinition.StepDefaults, true},
		{"transitionIssue", jira.TransitionIssueDefinition.Branches, jira.TransitionIssueDefinition.StepDefaults, false},
		{"addComment", jira.AddCommentDefinition.Branches, jira.AddCommentDefinition.StepDefaults, true},
	} {
		required, hasUncertain := 0, false
		for _, branch := range test.branches {
			if branch.ID == sdkgo.UncertainBranchID {
				hasUncertain = true
			}
			if !branch.Optional {
				required++
			}
		}
		require.Equal(t, 1, required, test.operation)
		require.Equal(t, test.hasUncertain, hasUncertain, test.operation)
		expectedDurability := dex.StepDurabilityAsync
		if test.hasUncertain {
			expectedDurability = dex.StepDurabilitySync
		}
		require.Equal(t, expectedDurability, test.defaults.ExecuteDurability, "%s: an async fallback attempt would resend an unkeyed write", test.operation)
	}
}

func TestJiraConnectionCannotBeSerialized(t *testing.T) {
	connection, err := jira.NewConnection(newJiraClient(t, "http://127.0.0.1:1"), jiraConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.Equal(t, "jira.Connection{[REDACTED]}", fmt.Sprint(connection))
}
