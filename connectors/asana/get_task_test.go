// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetTaskReturnsMembershipsNotesAndCustomFields(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":`+taskJSON(testTaskID, "[REQ-1042] Replace badge reader", true, testSectionID)+`}`)
	})
	client := newAsanaClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newAsanaDexContext("get"), client.GetTask(), asanaConnection, asana.GetTaskInput{TaskID: " " + testTaskID + " "})
	require.NoError(t, err)
	require.Equal(t, asana.GetTaskBranchFound, result.Branch)
	task := result.Value
	completedAt := time.Date(2026, time.September, 30, 18, 0, 0, 0, time.UTC)
	require.Equal(t, asana.Task{
		ID: testTaskID, Name: "[REQ-1042] Replace badge reader", ResourceSubtype: "default_task", IsCompleted: true, CompletedAt: &completedAt,
		Assignee: &asana.UserReference{ID: testUserID, Name: "Ada Lovelace"}, DueOn: "2026-10-15",
		Memberships: []asana.TaskMembership{{
			Project: asana.ProjectReference{ID: testProjectID, Name: "Facilities"},
			Section: &asana.SectionReference{ID: testSectionID, Name: "Approved"},
		}},
		Workspace: &asana.WorkspaceReference{ID: "1100000000000001", Name: "Operations"},
		Notes:     "Badge reader at door 4 is offline.",
		CustomFields: []asana.CustomFieldValue{
			{ID: "1300000000000001", Name: "Request ID", Type: "text", DisplayValue: "REQ-1042", TextValue: stringPointer("REQ-1042")},
			{ID: "1300000000000002", Name: "Priority", Type: "enum", DisplayValue: "High", EnumOption: &asana.EnumOptionReference{ID: "1300000000000021", Name: "High"}},
		},
		PermalinkURL: "https://app.asana.com/0/" + testProjectID + "/" + testTaskID,
		CreatedAt:    time.Date(2026, time.September, 30, 16, 15, 0, 0, time.UTC),
		ModifiedAt:   time.Date(2026, time.September, 30, 17, 20, 0, 0, time.UTC),
	}, task)
	require.True(t, task.IsInProject(testProjectID))
	require.False(t, task.IsInProject("1"))
	query, err := url.ParseQuery(provider.request(0).rawQuery)
	require.NoError(t, err)
	for _, field := range []string{"notes", "custom_fields.display_value", "workspace.name", "assignee.name"} {
		require.Contains(t, strings.Split(query.Get("opt_fields"), ","), field)
	}
}

func TestGetTaskTruncatesLongNotes(t *testing.T) {
	longNotes := strings.Repeat("é", 65537)
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":{"gid":"`+testTaskID+`","name":"Long","notes":"`+longNotes+`","created_at":"2026-09-30T16:15:00.000Z","modified_at":"2026-09-30T16:15:00.000Z"}}`)
	})
	client := newAsanaClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newAsanaDexContext("get-long"), client.GetTask(), asanaConnection, asana.GetTaskInput{TaskID: testTaskID})
	require.NoError(t, err)
	require.Equal(t, asana.GetTaskBranchFound, result.Branch)
	require.True(t, result.Value.IsNotesTruncated)
	require.Equal(t, 65536, len([]rune(result.Value.Notes)))
}

func TestGetTaskSelectsNotFoundAndRejectsInvalidIDs(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, `{"errors":[{"message":"SENTINEL task: Unknown object"}]}`)
	})
	client := newAsanaClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newAsanaDexContext("get-missing"), client.GetTask(), asanaConnection, asana.GetTaskInput{TaskID: testTaskID})
	require.NoError(t, err)
	require.Equal(t, asana.GetTaskBranchNotFound, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	requireNoSentinel(t, result)

	for _, taskID := range []string{"", "external:REQ-1042", "../projects", "12 34"} {
		result, err := sdkgo.RunQuery(newAsanaDexContext("get-invalid"), client.GetTask(), asanaConnection, asana.GetTaskInput{TaskID: taskID})
		require.NoError(t, err)
		require.Equal(t, asana.GetTaskBranchDefect, result.Branch, taskID)
	}
	require.Equal(t, 1, provider.requestCount())
}

func stringPointer(value string) *string { return &value }
