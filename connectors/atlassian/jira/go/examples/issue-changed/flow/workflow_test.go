// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package issuechanged

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(kind jira.IssueChangeKind) sdkgo.TriggerEvent[jira.IssueChangedEvent] {
	return sdkgo.TriggerEvent[jira.IssueChangedEvent]{
		ID: "jira-0123456789abcdef0123456789abcdef",
		Payload: jira.IssueChangedEvent{
			CloudID: "1324a887-45db-1bf4-1e99-ef0ff456d421", ChangeKind: kind,
			Issue: jira.Issue{ID: "10004", Key: "OPS-4", Summary: "Loading dock light out"},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerIssueVersion(t *testing.T) {
	event := sampleTriggerEvent(jira.IssueChangeUpdated)
	require.Equal(t, "jira-issue-change-jira-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryIssueChange(event))
	require.Equal(t, jira.GetIssueInput{IssueIDOrKey: "10004"}, MapToGetIssueInput(input), "the stable ID survives a project move")
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	for _, kind := range []jira.IssueChangeKind{jira.IssueChangeCreated, jira.IssueChangeUpdated} {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(kind))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(jira.IssueChangeCreated))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingIssue := MapToFlowInput(sampleTriggerEvent(jira.IssueChangeCreated))
	missingIssue.Change.Issue.ID = ""
	require.ErrorContains(t, validateInput(missingIssue), "change.issue.id")
	unknownKind := MapToFlowInput(sampleTriggerEvent("deleted"))
	require.ErrorContains(t, validateInput(unknownKind), "changeKind")
}

func TestTriggerBindingShowsOneGuidedProjectPicker(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, jira.IssueChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, IssueChangedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 1, "the picker is the only visible Trigger field")
	unit := binding.ConfigurationUI.Units[0]
	require.Equal(t, jira.UIUnitProjectPicker, unit.UnitID)
	require.Equal(t, "Watched project", unit.Label)
	require.False(t, unit.Required, "an unsaved picker watches every browsable project")
	for _, guidance := range []string{"Jira site", "account can browse", "project ID, key, and name", "unsaved", "baseline", "Restart the Worker"} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{
		{Port: jira.UIProjectPickerPortProjectID, JSONPointer: "/projectId"},
		{Port: jira.UIProjectPickerPortProjectKey, JSONPointer: "/projectKey"},
		{Port: jira.UIProjectPickerPortProjectName, JSONPointer: "/projectName"},
	}, unit.Bindings, "the saved JSON decodes as jira.IssueChangedTriggerConfiguration")
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(jira.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordIssueChangeStepType, dex.GetFinalStepType[Input](recordIssueChange{}))
	require.Equal(t, completeIssueChangeStepType, dex.GetFinalStepType[jira.GetIssueResult](completeIssueChange{}))
	wait, err := recordIssueChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := jira.New(jira.Config{}, sdkgo.StaticCredentialProvider[jira.Credentials]{})
	require.NoError(t, err)
	connection, err := jira.NewConnection(client, sdkgo.ConnectionRef{Provider: "atlassian", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
