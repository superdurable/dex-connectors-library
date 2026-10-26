// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type laterJiraDexContext struct{ *jiraDexContext }

func (*laterJiraDexContext) Attempt() int32 { return 2 }

type replayJiraDexContext struct {
	*jiraDexContext
	attempt   int32
	heartbeat json.RawMessage
}

func (ctx *replayJiraDexContext) Attempt() int32 { return ctx.attempt }

func (ctx *replayJiraDexContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		ctx.heartbeat = encoded
	}
	return err
}

func (ctx *replayJiraDexContext) GetLastHeartbeatValue(destination any) (bool, error) {
	if len(ctx.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(ctx.heartbeat, destination)
}

func TestLaterCreateAttemptWithoutDispatchCheckpointDoesNotResend(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"10043","key":"OPS-43"}`)
	})
	client := newJiraClient(t, provider.URL)
	result, err := sdkgo.RunMutation(&laterJiraDexContext{newJiraDexContext("create-later")}, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchUncertain, result.Branch)
	require.Zero(t, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue"))
}

func TestLaterCommentAttemptWithoutDispatchCheckpointDoesNotResend(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"10500"}`)
	})
	client := newJiraClient(t, provider.URL)
	result, err := sdkgo.RunMutation(&laterJiraDexContext{newJiraDexContext("comment-later")}, client.AddComment(), jiraConnection,
		jira.AddCommentInput{IssueIDOrKey: "OPS-43", Body: "Triage started."})
	require.NoError(t, err)
	require.Equal(t, jira.AddCommentBranchUncertain, result.Branch)
	require.Zero(t, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue/OPS-43/comment"))
}

func TestAcceptedIssueIsReadBackWithoutResending(t *testing.T) {
	var markerValue string
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/issue":
			var body struct {
				Properties []struct {
					Value string `json:"value"`
				} `json:"properties"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			markerValue = body.Properties[0].Value
			writeJSON(t, response, http.StatusCreated, `{"id":"10043","key":"OPS-43"}`)
		case request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/search/jql":
			writeJSON(t, response, http.StatusOK, `{"issues":[{"id":"10043","key":"OPS-43","fields":{"summary":"Fire panel wiring"}}]}`)
		case request.Method == http.MethodGet && request.URL.Path == testSitePrefix+"/issue/10043/properties/dex.connector.call":
			writeJSON(t, response, http.StatusOK, `{"value":"`+markerValue+`"}`)
		default:
			writeJSON(t, response, http.StatusNotFound, `{}`)
		}
	})
	client := newJiraClient(t, provider.URL)
	ctx := &replayJiraDexContext{jiraDexContext: newJiraDexContext("create-replay"), attempt: 1}
	first, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, first.Branch)
	ctx.attempt = 2
	second, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, second.Branch)
	require.Equal(t, "OPS-43", second.Value.IssueKey)
	require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue"))
}

func TestAcceptedCommentIsReadBackWithoutResending(t *testing.T) {
	var markerValue string
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/issue/OPS-43/comment":
			var body struct {
				Properties []struct {
					Value string `json:"value"`
				} `json:"properties"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			markerValue = body.Properties[0].Value
			writeJSON(t, response, http.StatusCreated, `{"id":"10500","created":"2026-09-30T10:05:00.000-0700"}`)
		case request.Method == http.MethodGet && request.URL.Path == testSitePrefix+"/issue/OPS-43/comment":
			writeJSON(t, response, http.StatusOK, `{"comments":[{"id":"10500","created":"2026-09-30T10:05:00.000-0700"}]}`)
		case request.Method == http.MethodGet && request.URL.Path == testSitePrefix+"/comment/10500/properties/dex.connector.call":
			writeJSON(t, response, http.StatusOK, `{"value":"`+markerValue+`"}`)
		default:
			writeJSON(t, response, http.StatusNotFound, `{}`)
		}
	})
	client := newJiraClient(t, provider.URL)
	ctx := &replayJiraDexContext{jiraDexContext: newJiraDexContext("comment-replay"), attempt: 1}
	input := jira.AddCommentInput{IssueIDOrKey: "OPS-43", Body: "Triage started."}
	first, err := sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	require.NoError(t, err)
	require.Equal(t, jira.AddCommentBranchAdded, first.Branch)
	ctx.attempt = 2
	second, err := sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	require.NoError(t, err)
	require.Equal(t, jira.AddCommentBranchAdded, second.Branch)
	require.Equal(t, "10500", second.Value.CommentID)
	require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue/OPS-43/comment"))
}

func TestUnavailableIssueRetriesWithRecordedSafeMarker(t *testing.T) {
	postCount := 0
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/issue" {
			postCount++
			if postCount == 1 {
				writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
				return
			}
			writeJSON(t, response, http.StatusCreated, `{"id":"10043","key":"OPS-43"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"issues":[]}`)
	})
	client := newJiraClient(t, provider.URL)
	ctx := &replayJiraDexContext{jiraDexContext: newJiraDexContext("create-503-retry"), attempt: 1}
	_, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	ctx.attempt = 2
	result, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue"))
}

func TestAmbiguousIssueResponseFindsProviderMarker(t *testing.T) {
	var markerValue string
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/issue":
			var body struct {
				Properties []struct {
					Value string `json:"value"`
				} `json:"properties"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			markerValue = body.Properties[0].Value
			writeJSON(t, response, http.StatusBadGateway, `{}`)
		case request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/search/jql":
			writeJSON(t, response, http.StatusOK, `{"issues":[{"id":"10043","key":"OPS-43","fields":{"summary":"Fire panel wiring"}}]}`)
		case request.Method == http.MethodGet && request.URL.Path == testSitePrefix+"/issue/10043/properties/dex.connector.call":
			writeJSON(t, response, http.StatusOK, `{"value":"`+markerValue+`"}`)
		default:
			writeJSON(t, response, http.StatusNotFound, `{}`)
		}
	})
	client := newJiraClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newJiraDexContext("create-ambiguous-marker"), client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, result.Branch)
	require.Equal(t, "OPS-43", result.Value.IssueKey)
	require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue"))
}

func TestUnavailableCommentWithProviderMarkerDoesNotRetry(t *testing.T) {
	var markerValue string
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/issue/OPS-43/comment":
			var body struct {
				Properties []struct {
					Value string `json:"value"`
				} `json:"properties"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			markerValue = body.Properties[0].Value
			writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
		case request.Method == http.MethodGet && request.URL.Path == testSitePrefix+"/issue/OPS-43/comment":
			writeJSON(t, response, http.StatusOK, `{"comments":[{"id":"10500","created":"2026-09-30T10:05:00.000-0700"}]}`)
		case request.Method == http.MethodGet && request.URL.Path == testSitePrefix+"/comment/10500/properties/dex.connector.call":
			writeJSON(t, response, http.StatusOK, `{"value":"`+markerValue+`"}`)
		default:
			writeJSON(t, response, http.StatusNotFound, `{}`)
		}
	})
	client := newJiraClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newJiraDexContext("comment-503-marker"), client.AddComment(), jiraConnection,
		jira.AddCommentInput{IssueIDOrKey: "OPS-43", Body: "Triage started."})
	require.NoError(t, err)
	require.Equal(t, jira.AddCommentBranchAdded, result.Branch)
	require.Equal(t, "10500", result.Value.CommentID)
	require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue/OPS-43/comment"))
}

func TestWriteDoesNotStartWhenExecuteDeadlineIsTooNear(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"10043","key":"OPS-43"}`)
	})
	client := newJiraClient(t, provider.URL)
	stepContext, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	ctx := &jiraDexContext{Context: stepContext, step: "deadline-cut"}
	_, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Zero(t, provider.requestCount())
}
