// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestSearchIssuesSendsEscapedFilterJQLAndReturnsOnePage(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"issues":[`+issueJSON("10042", "OPS-42", `Panel "B" wiring`, "3", "In Progress")+`],"nextPageToken":"page-2","isLast":false}`)
	})
	client := newJiraClient(t, provider.URL)
	input := jira.SearchIssuesInput{
		Filter: &jira.IssueSearchFilter{
			ProjectKeys: []string{"OPS"}, StatusCategories: []jira.StatusCategoryKey{jira.StatusCategoryToDo, jira.StatusCategoryInProgress},
			SummaryPhrase: `Panel "B" wiring`,
		},
		AdditionalFields: []string{"customfield_10020"}, PageSize: 10,
	}

	result, err := sdkgo.RunQuery(newJiraDexContext("search"), client.SearchIssues(), jiraConnection, input)
	require.NoError(t, err)
	require.Equal(t, jira.SearchIssuesBranchSearched, result.Branch)
	expectedJQL := `project in ("OPS") AND statusCategory in (2, 4) AND summary ~ "\"Panel \\\"B\\\" wiring\"" ORDER BY created DESC`
	require.Equal(t, expectedJQL, result.Value.JQL)
	require.Equal(t, "page-2", result.Value.NextPageToken)
	require.Len(t, result.Value.Issues, 1)
	issue := result.Value.Issues[0]
	require.Equal(t, "OPS-42", issue.Key)
	require.Equal(t, jira.IssueStatus{ID: "3", Name: "In Progress", CategoryKey: jira.StatusCategoryToDo}, issue.Status)
	require.Empty(t, issue.Description, "search never returns the description")
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, testSitePrefix+"/search/jql", request.path)
	body := provider.requestBody(0)
	require.Equal(t, expectedJQL, body["jql"])
	require.Equal(t, float64(10), body["maxResults"])
	require.Equal(t, []any{"summary", "status", "issuetype", "project", "assignee", "reporter", "priority", "labels", "created", "updated", "customfield_10020"}, body["fields"])
	require.NotContains(t, body, "nextPageToken")
}

func TestSearchIssuesContinuesWithTheNextPageTokenAndStopsOnTheLastPage(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"issues":[],"isLast":true}`)
	})
	client := newJiraClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newJiraDexContext("search-next"), client.SearchIssues(), jiraConnection,
		jira.SearchIssuesInput{JQL: `project = "OPS" ORDER BY created DESC`, NextPageToken: "page-2"})
	require.NoError(t, err)
	require.Equal(t, jira.SearchIssuesBranchSearched, result.Branch)
	require.Empty(t, result.Value.Issues)
	require.Empty(t, result.Value.NextPageToken)
	body := provider.requestBody(0)
	require.Equal(t, "page-2", body["nextPageToken"])
	require.Equal(t, float64(50), body["maxResults"], "zero page size requests 50")
}

func TestSearchIssuesRejectsAmbiguousOrUnboundedInputWithoutARequest(t *testing.T) {
	provider := newRecordingJira(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newJiraClient(t, provider.URL)
	for _, test := range []struct {
		name    string
		input   jira.SearchIssuesInput
		message string
	}{
		{name: "filter and jql", input: jira.SearchIssuesInput{Filter: &jira.IssueSearchFilter{ProjectKeys: []string{"OPS"}}, JQL: "project = OPS"}, message: "set either filter or jql, not both"},
		{name: "empty filter", input: jira.SearchIssuesInput{Filter: &jira.IssueSearchFilter{}}, message: "filter needs at least one clause, because Jira accepts only bounded queries"},
		{name: "no query", input: jira.SearchIssuesInput{}, message: "jql is required when no filter is set"},
		{name: "page too large", input: jira.SearchIssuesInput{JQL: "project = OPS", PageSize: 101}, message: "pageSize must be between 1 and 100"},
		{name: "wildcard field", input: jira.SearchIssuesInput{JQL: "project = OPS", AdditionalFields: []string{"*all"}}, message: "additionalFields must be Jira field IDs such as customfield_10020 or duedate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(newJiraDexContext("search-invalid"), client.SearchIssues(), jiraConnection, test.input)
			require.NoError(t, err)
			require.Equal(t, jira.SearchIssuesBranchDefect, result.Branch)
			require.Equal(t, test.message, result.Failure.Message)
		})
	}
}

func TestSearchIssuesClassifiesJiraResponses(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		isRetry bool
		kind    sdkgo.FailureKind
	}{
		{name: "invalid JQL", status: http.StatusBadRequest, body: `{"errorMessages":["SENTINEL unbounded"]}`, branch: jira.SearchIssuesBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "server error", status: http.StatusInternalServerError, body: `{}`, isRetry: true, kind: sdkgo.FailureAvailability},
		{name: "invalid issue", status: http.StatusOK, body: `{"issues":[{"id":"SENTINEL","key":"OPS-1"}]}`, branch: jira.SearchIssuesBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "not JSON", status: http.StatusOK, body: `SENTINEL`, branch: jira.SearchIssuesBranchInvalidResponse, kind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newJiraClient(t, provider.URL)
			result, err := sdkgo.RunQuery(newJiraDexContext("search-"+test.name), client.SearchIssues(), jiraConnection, jira.SearchIssuesInput{JQL: "project = OPS"})
			if test.isRetry {
				requireRetry(t, err, test.kind)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			requireNoSentinel(t, result)
		})
	}
}
