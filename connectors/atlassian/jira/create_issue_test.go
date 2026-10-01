// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validCreateIssueInput() jira.CreateIssueInput {
	return jira.CreateIssueInput{
		ProjectKey: "OPS", IssueTypeName: "Task", Summary: "Fire panel wiring",
		Description: "Panel B trips at 02:00.\nBreaker is warm.\n\nCheck the fire log.", Labels: []string{"incident", "facilities"},
		AssigneeAccountID: "5b10ac8d82e05b22cc7d4ef5",
	}
}

func TestCreateIssueSendsFieldsWithADFDescriptionAndReturnsTheKey(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"10043","key":"OPS-43","self":"https://api.atlassian.com/issue/10043"}`)
	})
	client := newJiraClient(t, provider.URL)

	result, err := sdkgo.RunMutation(newJiraDexContext("create"), client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, result.Branch)
	require.Equal(t, jira.CreateIssueOutput{IssueID: "10043", IssueKey: "OPS-43", ProjectKey: "OPS", Summary: "Fire panel wiring"}, result.Value)
	require.Equal(t, "OPS-43", result.Receipt.ProviderObjectID)
	require.Equal(t, sdkgo.IdempotencyKey(result.Receipt.CallID), result.Receipt.IdempotencyKey)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, testSitePrefix+"/issue", request.path)
	require.JSONEq(t, `{"fields":{
		"project":{"key":"OPS"},"issuetype":{"name":"Task"},"summary":"Fire panel wiring",
		"description":{"type":"doc","version":1,"content":[
			{"type":"paragraph","content":[{"type":"text","text":"Panel B trips at 02:00."},{"type":"hardBreak"},{"type":"text","text":"Breaker is warm."}]},
			{"type":"paragraph","content":[{"type":"text","text":"Check the fire log."}]}]},
		"labels":["incident","facilities"],"assignee":{"accountId":"5b10ac8d82e05b22cc7d4ef5"}}}`, request.body)
}

func TestCreateIssueByIDsOmitsBlankOptionalFields(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"10043","key":"OPS-43"}`)
	})
	client := newJiraClient(t, provider.URL)
	input := jira.CreateIssueInput{ProjectID: "10000", IssueTypeID: "10001", Summary: "  Rotate badges  "}
	result, err := sdkgo.RunMutation(newJiraDexContext("create-ids"), client.CreateIssue(), jiraConnection, input)
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, result.Branch)
	require.JSONEq(t, `{"fields":{"project":{"id":"10000"},"issuetype":{"id":"10001"},"summary":"Rotate badges"}}`, provider.request(0).body)
}

func TestCreateIssueNeverResendsARequestJiraMayHaveReceived(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		body     string
		branch   sdkgo.BranchID
		kind     sdkgo.FailureKind
		isRetry  bool
		rejected []string
	}{
		{name: "rejected fields", status: http.StatusBadRequest, body: `{"errorMessages":[],"errors":{"summary":"SENTINEL"}}`,
			branch: jira.CreateIssueBranchProviderRejected, kind: sdkgo.FailureValidation, rejected: []string{"summary"}},
		{name: "permission", status: http.StatusForbidden, body: `{"errorMessages":["SENTINEL"]}`, branch: jira.CreateIssueBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "configuration", status: http.StatusUnprocessableEntity, body: `{"errorMessages":["SENTINEL"]}`, branch: jira.CreateIssueBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
		{name: "rate limit", status: http.StatusTooManyRequests, body: `{}`, isRetry: true, kind: sdkgo.FailureRateLimit},
		{name: "server error", status: http.StatusInternalServerError, body: `{"errorMessages":["SENTINEL"]}`, branch: jira.CreateIssueBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "gateway timeout", status: http.StatusGatewayTimeout, body: `{}`, branch: jira.CreateIssueBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "redirect", status: http.StatusFound, body: `{}`, branch: jira.CreateIssueBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "unusable created body", status: http.StatusCreated, body: `{"id":"SENTINEL","key":"not a key"}`, branch: jira.CreateIssueBranchUncertain, kind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newJiraClient(t, provider.URL)
			result, err := sdkgo.RunMutation(newJiraDexContext("create-"+test.name), client.CreateIssue(), jiraConnection, validCreateIssueInput())
			require.Equal(t, 1, provider.requestCount())
			if test.isRetry {
				requireRetry(t, err, test.kind)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Empty(t, result.Value.IssueKey)
			require.Equal(t, "OPS", result.Value.ProjectKey)
			require.Equal(t, "Fire panel wiring", result.Value.Summary)
			require.Equal(t, test.rejected, result.Value.RejectedFieldIDs)
			requireNoSentinel(t, result)
		})
	}
}

func TestCreateIssueTimeoutAfterDispatchIsUncertain(t *testing.T) {
	provider := newRecordingJira(t, func(_ http.ResponseWriter, request *http.Request, _ int) {
		<-request.Context().Done()
	})
	client := newJiraClient(t, provider.URL, jira.WithHTTPClient(&http.Client{Timeout: 200 * time.Millisecond}))
	result, err := sdkgo.RunMutation(newJiraDexContext("create-timeout"), client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateIssueRefusedConnectionRetriesBecauseNothingWasSent(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedEndpoint := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	client := newJiraClient(t, closedEndpoint)
	_, err = sdkgo.RunMutation(newJiraDexContext("create-refused"), client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureTransport)
}

func TestCreateIssueRejectsInvalidInputWithoutARequest(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*jira.CreateIssueInput)
		message string
	}{
		{name: "two projects", mutate: func(input *jira.CreateIssueInput) { input.ProjectID = "10000" }, message: "set exactly one of projectKey and projectId"},
		{name: "no project", mutate: func(input *jira.CreateIssueInput) { input.ProjectKey = "" }, message: "set exactly one of projectKey and projectId"},
		{name: "lowercase key", mutate: func(input *jira.CreateIssueInput) { input.ProjectKey = "ops" }, message: "projectKey must be an uppercase project key such as OPS"},
		{name: "two issue types", mutate: func(input *jira.CreateIssueInput) { input.IssueTypeID = "10001" }, message: "set exactly one of issueTypeName and issueTypeId"},
		{name: "blank summary", mutate: func(input *jira.CreateIssueInput) { input.Summary = " " }, message: "summary is required"},
		{name: "multi-line summary", mutate: func(input *jira.CreateIssueInput) { input.Summary = "one\ntwo" }, message: "summary must be one line without control characters"},
		{name: "label with space", mutate: func(input *jira.CreateIssueInput) { input.Labels = []string{"needs triage"} }, message: "each label must be 1 to 255 characters without whitespace"},
		{name: "assignee email", mutate: func(input *jira.CreateIssueInput) { input.AssigneeAccountID = "ada@example.com" }, message: "assigneeAccountId must be an Atlassian account ID"},
		{name: "control character", mutate: func(input *jira.CreateIssueInput) { input.Description = "bell\a" }, message: "description cannot contain control characters"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingJira(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
			client := newJiraClient(t, provider.URL)
			input := validCreateIssueInput()
			test.mutate(&input)
			result, err := sdkgo.RunMutation(newJiraDexContext("create-invalid-"+test.name), client.CreateIssue(), jiraConnection, input)
			require.NoError(t, err)
			require.Equal(t, jira.CreateIssueBranchDefect, result.Branch)
			require.Equal(t, test.message, result.Failure.Message)
		})
	}
}
