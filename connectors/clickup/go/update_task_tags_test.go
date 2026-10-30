// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateTaskTagsAddsRemovesAndReadsTheTagsBack(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, taskJSON(t, testTask{id: testTaskID, name: "Fix login", tags: []string{"escalated", "vip customer"}}))
			return
		}
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	result, err := sdkgo.RunMutation(newTestDexContext("tags"), newClickUpClient(t, provider.URL).UpdateTaskTags(), clickupConnection, clickup.UpdateTaskTagsInput{
		TaskID: testTaskID, AddTags: []string{"escalated", "vip customer"}, RemoveTags: []string{"triage"},
	})
	require.NoError(t, err)
	require.Equal(t, clickup.UpdateTaskTagsBranchUpdated, result.Branch)
	require.Equal(t, []string{"escalated", "vip customer"}, result.Value.Tags)
	require.Equal(t, 3, result.Value.AppliedChanges)
	require.Equal(t, 4, provider.requestCount())
	for index, expected := range []struct{ method, path string }{
		{http.MethodPost, "/task/" + testTaskID + "/tag/escalated"},
		{http.MethodPost, "/task/" + testTaskID + "/tag/vip%20customer"},
		{http.MethodDelete, "/task/" + testTaskID + "/tag/triage"},
		{http.MethodGet, "/task/" + testTaskID},
	} {
		require.Equal(t, expected.method, provider.request(index).method)
		require.Equal(t, expected.path, provider.request(index).path)
	}
}

func TestUpdateTaskTagsReportsAppliedChangesOnRejection(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, `{}`)
			return
		}
		writeJSON(t, response, http.StatusForbidden, clickupError("ACCESS_083"))
	})
	result, err := sdkgo.RunMutation(newTestDexContext("tags-rejected"), newClickUpClient(t, provider.URL).UpdateTaskTags(), clickupConnection,
		clickup.UpdateTaskTagsInput{TaskID: testTaskID, AddTags: []string{"escalated"}, RemoveTags: []string{"triage"}})
	require.NoError(t, err)
	require.Equal(t, clickup.UpdateTaskTagsBranchProviderRejected, result.Branch)
	require.Equal(t, 1, result.Value.AppliedChanges)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
}

func TestUpdateTaskTagsRetriesAnUnavailableProvider(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusServiceUnavailable, clickupError("SERVER_1"))
	})
	_, err := sdkgo.RunMutation(newTestDexContext("tags-unavailable"), newClickUpClient(t, provider.URL).UpdateTaskTags(), clickupConnection,
		clickup.UpdateTaskTagsInput{TaskID: testTaskID, AddTags: []string{"escalated"}})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}

func TestUpdateTaskTagsValidatesTagsWithoutARequest(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	for name, input := range map[string]clickup.UpdateTaskTagsInput{
		"none":           {TaskID: testTaskID},
		"slash":          {TaskID: testTaskID, AddTags: []string{"a/b"}},
		"dot segment":    {TaskID: testTaskID, RemoveTags: []string{".."}},
		"dot":            {TaskID: testTaskID, AddTags: []string{"."}},
		"add and remove": {TaskID: testTaskID, AddTags: []string{"a"}, RemoveTags: []string{"a"}},
		"too many":       {TaskID: testTaskID, AddTags: []string{"a", "b", "c", "d", "e", "f"}, RemoveTags: []string{"g", "h", "i", "j", "k"}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunMutation(newTestDexContext("tags-invalid-"+name), newClickUpClient(t, provider.URL).UpdateTaskTags(), clickupConnection, input)
			require.NoError(t, err)
			require.Equal(t, clickup.UpdateTaskTagsBranchDefect, result.Branch)
		})
	}
	require.Zero(t, provider.requestCount())
}
