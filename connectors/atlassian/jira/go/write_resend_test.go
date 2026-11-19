// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

// writeTruncatedJSON sends 2xx headers that promise more body bytes than follow.
func writeTruncatedJSON(response http.ResponseWriter, status int) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Content-Length", "64")
	response.WriteHeader(status)
	_, _ = response.Write([]byte(`{"id":"100`))
}

func TestIssueResendAfterCanResendReadsBackBeforeTheThirdSend(t *testing.T) {
	var operations []string
	posts := 0
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.URL.Path == testSitePrefix+"/search/jql" {
			operations = append(operations, "search")
			writeJSON(t, response, http.StatusOK, `{"issues":[]}`)
			return
		}
		posts++
		operations = append(operations, "create")
		switch posts {
		case 1:
			writeJSON(t, response, http.StatusInternalServerError, `{}`)
		case 2:
			writeJSON(t, response, http.StatusTooManyRequests, `{}`)
		default:
			writeJSON(t, response, http.StatusCreated, `{"id":"10043","key":"OPS-43"}`)
		}
	})
	client := newJiraClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("jira-flow", "create-after-can-resend")
	_, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	ctx.AdvanceAttempt()
	_, err = sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureRateLimit)
	frames := ctx.Heartbeats()
	require.Equal(t, singledispatch.StateCanResend, decodeRecordedMarker(t, frames[len(frames)-1]).State)
	ctx.AdvanceAttempt()
	operations = nil
	result, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, result.Branch)
	require.Equal(t, []string{"search", "create"}, operations, "the first send may exist, so attempt three reads back first")
}

func TestCommentResendAfterCanResendReadsBackBeforeTheThirdSend(t *testing.T) {
	var operations []string
	posts := 0
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			operations = append(operations, "list")
			writeJSON(t, response, http.StatusOK, `{"comments":[]}`)
			return
		}
		posts++
		operations = append(operations, "add")
		switch posts {
		case 1:
			writeJSON(t, response, http.StatusBadGateway, `{}`)
		case 2:
			writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
		default:
			writeJSON(t, response, http.StatusCreated, `{"id":"10500"}`)
		}
	})
	client := newJiraClient(t, provider.URL)
	input := jira.AddCommentInput{IssueIDOrKey: "OPS-43", Body: "Triage started."}
	ctx := singledispatchtest.NewContext("jira-flow", "comment-after-can-resend")
	_, err := sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	ctx.AdvanceAttempt()
	_, err = sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	ctx.AdvanceAttempt()
	operations = nil
	result, err := sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	require.NoError(t, err)
	require.Equal(t, jira.AddCommentBranchAdded, result.Branch)
	require.Equal(t, []string{"list", "add"}, operations, "the first send may exist, so attempt three reads back first")
}

func TestAcceptedWritesWithBrokenBodiesAreNeverResent(t *testing.T) {
	for _, test := range []struct {
		name  string
		write func(http.ResponseWriter)
	}{
		{name: "truncated body", write: func(response http.ResponseWriter) { writeTruncatedJSON(response, http.StatusCreated) }},
		{name: "empty body", write: func(response http.ResponseWriter) { writeJSON(t, response, http.StatusCreated, `{}`) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				switch {
				case request.URL.Path == testSitePrefix+"/search/jql":
					writeJSON(t, response, http.StatusOK, `{"issues":[]}`)
				case request.Method == http.MethodGet:
					writeJSON(t, response, http.StatusOK, `{"comments":[]}`)
				default:
					test.write(response)
				}
			})
			client := newJiraClient(t, provider.URL)
			issue, err := sdkgo.RunMutation(singledispatchtest.NewContext("jira-flow", "issue-"+test.name), client.CreateIssue(), jiraConnection, validCreateIssueInput())
			require.NoError(t, err)
			require.Equal(t, jira.CreateIssueBranchUncertain, issue.Branch)
			comment, err := sdkgo.RunMutation(singledispatchtest.NewContext("jira-flow", "comment-"+test.name), client.AddComment(), jiraConnection,
				jira.AddCommentInput{IssueIDOrKey: "OPS-43", Body: "Triage started."})
			require.NoError(t, err)
			require.Equal(t, jira.AddCommentBranchUncertain, comment.Branch)
			require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue"))
			require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue/OPS-43/comment"))
		})
	}
}

func TestRefusalStatusWithAnUnreadableBodyIsAnUnknownOutcome(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch {
		case request.URL.Path == testSitePrefix+"/search/jql":
			writeJSON(t, response, http.StatusOK, `{"issues":[]}`)
		case request.Method == http.MethodGet:
			writeJSON(t, response, http.StatusOK, `{"comments":[]}`)
		default:
			writeTruncatedJSON(response, http.StatusBadRequest)
		}
	})
	client := newJiraClient(t, provider.URL)
	_, err := sdkgo.RunMutation(singledispatchtest.NewContext("jira-flow", "issue-cut-refusal"), client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	_, err = sdkgo.RunMutation(singledispatchtest.NewContext("jira-flow", "comment-cut-refusal"), client.AddComment(), jiraConnection,
		jira.AddCommentInput{IssueIDOrKey: "OPS-43", Body: "Triage started."})
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/search/jql"), "the unknown create is read back")
}

func TestAcceptedIssueWithoutIDIsUncertainWhenReadBackIsRateLimited(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.URL.Path == testSitePrefix+"/search/jql" {
			writeJSON(t, response, http.StatusTooManyRequests, `{}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, `{}`)
	})
	client := newJiraClient(t, provider.URL)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("jira-flow", "accepted-rate-limited"), client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchUncertain, result.Branch, "a retry could not tell that Jira accepted the create")
	require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue"))
}

func TestCredentialFailureAfterAPossibleSendRecordsNoNotSentMarker(t *testing.T) {
	posts := 0
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.URL.Path == testSitePrefix+"/search/jql" {
			writeJSON(t, response, http.StatusOK, `{"issues":[]}`)
			return
		}
		posts++
		if posts == 1 {
			writeJSON(t, response, http.StatusInternalServerError, `{}`)
			return
		}
		writeJSON(t, response, http.StatusTooManyRequests, `{}`)
	})
	credentials := &delayedJiraCredentials{isReady: true}
	client, err := jira.New(jira.Config{CloudID: testCloudID, Endpoint: provider.URL}, credentials)
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("jira-flow", "credential-after-send")
	for _, kind := range []sdkgo.FailureKind{sdkgo.FailureAvailability, sdkgo.FailureRateLimit} {
		_, err = sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
		requireRetry(t, err, kind)
		ctx.AdvanceAttempt()
	}
	credentials.isReady = false
	before := len(ctx.Heartbeats())
	_, err = sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Len(t, ctx.Heartbeats(), before, "notSent would hide the earlier possible send")
	ctx.AdvanceAttempt()
	credentials.hasInvalidCredentials = true
	result, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchUncertain, result.Branch, "a terminal credential failure after a possible send is uncertain")
	require.Equal(t, 2, posts)
}
