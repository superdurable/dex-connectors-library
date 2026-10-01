// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestAddCommentSendsADFAndReturnsTheCommentID(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"10500","created":"2026-09-30T10:05:00.000-0700",`+
			`"author":{"accountId":"5b10ac8d82e05b22cc7d4ef5","displayName":"Ada"},"body":{"type":"doc","version":1,"content":[]}}`)
	})
	client := newJiraClient(t, provider.URL)

	result, err := sdkgo.RunMutation(newJiraDexContext("comment"), client.AddComment(), jiraConnection,
		jira.AddCommentInput{IssueIDOrKey: "OPS-43", Body: "Triage started.\n*not bold*"})
	require.NoError(t, err)
	require.Equal(t, jira.AddCommentBranchAdded, result.Branch)
	require.Equal(t, jira.AddCommentOutput{
		IssueIDOrKey: "OPS-43", CommentID: "10500", AuthorAccountID: "5b10ac8d82e05b22cc7d4ef5",
		CreatedAt: time.Date(2026, time.September, 30, 17, 5, 0, 0, time.UTC),
	}, result.Value)
	request := provider.request(0)
	require.Equal(t, testSitePrefix+"/issue/OPS-43/comment", request.path)
	require.JSONEq(t, `{"body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[
		{"type":"text","text":"Triage started."},{"type":"hardBreak"},{"type":"text","text":"*not bold*"}]}]}}`, request.body)
}

func TestAddCommentOutcomes(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{name: "missing issue", status: http.StatusNotFound, branch: jira.AddCommentBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "comment limit", status: http.StatusRequestEntityTooLarge, branch: jira.AddCommentBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
		{name: "invalid body", status: http.StatusBadRequest, branch: jira.AddCommentBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "server error", status: http.StatusBadGateway, branch: jira.AddCommentBranchUncertain, kind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, `{"errorMessages":["SENTINEL"]}`)
			})
			client := newJiraClient(t, provider.URL)
			result, err := sdkgo.RunMutation(newJiraDexContext("comment-"+test.name), client.AddComment(), jiraConnection,
				jira.AddCommentInput{IssueIDOrKey: "OPS-43", Body: "Triage started."})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, "OPS-43", result.Value.IssueIDOrKey)
			require.Empty(t, result.Value.CommentID)
			require.Equal(t, 1, provider.requestCount())
			requireNoSentinel(t, result)
		})
	}
}

func TestAddCommentRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingJira(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newJiraClient(t, provider.URL)
	for _, input := range []jira.AddCommentInput{
		{IssueIDOrKey: "OPS-43", Body: "  "},
		{IssueIDOrKey: "../project", Body: "Triage started."},
	} {
		result, err := sdkgo.RunMutation(newJiraDexContext("comment-invalid"), client.AddComment(), jiraConnection, input)
		require.NoError(t, err)
		require.Equal(t, jira.AddCommentBranchDefect, result.Branch)
	}
}
