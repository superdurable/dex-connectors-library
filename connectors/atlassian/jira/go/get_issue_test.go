// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetIssueReturnsStandardFieldsDescriptionAndAdditionalFields(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":"10042","key":"OPS-42","fields":{`+
			`"summary":"Fire panel wiring","status":{"id":"3","name":"In Progress","statusCategory":{"key":"indeterminate"}},`+
			`"issuetype":{"id":"10001","name":"Task","subtask":false},"project":{"id":"10000","key":"OPS","name":"Operations"},`+
			`"assignee":{"accountId":"5b10ac8d82e05b22cc7d4ef5","displayName":"Ada","emailAddress":"ada@example.com"},"reporter":null,`+
			`"priority":{"id":"3","name":"Medium"},"labels":["incident"],"created":"2026-09-30T09:15:00.000-0700","updated":"2026-09-30T10:00:00.000-0700",`+
			`"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Panel B trips."},`+
			`{"type":"hardBreak"},{"type":"mention","attrs":{"id":"x","text":"@Ben"}}]}]},`+
			`"customfield_10020":[{"id":7,"name":"Sprint 7"}]}}`)
	})
	client := newJiraClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newJiraDexContext("get"), client.GetIssue(), jiraConnection,
		jira.GetIssueInput{IssueIDOrKey: "OPS-42", AdditionalFields: []string{"customfield_10020", "duedate", "summary"}})
	require.NoError(t, err)
	require.Equal(t, jira.GetIssueBranchFound, result.Branch)
	issue := result.Value
	require.Equal(t, jira.Issue{
		ID: "10042", Key: "OPS-42", Summary: "Fire panel wiring",
		Status:    jira.IssueStatus{ID: "3", Name: "In Progress", CategoryKey: jira.StatusCategoryInProgress},
		IssueType: jira.IssueTypeReference{ID: "10001", Name: "Task"},
		Project:   jira.ProjectReference{ID: "10000", Key: "OPS", Name: "Operations"},
		Assignee:  &jira.AccountReference{AccountID: "5b10ac8d82e05b22cc7d4ef5", DisplayName: "Ada"},
		Priority:  &jira.PriorityReference{ID: "3", Name: "Medium"}, Labels: []string{"incident"},
		Description: "Panel B trips.\n@Ben",
		CreatedAt:   time.Date(2026, time.September, 30, 16, 15, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, time.September, 30, 17, 0, 0, 0, time.UTC),
		AdditionalFields: map[string]json.RawMessage{
			"customfield_10020": json.RawMessage(`[{"id":7,"name":"Sprint 7"}]`),
			"summary":           json.RawMessage(`"Fire panel wiring"`),
		},
	}, issue)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "ada@example.com", "account emails never cross the connector boundary")
	request := provider.request(0)
	require.Equal(t, "fields=summary%2Cstatus%2Cissuetype%2Cproject%2Cassignee%2Creporter%2Cpriority%2Clabels%2Ccreated%2Cupdated%2Cdescription%2Ccustomfield_10020%2Cduedate", request.rawQuery)
}

func TestGetIssueOutcomes(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{name: "missing", status: http.StatusNotFound, body: `{"errorMessages":["SENTINEL Issue does not exist"]}`, branch: jira.GetIssueBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "forbidden", status: http.StatusForbidden, body: `{}`, branch: jira.GetIssueBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "invalid time", status: http.StatusOK, body: `{"id":"10042","key":"OPS-42","fields":{"created":"SENTINEL"}}`, branch: jira.GetIssueBranchInvalidResponse, kind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newJiraClient(t, provider.URL)
			result, err := sdkgo.RunQuery(newJiraDexContext("get-"+test.name), client.GetIssue(), jiraConnection, jira.GetIssueInput{IssueIDOrKey: "10042"})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			requireNoSentinel(t, result)
		})
	}
}

func TestOversizedIssueSelectsInvalidResponse(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":"10042","key":"OPS-42","fields":{"summary":"`+strings.Repeat("x", 2048)+`"}}`)
	})
	client, err := jira.New(jira.Config{CloudID: testCloudID, Endpoint: provider.URL, MaxResponseBytes: 1024}, staticJiraCredentials())
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newJiraDexContext("get-oversized"), client.GetIssue(), jiraConnection, jira.GetIssueInput{IssueIDOrKey: "OPS-42"})
	require.NoError(t, err)
	require.Equal(t, jira.GetIssueBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestGetIssueRejectsPathTraversalWithoutARequest(t *testing.T) {
	provider := newRecordingJira(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newJiraClient(t, provider.URL)
	for _, key := range []string{"", "OPS-0", "../../myself", "OPS-1/comment", "OPS 1"} {
		result, err := sdkgo.RunQuery(newJiraDexContext("get-invalid"), client.GetIssue(), jiraConnection, jira.GetIssueInput{IssueIDOrKey: key})
		require.NoError(t, err)
		require.Equal(t, jira.GetIssueBranchDefect, result.Branch, key)
	}
}
