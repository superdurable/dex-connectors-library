// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package escalateblocked_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	escalateblocked "github.com/superdurable/dex-connectors-library/connectors/clickup/examples/escalate-blocked-task/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var validSettings = escalateblocked.EscalationSettings{
	WorkspaceID: "9014123456", EscalationListID: "901410000099", ManagerEmail: "manager@acme.example.com", BlockedStatus: "blocked",
}

func TestSettingsValidation(t *testing.T) {
	require.NoError(t, validSettings.Validate())
	for name, change := range map[string]func(*escalateblocked.EscalationSettings){
		"workspace": func(settings *escalateblocked.EscalationSettings) { settings.WorkspaceID = "acme" },
		"list":      func(settings *escalateblocked.EscalationSettings) { settings.EscalationListID = "" },
		"named email": func(settings *escalateblocked.EscalationSettings) {
			settings.ManagerEmail = "Ada <ada@acme.example.com>"
		},
		"blank status":  func(settings *escalateblocked.EscalationSettings) { settings.BlockedStatus = " " },
		"missing email": func(settings *escalateblocked.EscalationSettings) { settings.ManagerEmail = "" },
	} {
		settings := validSettings
		change(&settings)
		require.Error(t, settings.Validate(), name)
	}
}

func TestOnlyAChangeToTheBlockedStatusIsAdmitted(t *testing.T) {
	flow := escalateblocked.NewFlow(clickup.Connection{}, validSettings)
	statusEvent := func(event string, after string) sdkgo.TriggerEvent[clickup.TaskEvent] {
		return sdkgo.TriggerEvent[clickup.TaskEvent]{ID: "wh:" + event + ":1", Payload: clickup.TaskEvent{
			Event: event, TaskID: "86b2x4k7q",
			HistoryItems: []clickup.TaskHistoryItem{{ID: "2800763136717140857", Field: "status", StatusAfter: after}},
		}}
	}
	require.True(t, flow.AcceptBlockedStatusEvent(statusEvent(clickup.EventTaskStatusUpdated, "blocked")))
	require.True(t, flow.AcceptBlockedStatusEvent(statusEvent(clickup.EventTaskStatusUpdated, " Blocked ")), "status names compare without case")
	require.False(t, flow.AcceptBlockedStatusEvent(statusEvent(clickup.EventTaskStatusUpdated, "in review")))
	require.False(t, flow.AcceptBlockedStatusEvent(statusEvent(clickup.EventTaskUpdated, "blocked")), "only taskStatusUpdated starts a Flow")
}

func TestFlowIdentityAndStartInputComeFromTheStatusChange(t *testing.T) {
	occurredAt := time.UnixMilli(1642734631523).UTC()
	event := sdkgo.TriggerEvent[clickup.TaskEvent]{ID: "wh:taskStatusUpdated:2800763136717140857", OccurredAt: occurredAt, Payload: clickup.TaskEvent{
		Event: clickup.EventTaskStatusUpdated, TaskID: "86b2x4k7q",
		HistoryItems: []clickup.TaskHistoryItem{{ID: "2800763136717140857", Field: "status", StatusAfter: "blocked"}},
	}}
	require.Equal(t, escalateblocked.FlowIDPrefix+"86b2x4k7q-2800763136717140857", escalateblocked.ResolveFlowID(event))
	require.Equal(t, escalateblocked.BlockedTask{TaskID: "86b2x4k7q", EventID: event.ID, StatusAfter: "blocked", OccurredAt: &occurredAt},
		escalateblocked.MapToFlowInput(event))
	event.Payload.HistoryItems = nil
	require.Equal(t, escalateblocked.FlowIDPrefix+"86b2x4k7q-86b2x4k7q", escalateblocked.ResolveFlowID(event))
}

func TestEscalationTaskNameCarriesTheBlockedTaskID(t *testing.T) {
	name := escalateblocked.EscalationTaskName("86b2x4k7q", "  Fix login  ")
	require.Equal(t, "Escalation: Fix login [86b2x4k7q]", name)
	existing, isFound := escalateblocked.FindExistingEscalation([]clickup.Task{
		{ID: "a", Name: "Escalation: Fix login [86b2x4k7]"}, {ID: "b", Name: name + " "},
	}, "86b2x4k7q")
	require.True(t, isFound)
	require.Equal(t, "b", existing.ID, "a different task ID that shares a prefix does not match")
	_, isFound = escalateblocked.FindExistingEscalation([]clickup.Task{{ID: "c", Name: "Fix login"}}, "86b2x4k7q")
	require.False(t, isFound)

	long := escalateblocked.EscalationTaskName("86b2x4k7q", strings.Repeat("é", 600))
	require.LessOrEqual(t, len(long), clickup.MaxTaskNameBytes)
	require.True(t, strings.HasSuffix(long, "[86b2x4k7q]"))
}

func TestMappersBuildValidConnectorInputs(t *testing.T) {
	require.Equal(t, clickup.UpdateTaskTagsInput{TaskID: "86b2x4k7q", AddTags: []string{escalateblocked.EscalatedTag}},
		escalateblocked.MapToUpdateTaskTagsInput(escalateblocked.TaskReference{TaskID: "86b2x4k7q"}))
	require.Equal(t, clickup.UpdateTaskInput{TaskID: "86b2x4k7q", Priority: clickup.TaskPriorityUrgent, AddAssigneeIDs: []int64{183}},
		escalateblocked.MapToUpdateTaskInput(escalateblocked.ManagerAssignment{TaskID: "86b2x4k7q", ManagerID: 183}))
	require.Equal(t, clickup.AddCommentInput{TaskID: "86b2x4k7q", Text: "Escalated to Ada: https://app.clickup.com/t/86b2x9"},
		escalateblocked.MapToAddCommentInput(escalateblocked.EscalationComment{
			TaskID: "86b2x4k7q", Text: escalateblocked.EscalationCommentText("Ada", "https://app.clickup.com/t/86b2x9"),
		}))
	require.Equal(t, clickup.GetTaskInput{TaskID: "86b2x4k7q"}, escalateblocked.MapToGetTaskInput(escalateblocked.TaskReference{TaskID: "86b2x4k7q"}))
}
