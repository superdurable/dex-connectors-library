// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type createdTaskTarget struct {
	dex.StepDefaultsNoWaitFor[asana.CreateTaskResult]
}

func (createdTaskTarget) Execute(dex.Context, asana.CreateTaskResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestCreateTaskFactoryRequiresOnlyTheHappyPath(t *testing.T) {
	connection, err := asana.NewConnection(newAsanaClient(t, "http://127.0.0.1:1"), asanaConnection)
	require.NoError(t, err)
	annotations := sdkgo.StepAnnotations{GroupID: "asana", GroupLabel: "Asana", Explanation: "Create a task."}
	mapToInput := func(string) asana.CreateTaskInput { return asana.CreateTaskInput{} }
	require.NotPanics(t, func() {
		asana.NewCreateTaskStep(asana.CreateTaskStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: asanaConnection.Name,
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdTaskTarget{}),
		})
	})
	require.Panics(t, func() {
		asana.NewCreateTaskStep(asana.CreateTaskStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection,
			MapToOperationInput: mapToInput, Uncertain: sdkgo.GoTo(createdTaskTarget{}),
		})
	})
	require.Panics(t, func() {
		asana.NewCreateTaskStep(asana.CreateTaskStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: mapToInput, Created: sdkgo.GoTo(createdTaskTarget{}),
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
		{"listTasks", asana.ListTasksDefinition.Branches, asana.ListTasksDefinition.StepDefaults, false},
		{"getTask", asana.GetTaskDefinition.Branches, asana.GetTaskDefinition.StepDefaults, false},
		{"createTask", asana.CreateTaskDefinition.Branches, asana.CreateTaskDefinition.StepDefaults, true},
		{"updateTask", asana.UpdateTaskDefinition.Branches, asana.UpdateTaskDefinition.StepDefaults, false},
		{"addComment", asana.AddCommentDefinition.Branches, asana.AddCommentDefinition.StepDefaults, true},
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

func TestAsanaConnectionCannotBeSerialized(t *testing.T) {
	connection, err := asana.NewConnection(newAsanaClient(t, "http://127.0.0.1:1"), asanaConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.Equal(t, "asana.Connection{[REDACTED]}", fmt.Sprint(connection))
}
