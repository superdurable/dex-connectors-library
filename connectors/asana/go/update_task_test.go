// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateTaskMovesTheTaskThenAppliesAbsoluteFields(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, `{"data":{}}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"data":`+taskJSON(testTaskID, "[REQ-1042] Replace badge reader", true, testSectionID)+`}`)
	})
	client := newAsanaClient(t, provider.URL)
	isCompleted, assignee, dueOn, text := true, testUserID, "2026-10-15", "REQ-1042"

	result, err := sdkgo.RunMutation(newAsanaDexContext("update"), client.UpdateTask(), asanaConnection, asana.UpdateTaskInput{
		TaskID: testTaskID, IsCompleted: &isCompleted, AssigneeID: &assignee, DueOn: &dueOn, SectionID: testSectionID,
		CustomFields: []asana.CustomFieldValueInput{{CustomFieldID: "1300000000000001", Text: &text}},
	})
	require.NoError(t, err)
	require.Equal(t, asana.UpdateTaskBranchUpdated, result.Branch)
	require.True(t, result.Value.IsMovedToSection)
	require.Equal(t, testTaskID, result.Value.TaskID)
	require.Equal(t, testSectionID, result.Value.Task.SectionID(testProjectID))
	require.True(t, result.Value.Task.IsCompleted)
	require.Equal(t, testTaskID, result.Receipt.ProviderObjectID)

	move := provider.request(0)
	require.Equal(t, http.MethodPost, move.method)
	require.Equal(t, "/sections/"+testSectionID+"/addTask", move.path)
	require.JSONEq(t, `{"data":{"task":"`+testTaskID+`"}}`, move.body)
	update := provider.request(1)
	require.Equal(t, http.MethodPut, update.method)
	require.Equal(t, "/tasks/"+testTaskID, update.path)
	require.Contains(t, update.rawQuery, "opt_fields=")
	require.JSONEq(t, `{"data":{"completed":true,"assignee":"`+testUserID+`","due_on":"2026-10-15","custom_fields":{"1300000000000001":"REQ-1042"}}}`, update.body)
}

func TestUpdateTaskSendsNullToUnassignAndClearTheDueDate(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":`+taskJSON(testTaskID, "Rotate badges", false, testSectionID)+`}`)
	})
	client := newAsanaClient(t, provider.URL)
	cleared, isCompleted := "", false
	result, err := sdkgo.RunMutation(newAsanaDexContext("update-clear"), client.UpdateTask(), asanaConnection, asana.UpdateTaskInput{
		TaskID: testTaskID, AssigneeID: &cleared, DueOn: &cleared, IsCompleted: &isCompleted,
	})
	require.NoError(t, err)
	require.Equal(t, asana.UpdateTaskBranchUpdated, result.Branch)
	require.False(t, result.Value.IsMovedToSection)
	require.Equal(t, 1, provider.requestCount())
	require.JSONEq(t, `{"data":{"assignee":null,"due_on":null,"completed":false}}`, provider.request(0).body)
}

func TestUpdateTaskMoveOnlyReadsTheTaskBack(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, `{"data":{}}`)
			return
		}
		require.Equal(t, http.MethodGet, request.Method)
		writeJSON(t, response, http.StatusOK, `{"data":`+taskJSON(testTaskID, "Rotate badges", false, testSectionID)+`}`)
	})
	client := newAsanaClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newAsanaDexContext("update-move"), client.UpdateTask(), asanaConnection, asana.UpdateTaskInput{
		TaskID: testTaskID, SectionID: testSectionID,
	})
	require.NoError(t, err)
	require.Equal(t, asana.UpdateTaskBranchUpdated, result.Branch)
	require.Equal(t, "Approved", result.Value.Task.Memberships[0].Section.Name)
	require.Equal(t, "/tasks/"+testTaskID, provider.request(1).path)
}

func TestUpdateTaskRetriesEveryAmbiguousOutcome(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		kind   sdkgo.FailureKind
	}{
		{name: "server error", status: http.StatusInternalServerError, kind: sdkgo.FailureAvailability},
		{name: "unavailable", status: http.StatusServiceUnavailable, kind: sdkgo.FailureAvailability},
		{name: "rate limit", status: http.StatusTooManyRequests, kind: sdkgo.FailureRateLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, `{"errors":[{"message":"SENTINEL"}]}`)
			})
			client := newAsanaClient(t, provider.URL)
			isCompleted := true
			_, err := sdkgo.RunMutation(newAsanaDexContext("update-"+test.name), client.UpdateTask(), asanaConnection,
				asana.UpdateTaskInput{TaskID: testTaskID, IsCompleted: &isCompleted})
			requireRetry(t, err, test.kind)
		})
	}

	provider := newRecordingAsana(t, func(_ http.ResponseWriter, request *http.Request, _ int) {
		<-request.Context().Done()
	})
	client := newAsanaClient(t, provider.URL, asana.WithHTTPClient(&http.Client{Timeout: 200 * time.Millisecond}))
	isCompleted := true
	_, err := sdkgo.RunMutation(newAsanaDexContext("update-timeout"), client.UpdateTask(), asanaConnection,
		asana.UpdateTaskInput{TaskID: testTaskID, IsCompleted: &isCompleted})
	requireRetry(t, err, sdkgo.FailureTransport)
}

func TestUpdateTaskMapsConclusiveRejections(t *testing.T) {
	for _, test := range []struct {
		name             string
		failingIndex     int
		status           int
		branch           sdkgo.BranchID
		isMovedToSection bool
	}{
		{name: "missing section", failingIndex: 0, status: http.StatusNotFound, branch: asana.UpdateTaskBranchNotFound},
		{name: "section of another project", failingIndex: 0, status: http.StatusBadRequest, branch: asana.UpdateTaskBranchProviderRejected},
		{name: "unknown assignee after the move", failingIndex: 1, status: http.StatusBadRequest, branch: asana.UpdateTaskBranchProviderRejected, isMovedToSection: true},
		{name: "missing task after the move", failingIndex: 1, status: http.StatusNotFound, branch: asana.UpdateTaskBranchNotFound, isMovedToSection: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if index == test.failingIndex {
					writeJSON(t, response, test.status, `{"errors":[{"message":"SENTINEL assignee: Not a recognized ID"}]}`)
					return
				}
				writeJSON(t, response, http.StatusOK, `{"data":{}}`)
			})
			client := newAsanaClient(t, provider.URL)
			assignee := "ada@example.com"
			result, err := sdkgo.RunMutation(newAsanaDexContext("update-"+test.name), client.UpdateTask(), asanaConnection,
				asana.UpdateTaskInput{TaskID: testTaskID, SectionID: testSectionID, AssigneeID: &assignee})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.isMovedToSection, result.Value.IsMovedToSection)
			require.Nil(t, result.Value.Task)
			require.Equal(t, test.failingIndex+1, provider.requestCount())
			requireNoSentinel(t, result)
		})
	}
}

func TestUpdateTaskRejectsInvalidInputWithoutARequest(t *testing.T) {
	invalidDate, invalidAssignee := "tomorrow", "Ada"
	for _, test := range []struct {
		input   asana.UpdateTaskInput
		message string
	}{
		{asana.UpdateTaskInput{TaskID: testTaskID}, "set at least one of isCompleted, assigneeId, dueOn, customFields, and sectionId"},
		{asana.UpdateTaskInput{TaskID: "OPS-1", SectionID: testSectionID}, "taskId must be an Asana gid, a decimal string such as 1204567890123456"},
		{asana.UpdateTaskInput{TaskID: testTaskID, SectionID: "Approved"}, "sectionId must be an Asana gid, a decimal string such as 1204567890123456"},
		{asana.UpdateTaskInput{TaskID: testTaskID, DueOn: &invalidDate}, "dueOn must be a YYYY-MM-DD date such as 2026-10-15"},
		{asana.UpdateTaskInput{TaskID: testTaskID, AssigneeID: &invalidAssignee}, "assigneeId must be me, a user gid, or an email address"},
	} {
		provider := newRecordingAsana(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
		client := newAsanaClient(t, provider.URL)
		result, err := sdkgo.RunMutation(newAsanaDexContext("update-invalid"), client.UpdateTask(), asanaConnection, test.input)
		require.NoError(t, err)
		require.Equal(t, asana.UpdateTaskBranchDefect, result.Branch)
		require.Equal(t, test.message, result.Failure.Message)
		require.False(t, strings.Contains(result.Failure.Message, "SENTINEL"))
	}
}
