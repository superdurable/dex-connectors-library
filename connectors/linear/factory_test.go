// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type completeTarget[IN any] struct {
	dex.StepDefaultsNoWaitFor[IN]
}

func (completeTarget[IN]) Execute(dex.Context, IN) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

// GetStepType names the target: Dex gives a generic Step type no default name.
func (completeTarget[IN]) GetStepType() string { return "Complete" }

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection, err := linear.NewConnection(newAPIKeyClient(t, "http://127.0.0.1:1"), linearConnection)
	require.NoError(t, err)
	annotations := sdkgo.StepAnnotations{GroupID: "linear", GroupLabel: "Linear", Explanation: "Call Linear."}
	name := linearConnection.Name
	require.NotPanics(t, func() {
		linear.NewSearchIssuesStep(linear.SearchIssuesStepConfig[string]{
			StepType: "Search", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) linear.SearchIssuesInput { return linear.SearchIssuesInput{} },
			Searched:            sdkgo.GoTo(completeTarget[linear.SearchIssuesResult]{}),
		})
		linear.NewGetIssueStep(linear.GetIssueStepConfig[string]{
			StepType: "Get", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(issueID string) linear.GetIssueInput { return linear.GetIssueInput{IssueID: issueID} },
			Found:               sdkgo.GoTo(completeTarget[linear.GetIssueResult]{}),
		})
		linear.NewCreateIssueStep(linear.CreateIssueStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) linear.CreateIssueInput { return validCreateIssueInput() },
			Created:             sdkgo.GoTo(completeTarget[linear.CreateIssueResult]{}),
		})
		linear.NewUpdateIssueStep(linear.UpdateIssueStepConfig[string]{
			StepType: "Update", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(issueID string) linear.UpdateIssueInput {
				return linear.UpdateIssueInput{IssueID: issueID, StateID: testStateID}
			},
			Updated: sdkgo.GoTo(completeTarget[linear.UpdateIssueResult]{}),
		})
		linear.NewAddCommentStep(linear.AddCommentStepConfig[string]{
			StepType: "Comment", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(issueID string) linear.AddCommentInput {
				return linear.AddCommentInput{IssueID: issueID, Body: "Done."}
			},
			Added: sdkgo.GoTo(completeTarget[linear.AddCommentResult]{}),
		})
		linear.NewFindUserByEmailStep(linear.FindUserByEmailStepConfig[string]{
			StepType: "Find", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(email string) linear.FindUserByEmailInput { return linear.FindUserByEmailInput{Email: email} },
			Found:               sdkgo.GoTo(completeTarget[linear.FindUserByEmailResult]{}),
		})
		linear.NewListWorkflowStatesStep(linear.ListWorkflowStatesStepConfig[string]{
			StepType: "States", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(teamID string) linear.ListWorkflowStatesInput {
				return linear.ListWorkflowStatesInput{TeamID: teamID}
			},
			Listed: sdkgo.GoTo(completeTarget[linear.ListWorkflowStatesResult]{}),
		})
	})
	require.Panics(t, func() {
		linear.NewCreateIssueStep(linear.CreateIssueStepConfig[string]{
			StepType: "Create", Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) linear.CreateIssueInput { return validCreateIssueInput() },
			ProviderRejected:    sdkgo.GoTo(completeTarget[linear.CreateIssueResult]{}),
		})
	}, "created is the required branch")
	require.Panics(t, func() {
		linear.NewGetIssueStep(linear.GetIssueStepConfig[string]{
			StepType: "Get", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(issueID string) linear.GetIssueInput { return linear.GetIssueInput{IssueID: issueID} },
			Found:               sdkgo.GoTo(completeTarget[linear.GetIssueResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

// TestDefinitionsAreAsyncWithOnlyTheHappyPathRequired documents the duplicate-safety choice: every write
// either carries a client-supplied UUID or sets absolute values, so none needs sync durability or uncertain.
func TestDefinitionsAreAsyncWithOnlyTheHappyPathRequired(t *testing.T) {
	type definition struct {
		branches []sdkgo.BranchDefinition
		defaults sdkgo.StepDefaults
	}
	definitions := map[string]definition{
		"searchIssues":       {linear.SearchIssuesDefinition.Branches, linear.SearchIssuesDefinition.StepDefaults},
		"getIssue":           {linear.GetIssueDefinition.Branches, linear.GetIssueDefinition.StepDefaults},
		"findUserByEmail":    {linear.FindUserByEmailDefinition.Branches, linear.FindUserByEmailDefinition.StepDefaults},
		"listWorkflowStates": {linear.ListWorkflowStatesDefinition.Branches, linear.ListWorkflowStatesDefinition.StepDefaults},
		"createIssue":        {linear.CreateIssueDefinition.Branches, linear.CreateIssueDefinition.StepDefaults},
		"updateIssue":        {linear.UpdateIssueDefinition.Branches, linear.UpdateIssueDefinition.StepDefaults},
		"addComment":         {linear.AddCommentDefinition.Branches, linear.AddCommentDefinition.StepDefaults},
	}
	for operation, definition := range definitions {
		required := 0
		for _, branch := range definition.branches {
			require.NotEqual(t, sdkgo.BranchID("uncertain"), branch.ID, "%s never leaves an unknown outcome", operation)
			if !branch.Optional {
				required++
			}
		}
		require.Equal(t, 1, required, operation)
		require.Equal(t, dex.StepDurabilityAsync, definition.defaults.ExecuteDurability, operation)
		require.Equal(t, 30*time.Second, definition.defaults.ExecuteMethodTimeout, operation)
		require.Equal(t, 5*time.Minute, definition.defaults.ExecuteRetry.TotalDuration, operation)
	}
}
