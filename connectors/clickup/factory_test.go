// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
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
	connection := newTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "clickup", GroupLabel: "ClickUp", Explanation: "Call ClickUp."}
	name := clickupConnection.Name
	require.NotPanics(t, func() {
		clickup.NewSearchTasksStep(clickup.SearchTasksStepConfig[string]{
			StepType: "Search", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(id string) clickup.SearchTasksInput { return clickup.SearchTasksInput{WorkspaceID: id} },
			Searched:            sdkgo.GoTo(completeTarget[clickup.SearchTasksResult]{}),
		})
		clickup.NewGetTaskStep(clickup.GetTaskStepConfig[string]{
			StepType: "Get", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(id string) clickup.GetTaskInput { return clickup.GetTaskInput{TaskID: id} },
			Found:               sdkgo.GoTo(completeTarget[clickup.GetTaskResult]{}),
		})
		clickup.NewCreateTaskStep(clickup.CreateTaskStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) clickup.CreateTaskInput { return validCreateTaskInput() },
			Created:             sdkgo.GoTo(completeTarget[clickup.CreateTaskResult]{}),
		})
		clickup.NewUpdateTaskStep(clickup.UpdateTaskStepConfig[string]{
			StepType: "Update", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(id string) clickup.UpdateTaskInput { return clickup.UpdateTaskInput{TaskID: id, Status: "open"} },
			Updated:             sdkgo.GoTo(completeTarget[clickup.UpdateTaskResult]{}),
		})
		clickup.NewUpdateTaskTagsStep(clickup.UpdateTaskTagsStepConfig[string]{
			StepType: "Tag", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(id string) clickup.UpdateTaskTagsInput {
				return clickup.UpdateTaskTagsInput{TaskID: id, AddTags: []string{"a"}}
			},
			Updated: sdkgo.GoTo(completeTarget[clickup.UpdateTaskTagsResult]{}),
		})
		clickup.NewAddCommentStep(clickup.AddCommentStepConfig[string]{
			StepType: "Comment", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(id string) clickup.AddCommentInput { return clickup.AddCommentInput{TaskID: id, Text: "Hi"} },
			Added:               sdkgo.GoTo(completeTarget[clickup.AddCommentResult]{}),
		})
		clickup.NewFindMemberByEmailStep(clickup.FindMemberByEmailStepConfig[string]{
			StepType: "Member", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(email string) clickup.FindMemberByEmailInput {
				return clickup.FindMemberByEmailInput{WorkspaceID: testWorkspaceID, Email: email}
			},
			Found: sdkgo.GoTo(completeTarget[clickup.FindMemberByEmailResult]{}),
		})
	})
	require.Panics(t, func() {
		clickup.NewCreateTaskStep(clickup.CreateTaskStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) clickup.CreateTaskInput { return validCreateTaskInput() },
			Uncertain:           sdkgo.GoTo(completeTarget[clickup.CreateTaskResult]{}),
		})
	}, "created is the required branch")
	require.Panics(t, func() {
		clickup.NewGetTaskStep(clickup.GetTaskStepConfig[string]{
			StepType: "Get", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(id string) clickup.GetTaskInput { return clickup.GetTaskInput{TaskID: id} },
			Found:               sdkgo.GoTo(completeTarget[clickup.GetTaskResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndDurability(t *testing.T) {
	readBranches := map[sdkgo.BranchID]bool{"notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true}
	require.Equal(t, withHappyBranch(readBranches, "searched"), branchOptionality(clickup.SearchTasksDefinition.Branches))
	require.Equal(t, withHappyBranch(readBranches, "found"), branchOptionality(clickup.GetTaskDefinition.Branches))
	require.Equal(t, withHappyBranch(readBranches, "found"), branchOptionality(clickup.FindMemberByEmailDefinition.Branches))
	require.Equal(t, withHappyBranch(readBranches, "updated"), branchOptionality(clickup.UpdateTaskDefinition.Branches), "a repeated update is safe, so it is never uncertain")
	require.Equal(t, withHappyBranch(readBranches, "updated"), branchOptionality(clickup.UpdateTaskTagsDefinition.Branches))
	unkeyedWriteBranches := map[sdkgo.BranchID]bool{"notFound": true, "providerRejected": true, "uncertain": true, "defect": true}
	require.Equal(t, withHappyBranch(unkeyedWriteBranches, "created"), branchOptionality(clickup.CreateTaskDefinition.Branches))
	require.Equal(t, withHappyBranch(unkeyedWriteBranches, "added"), branchOptionality(clickup.AddCommentDefinition.Branches))
	for _, defaults := range []sdkgo.StepDefaults{clickup.CreateTaskDefinition.StepDefaults, clickup.AddCommentDefinition.StepDefaults} {
		require.Equal(t, dex.StepDurabilitySync, defaults.ExecuteDurability, "sync keeps Dex from dispatching a second create while the first is in flight")
	}
	for _, defaults := range []sdkgo.StepDefaults{
		clickup.SearchTasksDefinition.StepDefaults, clickup.GetTaskDefinition.StepDefaults, clickup.UpdateTaskDefinition.StepDefaults,
		clickup.UpdateTaskTagsDefinition.StepDefaults, clickup.FindMemberByEmailDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability)
	}
	for _, defaults := range []sdkgo.StepDefaults{
		clickup.SearchTasksDefinition.StepDefaults, clickup.CreateTaskDefinition.StepDefaults, clickup.AddCommentDefinition.StepDefaults,
	} {
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout)
		require.Greater(t, defaults.ExecuteRetry.TotalDuration, time.Minute, "a one-minute rate-limit wait must fit in the window")
	}
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	credentials := clickup.Credentials{APIToken: sdkgo.NewSecretString(testAPIToken), WebhookSecret: sdkgo.NewSecretString(testWebhookSecret)}
	formatted := fmt.Sprintf("%v %#v %v %#v", connection, connection, credentials, credentials)
	require.NotContains(t, formatted, testAPIToken)
	require.NotContains(t, formatted, testWebhookSecret)
	_, err = json.Marshal(credentials.APIToken)
	require.Error(t, err)
}

func branchOptionality(branches []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	optionality := make(map[sdkgo.BranchID]bool, len(branches))
	for _, branch := range branches {
		optionality[branch.ID] = branch.Optional
	}
	return optionality
}

func withHappyBranch(optional map[sdkgo.BranchID]bool, happy sdkgo.BranchID) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{happy: false}
	for branch, isOptional := range optional {
		branches[branch] = isOptional
	}
	return branches
}
