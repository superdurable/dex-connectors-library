// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskchanged

import (
	"testing"

	"github.com/stretchr/testify/require"
	asana "github.com/superdurable/dex-connectors-library/connectors/asana/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(action asana.TaskChangeAction) sdkgo.TriggerEvent[asana.TaskChangedEvent] {
	return sdkgo.TriggerEvent[asana.TaskChangedEvent]{
		ID: "asana-0123456789abcdef0123456789abcdef",
		Payload: asana.TaskChangedEvent{
			ProjectID: "1201000000000001", Action: action, ChangedField: "completed",
			Task: asana.TaskReference{ID: "1204000000000002", Name: "Replace badge reader"},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerTaskEvent(t *testing.T) {
	event := sampleTriggerEvent(asana.TaskChangeChanged)
	require.Equal(t, "asana-task-change-asana-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryTaskChange(event))
	require.Equal(t, asana.GetTaskInput{TaskID: "1204000000000002"}, MapToGetTaskInput(input))
	require.Equal(t, ChangeOutcome{ProjectID: "1201000000000001", TaskID: "1204000000000002", Action: asana.TaskChangeChanged, ChangedField: "completed"},
		newChangeOutcome(event.Payload, nil))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	for _, action := range []asana.TaskChangeAction{asana.TaskChangeAdded, asana.TaskChangeChanged, asana.TaskChangeRemoved, asana.TaskChangeDeleted, asana.TaskChangeUndeleted} {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(action))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(asana.TaskChangeAdded))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingTask := MapToFlowInput(sampleTriggerEvent(asana.TaskChangeAdded))
	missingTask.Change.Task.ID = ""
	require.ErrorContains(t, validateInput(missingTask), "change.task.id")
	require.ErrorContains(t, validateInput(MapToFlowInput(sampleTriggerEvent("moved"))), "change.action")
}

func TestTriggerBindingShowsOneGuidedRequiredProjectPicker(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, asana.TaskChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, TaskChangedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 1, "the picker is the only visible Trigger field")
	unit := binding.ConfigurationUI.Units[0]
	require.Equal(t, asana.UIUnitProjectPicker, unit.UnitID)
	require.Equal(t, "Watched project", unit.Label)
	require.True(t, unit.Required, "the Events API needs one project")
	for _, guidance := range []string{"workspace ID, project ID, and project name", "/project/", "no blank default", "subtasks", "baseline", "Restart the Worker"} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{
		{Port: asana.UIProjectPickerPortWorkspaceID, JSONPointer: "/workspaceId"},
		{Port: asana.UIProjectPickerPortProjectID, JSONPointer: "/projectId"},
		{Port: asana.UIProjectPickerPortProjectName, JSONPointer: "/projectName"},
	}, unit.Bindings, "the section output is not bound: the Trigger watches the whole project")
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(asana.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordTaskChangeStepType, dex.GetFinalStepType[Input](recordTaskChange{}))
	require.Equal(t, completeTaskChangeStepType, dex.GetFinalStepType[asana.GetTaskResult](completeTaskChange{}))
	wait, err := recordTaskChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := asana.New(asana.Config{}, sdkgo.StaticCredentialProvider[asana.Credentials]{})
	require.NoError(t, err)
	connection, err := asana.NewConnection(client, sdkgo.ConnectionRef{Provider: "asana", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
