// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListProjectTasksSendsFiltersAndReturnsTheNextOffset(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":[`+taskJSON(testTaskID, "[REQ-1042] Replace badge reader", false, testSectionID)+`],`+
			`"next_page":{"offset":"eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.page2","path":"/tasks?offset=x","uri":"https://app.asana.com/api/1.0/tasks?offset=x"}}`)
	})
	client := newAsanaClient(t, provider.URL)
	modifiedSince := time.Date(2026, time.September, 29, 8, 30, 0, 0, time.FixedZone("PDT", -7*3600))

	result, err := sdkgo.RunQuery(newAsanaDexContext("list-project"), client.ListTasks(), asanaConnection, asana.ListTasksInput{
		ProjectID: testProjectID, IsIncompleteOnly: true, ModifiedSince: &modifiedSince, PageSize: 100,
	})
	require.NoError(t, err)
	require.Equal(t, asana.ListTasksBranchListed, result.Branch)
	require.Len(t, result.Value.Tasks, 1)
	task := result.Value.Tasks[0]
	require.Equal(t, testTaskID, task.ID)
	require.Equal(t, "[REQ-1042] Replace badge reader", task.Name)
	require.Equal(t, &asana.UserReference{ID: testUserID, Name: "Ada Lovelace"}, task.Assignee)
	require.Equal(t, testSectionID, task.SectionID(testProjectID))
	require.Empty(t, task.Notes, "a listed task carries no notes")
	require.Empty(t, task.CustomFields, "a listed task carries no custom field values")
	require.Equal(t, "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.page2", result.Value.NextOffset)

	request := provider.request(0)
	require.Equal(t, "/tasks", request.path)
	query, err := url.ParseQuery(request.rawQuery)
	require.NoError(t, err)
	require.Equal(t, testProjectID, query.Get("project"))
	require.Equal(t, "now", query.Get("completed_since"))
	require.Equal(t, "2026-09-29T15:30:00Z", query.Get("modified_since"))
	require.Equal(t, "100", query.Get("limit"))
	require.Empty(t, query.Get("offset"))
	require.Contains(t, query.Get("opt_fields"), "memberships.section.name")
	require.NotContains(t, query.Get("opt_fields"), "email", "listed related objects keep to gid and name")
}

func TestListSectionTasksContinuesFromAnOffset(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":[],"next_page":null}`)
	})
	client := newAsanaClient(t, provider.URL)
	completedSince := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)

	result, err := sdkgo.RunQuery(newAsanaDexContext("list-section"), client.ListTasks(), asanaConnection, asana.ListTasksInput{
		SectionID: testSectionID, CompletedSince: &completedSince, Offset: "page-2-token",
	})
	require.NoError(t, err)
	require.Equal(t, asana.ListTasksBranchListed, result.Branch)
	require.Empty(t, result.Value.Tasks)
	require.Empty(t, result.Value.NextOffset)
	query, err := url.ParseQuery(provider.request(0).rawQuery)
	require.NoError(t, err)
	require.Equal(t, testSectionID, query.Get("section"))
	require.Empty(t, query.Get("project"))
	require.Equal(t, "2026-09-01T00:00:00Z", query.Get("completed_since"))
	require.Equal(t, "50", query.Get("limit"))
	require.Equal(t, "page-2-token", query.Get("offset"))
}

func TestListTasksMapsProviderOutcomes(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{name: "missing project", status: http.StatusNotFound, body: `{"errors":[{"message":"SENTINEL project: Unknown object"}]}`, branch: asana.ListTasksBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "expired offset", status: http.StatusBadRequest, body: `{"errors":[{"message":"SENTINEL offset: Your pagination token has expired."}]}`, branch: asana.ListTasksBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "page without data", status: http.StatusOK, body: `{"errors":[]}`, branch: asana.ListTasksBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "invalid task gid", status: http.StatusOK, body: `{"data":[{"gid":"SENTINEL","name":"x"}]}`, branch: asana.ListTasksBranchInvalidResponse, kind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newAsanaClient(t, provider.URL)
			result, err := sdkgo.RunQuery(newAsanaDexContext("list-"+test.name), client.ListTasks(), asanaConnection, asana.ListTasksInput{ProjectID: testProjectID})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			requireNoSentinel(t, result)
		})
	}
}

func TestListTasksServerErrorRetries(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusServiceUnavailable, `{"errors":[{"message":"SENTINEL","phrase":"6 sad squid snuggle softly"}]}`)
	})
	client := newAsanaClient(t, provider.URL)
	_, err := sdkgo.RunQuery(newAsanaDexContext("list-503"), client.ListTasks(), asanaConnection, asana.ListTasksInput{ProjectID: testProjectID})
	requireRetry(t, err, sdkgo.FailureAvailability)
}

func TestListTasksRejectsInvalidInputWithoutARequest(t *testing.T) {
	since := time.Now()
	for _, test := range []struct {
		input   asana.ListTasksInput
		message string
	}{
		{asana.ListTasksInput{}, "set exactly one of projectId and sectionId"},
		{asana.ListTasksInput{ProjectID: testProjectID, SectionID: testSectionID}, "set exactly one of projectId and sectionId"},
		{asana.ListTasksInput{ProjectID: "facilities"}, "projectId must be an Asana gid, a decimal string such as 1204567890123456"},
		{asana.ListTasksInput{ProjectID: testProjectID, IsIncompleteOnly: true, CompletedSince: &since}, "set at most one of isIncompleteOnly and completedSince"},
		{asana.ListTasksInput{ProjectID: testProjectID, PageSize: 101}, "pageSize must be between 1 and 100"},
		{asana.ListTasksInput{ProjectID: testProjectID, Offset: "has space"}, "offset must be the token from a previous task page"},
	} {
		provider := newRecordingAsana(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
		client := newAsanaClient(t, provider.URL)
		result, err := sdkgo.RunQuery(newAsanaDexContext("list-invalid"), client.ListTasks(), asanaConnection, test.input)
		require.NoError(t, err)
		require.Equal(t, asana.ListTasksBranchDefect, result.Branch)
		require.Equal(t, test.message, result.Failure.Message)
	}
}
