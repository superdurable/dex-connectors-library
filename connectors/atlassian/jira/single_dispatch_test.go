// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
	"github.com/superdurable/dex/sdk-go/dex"
)

func laterFenceContext(step string) *singledispatchtest.Context {
	ctx := singledispatchtest.NewContext("jira-flow", step)
	ctx.AdvanceAttempt()
	return ctx
}

func TestLaterCreateAttemptWithoutDispatchCheckpointDoesNotResend(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"10043","key":"OPS-43"}`)
	})
	client := newJiraClient(t, provider.URL)
	result, err := sdkgo.RunMutation(laterFenceContext("create-later"), client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchUncertain, result.Branch)
	require.Zero(t, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue"))
}

func TestLaterCommentAttemptWithoutDispatchCheckpointDoesNotResend(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"10500"}`)
	})
	client := newJiraClient(t, provider.URL)
	result, err := sdkgo.RunMutation(laterFenceContext("comment-later"), client.AddComment(), jiraConnection,
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
		case request.Method == http.MethodGet && request.URL.Path == testSitePrefix+"/issue/10043":
			writeJSON(t, response, http.StatusOK, `{"id":"10043","key":"OPS-43","fields":{"summary":"Fire panel wiring"}}`)
		case request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/search/jql":
			writeJSON(t, response, http.StatusOK, `{"issues":[{"id":"10043","key":"OPS-43","fields":{"summary":"Fire panel wiring"}}]}`)
		case request.Method == http.MethodGet && request.URL.Path == testSitePrefix+"/issue/10043/properties/dex.connector.call":
			writeJSON(t, response, http.StatusOK, `{"value":"`+markerValue+`"}`)
		default:
			writeJSON(t, response, http.StatusNotFound, `{}`)
		}
	})
	client := newJiraClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("jira-flow", "create-replay")
	first, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, first.Branch)
	ctx.AdvanceAttempt()
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
		case request.Method == http.MethodGet && request.URL.Path == testSitePrefix+"/issue/OPS-43/comment/10500":
			writeJSON(t, response, http.StatusOK, `{"id":"10500","created":"2026-09-30T10:05:00.000-0700"}`)
		case request.Method == http.MethodGet && request.URL.Path == testSitePrefix+"/issue/OPS-43/comment":
			writeJSON(t, response, http.StatusOK, `{"comments":[{"id":"10500","created":"2026-09-30T10:05:00.000-0700"}]}`)
		case request.Method == http.MethodGet && request.URL.Path == testSitePrefix+"/comment/10500/properties/dex.connector.call":
			writeJSON(t, response, http.StatusOK, `{"value":"`+markerValue+`"}`)
		default:
			writeJSON(t, response, http.StatusNotFound, `{}`)
		}
	})
	client := newJiraClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("jira-flow", "comment-replay")
	input := jira.AddCommentInput{IssueIDOrKey: "OPS-43", Body: "Triage started."}
	first, err := sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	require.NoError(t, err)
	require.Equal(t, jira.AddCommentBranchAdded, first.Branch)
	ctx.AdvanceAttempt()
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
				response.Header().Set("Retry-After", "7")
				writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
				return
			}
			writeJSON(t, response, http.StatusCreated, `{"id":"10043","key":"OPS-43"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"issues":[]}`)
	})
	client := newJiraClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("jira-flow", "create-503-retry")
	_, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 7*time.Second, retryAfter.After)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue"))
	require.Zero(t, provider.requestCountFor(http.MethodPost, testSitePrefix+"/search/jql"))
}

func TestUnavailableCommentRetriesWithRecordedSafeMarker(t *testing.T) {
	postCount := 0
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/issue/OPS-43/comment" {
			postCount++
			if postCount == 1 {
				response.Header().Set("Retry-After", "7")
				writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
				return
			}
			writeJSON(t, response, http.StatusCreated, `{"id":"10500"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"comments":[]}`)
	})
	client := newJiraClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("jira-flow", "comment-503-retry")
	input := jira.AddCommentInput{IssueIDOrKey: "OPS-43", Body: "Triage started."}
	_, err := sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 7*time.Second, retryAfter.After)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	require.NoError(t, err)
	require.Equal(t, jira.AddCommentBranchAdded, result.Branch)
	require.Equal(t, 2, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue/OPS-43/comment"))
	require.Zero(t, provider.requestCountFor(http.MethodGet, testSitePrefix+"/issue/OPS-43/comment"))
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

func TestAmbiguousCommentWithProviderMarkerDoesNotRetry(t *testing.T) {
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
			writeJSON(t, response, http.StatusBadGateway, `{}`)
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
	stepContext := singledispatchtest.NewContext("jira-flow", "deadline-cut")
	cancel := stepContext.SetDeadline(time.Now().Add(500 * time.Millisecond))
	defer cancel()
	_, err := sdkgo.RunMutation(stepContext, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Zero(t, provider.requestCount())
	stepContext.AdvanceAttempt()
	longCancel := stepContext.SetDeadline(time.Now().Add(30 * time.Second))
	defer longCancel()
	result, err := sdkgo.RunMutation(stepContext, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue"))
}

func TestStaleCanResendMarkerNeverAuthorizesThirdIssueWrite(t *testing.T) {
	posts := 0
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/issue" {
			posts++
			if posts == 1 {
				writeJSON(t, response, http.StatusTooManyRequests, `{}`)
				return
			}
			writeJSON(t, response, http.StatusCreated, `{"id":"10043","key":"OPS-43"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"issues":[]}`)
	})
	client := newJiraClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("jira-flow", "stale-marker")
	_, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureRateLimit)
	frames := ctx.Heartbeats()
	var firstMarker singledispatch.Marker
	require.NoError(t, json.Unmarshal(frames[len(frames)-1], &firstMarker))
	require.Equal(t, singledispatch.StateCanResend, firstMarker.State)
	ctx.AdvanceAttempt()
	second, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, second.Branch)
	ctx.AdvanceAttempt()
	require.NoError(t, ctx.SetPersistedCheckpoint(firstMarker))
	third, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchUncertain, third.Branch)
	require.Equal(t, 2, posts)
}

func TestStaleCanResendMarkerNeverAuthorizesThirdCommentWrite(t *testing.T) {
	postCount := 0
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/issue/OPS-43/comment" {
			postCount++
			if postCount == 1 {
				writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
				return
			}
			writeJSON(t, response, http.StatusCreated, `{"id":"10500"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"comments":[]}`)
	})
	client := newJiraClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("jira-flow", "stale-comment-marker")
	input := jira.AddCommentInput{IssueIDOrKey: "OPS-43", Body: "Triage started."}
	_, err := sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	frames := ctx.Heartbeats()
	var firstMarker singledispatch.Marker
	require.NoError(t, json.Unmarshal(frames[len(frames)-1], &firstMarker))
	require.Equal(t, singledispatch.StateCanResend, firstMarker.State)
	ctx.AdvanceAttempt()
	second, err := sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	require.NoError(t, err)
	require.Equal(t, jira.AddCommentBranchAdded, second.Branch)
	ctx.AdvanceAttempt()
	require.NoError(t, ctx.SetPersistedCheckpoint(firstMarker))
	third, err := sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	require.NoError(t, err)
	require.Equal(t, jira.AddCommentBranchUncertain, third.Branch)
	require.Equal(t, 2, postCount)
}

type delayedJiraCredentials struct{ isReady bool }

func (provider *delayedJiraCredentials) Resolve(sdkgo.Call) (jira.Credentials, error) {
	if !provider.isReady {
		return jira.Credentials{}, errors.New("credential broker temporarily unavailable")
	}
	return jira.Credentials{AccessToken: sdkgo.NewSecretString(testAccessToken)}, nil
}

func (provider *delayedJiraCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[jira.Credentials]) (jira.Credentials, error) {
	return provider.Resolve(call)
}

func TestCredentialFailureRecordsNotSentBeforeNextIssueAttempt(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"10043","key":"OPS-43"}`)
	})
	credentials := &delayedJiraCredentials{}
	client, err := jira.New(jira.Config{CloudID: testCloudID, Endpoint: provider.URL}, credentials)
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("jira-flow", "credential-retry")
	_, err = sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureAuthentication)
	require.Zero(t, provider.requestCount())
	credentials.isReady = true
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue"))
}

func TestCredentialFailureRecordsNotSentBeforeNextCommentAttempt(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"10500"}`)
	})
	credentials := &delayedJiraCredentials{}
	client, err := jira.New(jira.Config{CloudID: testCloudID, Endpoint: provider.URL}, credentials)
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("jira-flow", "comment-credential-retry")
	input := jira.AddCommentInput{IssueIDOrKey: "OPS-43", Body: "Triage started."}
	_, err = sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	requireRetry(t, err, sdkgo.FailureAuthentication)
	require.Zero(t, provider.requestCount())
	credentials.isReady = true
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	require.NoError(t, err)
	require.Equal(t, jira.AddCommentBranchAdded, result.Branch)
	require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue/OPS-43/comment"))
}

func TestTransientIssueReadbackRetriesWithoutAnotherCreate(t *testing.T) {
	var markerValue string
	searches := 0
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/issue":
			var body struct {
				Properties []writeMarkerPropertyForTest `json:"properties"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			markerValue = body.Properties[0].Value
			writeJSON(t, response, http.StatusBadGateway, `{}`)
		case request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/search/jql":
			searches++
			if searches == 1 {
				writeJSON(t, response, http.StatusTooManyRequests, `{}`)
				return
			}
			writeJSON(t, response, http.StatusOK, `{"issues":[{"id":"10043","key":"OPS-43","fields":{"summary":"Fire panel wiring"},"properties":{"dex.connector.call":"`+markerValue+`"}}]}`)
		default:
			writeJSON(t, response, http.StatusNotFound, `{}`)
		}
	})
	client := newJiraClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("jira-flow", "readback-retry")
	_, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue"))
}

func TestReadbackCredentialFailureRetriesWithoutAnotherIssue(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/issue" {
			writeJSON(t, response, http.StatusBadGateway, `{}`)
			return
		}
		writeJSON(t, response, http.StatusTooManyRequests, `{}`)
	})
	credentials := &delayedJiraCredentials{isReady: true}
	client, err := jira.New(jira.Config{CloudID: testCloudID, Endpoint: provider.URL}, credentials)
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("jira-flow", "issue-readback-credential")
	_, err = sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	credentials.isReady = false
	ctx.AdvanceAttempt()
	_, err = sdkgo.RunMutation(ctx, client.CreateIssue(), jiraConnection, validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureAuthentication)
	require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue"))
}

func TestReadbackCredentialFailureRetriesWithoutAnotherComment(t *testing.T) {
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/issue/OPS-43/comment" {
			writeJSON(t, response, http.StatusBadGateway, `{}`)
			return
		}
		writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
	})
	credentials := &delayedJiraCredentials{isReady: true}
	client, err := jira.New(jira.Config{CloudID: testCloudID, Endpoint: provider.URL}, credentials)
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("jira-flow", "comment-readback-credential")
	input := jira.AddCommentInput{IssueIDOrKey: "OPS-43", Body: "Triage started."}
	_, err = sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	credentials.isReady = false
	ctx.AdvanceAttempt()
	_, err = sdkgo.RunMutation(ctx, client.AddComment(), jiraConnection, input)
	requireRetry(t, err, sdkgo.FailureAuthentication)
	require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue/OPS-43/comment"))
}

type writeMarkerPropertyForTest struct {
	Value string `json:"value"`
}

func TestConflictIssueReadsProviderPropertyBeforeSelectingOutcome(t *testing.T) {
	var markerValue string
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/issue":
			var body struct {
				Properties []writeMarkerPropertyForTest `json:"properties"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			markerValue = body.Properties[0].Value
			writeJSON(t, response, http.StatusConflict, `{}`)
		case request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/search/jql":
			writeJSON(t, response, http.StatusOK, `{"issues":[{"id":"10043","key":"OPS-43","fields":{"summary":"Fire panel wiring"},"properties":{"dex.connector.call":"`+markerValue+`"}}]}`)
		default:
			writeJSON(t, response, http.StatusNotFound, `{}`)
		}
	})
	client := newJiraClient(t, provider.URL)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("jira-flow", "create-409"), client.CreateIssue(), jiraConnection, validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, jira.CreateIssueBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCountFor(http.MethodPost, testSitePrefix+"/issue"))
}

func TestConflictCommentReadsProviderPropertyBeforeSelectingOutcome(t *testing.T) {
	var markerValue string
	provider := newRecordingJira(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == testSitePrefix+"/issue/OPS-43/comment":
			var body struct {
				Properties []writeMarkerPropertyForTest `json:"properties"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			markerValue = body.Properties[0].Value
			writeJSON(t, response, http.StatusConflict, `{}`)
		case request.Method == http.MethodGet && request.URL.Path == testSitePrefix+"/issue/OPS-43/comment":
			writeJSON(t, response, http.StatusOK, `{"comments":[{"id":"10500","properties":[{"key":"dex.connector.call","value":"`+markerValue+`"}]}]}`)
		default:
			writeJSON(t, response, http.StatusNotFound, `{}`)
		}
	})
	client := newJiraClient(t, provider.URL)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("jira-flow", "comment-409"), client.AddComment(), jiraConnection,
		jira.AddCommentInput{IssueIDOrKey: "OPS-43", Body: "Triage started."})
	require.NoError(t, err)
	require.Equal(t, jira.AddCommentBranchAdded, result.Branch)
	require.Equal(t, 2, provider.requestCount())
}
