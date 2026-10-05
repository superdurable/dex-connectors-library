// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetTaskReturnsDescriptionAndCustomFieldValues(t *testing.T) {
	task := taskMap(testTask{id: testTaskID, name: "Fix login", status: "blocked", parent: "86b2x4k00", assigneeIDs: []int64{183}})
	task["custom_id"] = "SUP-42"
	task["markdown_description"] = "**Customer** cannot sign in."
	task["description"] = "Customer cannot sign in."
	task["priority"] = nil
	task["custom_fields"] = []any{
		map[string]any{"id": "d2ab17e0-41d3-4d28-bdbb-f3f0f2c62f8b", "name": "Severity", "type": "drop_down", "type_config": map[string]any{}, "value": 1},
		map[string]any{"id": "1f3cc395-3448-4264-bedd-590a3b3fa178", "name": "Story Points", "type": "number", "value": "8"},
		map[string]any{"id": "f4d2a20d-6759-4420-b897-222dbe2587d4", "name": "Reviewer", "type": "users", "value": nil},
		map[string]any{"id": "b2e4be70-c4d3-4726-985b-46ac25375b28", "name": "Notes", "type": "text"},
	}
	encoded, err := json.Marshal(task)
	require.NoError(t, err)
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, string(encoded))
	})

	result, err := sdkgo.RunQuery(newTestDexContext("get"), newClickUpClient(t, provider.URL).GetTask(), clickupConnection,
		clickup.GetTaskInput{TaskID: "SUP-42", IsCustomTaskID: true, WorkspaceID: testWorkspaceID})
	require.NoError(t, err)
	require.Equal(t, clickup.GetTaskBranchFound, result.Branch)
	require.Equal(t, "/task/SUP-42?custom_task_ids=true&include_markdown_description=true&team_id="+testWorkspaceID, provider.request(0).path)
	value := result.Value
	require.Equal(t, testTaskID, value.ID)
	require.Equal(t, "SUP-42", value.CustomID)
	require.Equal(t, clickup.TaskStatus{Name: "blocked", Type: "open"}, value.Status)
	require.Zero(t, value.Priority, "a null priority is no priority")
	require.Equal(t, "86b2x4k00", value.ParentTaskID)
	require.Equal(t, "**Customer** cannot sign in.", value.MarkdownDescription)
	require.Equal(t, time.UnixMilli(1767484800000).UTC(), *value.DueAt)
	require.Equal(t, time.UnixMilli(1767225600000).UTC(), value.CreatedAt)
	require.Equal(t, testWorkspaceID, value.WorkspaceID)
	require.Equal(t, "Escalations", value.ListName)
	require.Equal(t, &clickup.User{ID: 4411723, Username: "Dex Bot"}, value.Creator)
	require.Len(t, value.CustomFields, 4)
	require.JSONEq(t, `1`, string(value.CustomFields[0].Value))
	require.JSONEq(t, `"8"`, string(value.CustomFields[1].Value))
	require.Nil(t, value.CustomFields[2].Value, "an unset field has no value")
	require.Nil(t, value.CustomFields[3].Value)
	require.Equal(t, testTaskID, result.Receipt.ProviderObjectID)
}

func TestGetTaskTruncatesALongDescriptionAtARuneBoundary(t *testing.T) {
	task := taskMap(testTask{id: testTaskID, name: "Long"})
	task["markdown_description"] = strings.Repeat("é", clickup.MaxDescriptionBytes)
	encoded, err := json.Marshal(task)
	require.NoError(t, err)
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, string(encoded))
	})
	result, err := sdkgo.RunQuery(newTestDexContext("long"), newClickUpClient(t, provider.URL).GetTask(), clickupConnection, clickup.GetTaskInput{TaskID: testTaskID})
	require.NoError(t, err)
	require.True(t, result.Value.IsDescriptionTruncated)
	require.Len(t, result.Value.MarkdownDescription, clickup.MaxDescriptionBytes)
	require.True(t, strings.HasSuffix(result.Value.MarkdownDescription, "é"))
	require.Equal(t, "/task/"+testTaskID+"?include_markdown_description=true", provider.request(0).path)
}

func TestGetTaskValidatesTheTaskReference(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	for name, input := range map[string]clickup.GetTaskInput{
		"empty":                        {},
		"path":                         {TaskID: "../user"},
		"custom without workspace":     {TaskID: "SUP-42", IsCustomTaskID: true},
		"workspace without custom IDs": {TaskID: testTaskID, WorkspaceID: testWorkspaceID},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(newTestDexContext("invalid-"+name), newClickUpClient(t, provider.URL).GetTask(), clickupConnection, input)
			require.NoError(t, err)
			require.Equal(t, clickup.GetTaskBranchDefect, result.Branch)
		})
	}
	require.Zero(t, provider.requestCount())
}
