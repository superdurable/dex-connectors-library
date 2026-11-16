// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github_test

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/github/go"
	"github.com/superdurable/dex-connectors-library/connectors/github/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSetIssueStateClosesAndReopensAnIssue(t *testing.T) {
	provider := newIssueStateProvider(t)
	client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)

	closed, err := sdkgo.RunMutation(testsupport.NewDexContext("issues-flow", "close-issue"), client.SetIssueState(), githubConnection, github.SetIssueStateInput{
		Owner: " octocat ", Repository: "hello-world", IssueNumber: 7, State: " closed ", StateReason: "not_planned",
	})
	require.NoError(t, err)
	require.Equal(t, github.SetIssueStateBranchUpdated, closed.Branch, closed.Failure)
	require.Equal(t, github.UpdatedIssueState{
		Number: 7, URL: "https://github.com/octocat/hello-world/issues/7", State: "closed", StateReason: "not_planned",
		UpdatedAt: timePointer(time.Date(2026, 3, 8, 9, 30, 0, 0, time.UTC)),
	}, closed.Value)
	require.Equal(t, "I_node7", closed.Receipt.ProviderObjectID)
	require.Equal(t, sdkgo.IdempotencyKey(closed.Receipt.CallID), closed.Receipt.IdempotencyKey)

	reopened, err := sdkgo.RunMutation(testsupport.NewDexContext("issues-flow", "reopen-issue"), client.SetIssueState(), githubConnection, github.SetIssueStateInput{
		Owner: "octocat", Repository: "hello-world", IssueNumber: 7, State: github.IssueStateOpen,
	})
	require.NoError(t, err)
	require.Equal(t, github.SetIssueStateBranchUpdated, reopened.Branch, reopened.Failure)
	require.Equal(t, "open", reopened.Value.State)
	require.Equal(t, "reopened", reopened.Value.StateReason)
	require.Equal(t, []map[string]string{{"state": "closed", "state_reason": "not_planned"}, {"state": "open"}}, provider.sentBodies())
	requireRedacted(t, closed)
}

func TestSetIssueStateRepeatsTheSameUpdateAfterAnUnknownOutcome(t *testing.T) {
	provider := newIssueStateProvider(t, http.StatusBadGateway)
	client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)
	ctx := testsupport.NewDexContext("issues-flow", "repeat-close")
	input := github.SetIssueStateInput{Owner: "octocat", Repository: "hello-world", IssueNumber: 7, State: "closed"}

	_, err := sdkgo.RunMutation(ctx, client.SetIssueState(), githubConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	result, err := sdkgo.RunMutation(ctx, client.SetIssueState(), githubConnection, input)
	require.NoError(t, err)
	require.Equal(t, github.SetIssueStateBranchUpdated, result.Branch, result.Failure)
	require.Equal(t, []map[string]string{{"state": "closed"}, {"state": "closed"}}, provider.sentBodies(), "the repeat sends the same absolute state")
}

func TestSetIssueStateRetriesRateLimitsAndUnavailability(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		body     string
		header   map[string]string
		delay    time.Duration
		failure  sdkgo.FailureKind
		hasDelay bool
	}{
		{name: "429", status: http.StatusTooManyRequests, header: map[string]string{"Retry-After": "3"}, delay: 3 * time.Second, failure: sdkgo.FailureRateLimit, hasDelay: true},
		{name: "secondary rate limit", status: http.StatusForbidden, body: `{"message":"You have exceeded a secondary rate limit."}`, delay: time.Minute, failure: sdkgo.FailureRateLimit, hasDelay: true},
		{name: "503", status: http.StatusServiceUnavailable, failure: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				for key, value := range test.header {
					response.Header().Set(key, value)
				}
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(test.body))
			}))
			defer server.Close()
			client := newIssueClient(t, server.URL, github.RepositoryAuthMethodID)
			_, err := sdkgo.RunMutation(testsupport.NewDexContext("issues-flow", "retry"), client.SetIssueState(), githubConnection, closeIssueInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.failure, retry.Failure.Kind)
			if test.hasDelay {
				var retryAfter *dex.RetryAfterError
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.delay, retryAfter.After)
			}
		})
	}
}

func TestSetIssueStateRetriesATransportFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	baseURL := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	client := newIssueClient(t, baseURL, github.RepositoryAuthMethodID)
	_, err = sdkgo.RunMutation(testsupport.NewDexContext("issues-flow", "transport"), client.SetIssueState(), githubConnection, closeIssueInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	require.NotContains(t, err.Error(), "repository-token")
}

func TestSetIssueStateRequiresTheRepositoryAuthMethod(t *testing.T) {
	for _, authMethodID := range []string{github.PublicAuthMethodID, ""} {
		provider := newIssueStateProvider(t)
		client := newIssueClient(t, provider.server.URL, authMethodID)
		result, err := sdkgo.RunMutation(testsupport.NewDexContext("issues-flow", "public-method"), client.SetIssueState(), githubConnection, closeIssueInput())
		require.NoError(t, err)
		require.Equal(t, github.SetIssueStateBranchInsufficientScope, result.Branch)
		require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
		require.Equal(t, github.UpdatedIssueState{Number: 7}, result.Value)
		require.Empty(t, provider.sentBodies())
	}
}

func TestSetIssueStateClassifiesConclusiveAnswers(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{status: http.StatusUnauthorized, branch: github.SetIssueStateBranchAuthorizationRevoked, kind: sdkgo.FailureAuthentication},
		{status: http.StatusForbidden, body: `{"message":"Repository was archived so is read-only."}`, branch: github.SetIssueStateBranchInsufficientScope, kind: sdkgo.FailureAuthorization},
		{status: http.StatusNotFound, branch: github.SetIssueStateBranchNotFound, kind: sdkgo.FailureNotFound},
		{status: http.StatusGone, branch: github.SetIssueStateBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
		{status: http.StatusUnprocessableEntity, branch: github.SetIssueStateBranchProviderRejected, kind: sdkgo.FailureValidation},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			provider := newIssueStateProvider(t, test.status)
			client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)
			result, err := sdkgo.RunMutation(testsupport.NewDexContext("issues-flow", "rejected"), client.SetIssueState(), githubConnection, closeIssueInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, github.UpdatedIssueState{Number: 7}, result.Value)
			require.Len(t, provider.sentBodies(), 1)
			requireRedacted(t, result)
		})
	}
}

func TestSetIssueStateRejectsAnAnswerForAnotherStateOrIssue(t *testing.T) {
	for name, issue := range map[string]map[string]any{
		"unchanged state": issueJSON(7, "Crash", "", "open", map[string]any{"updated_at": "2026-03-08T09:30:00Z"}),
		"another issue":   issueJSON(8, "Crash", "", "closed", map[string]any{"updated_at": "2026-03-08T09:30:00Z"}),
		"no update time":  issueJSON(7, "Crash", "", "closed", map[string]any{"updated_at": nil}),
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				writeJSON(t, response, issue)
			}))
			defer server.Close()
			client := newIssueClient(t, server.URL, github.RepositoryAuthMethodID)
			result, err := sdkgo.RunMutation(testsupport.NewDexContext("issues-flow", "invalid"), client.SetIssueState(), githubConnection, closeIssueInput())
			require.NoError(t, err)
			require.Equal(t, github.SetIssueStateBranchInvalidResponse, result.Branch)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
		})
	}
}

func TestSetIssueStateValidatesInputBeforeProviderAccess(t *testing.T) {
	for name, input := range map[string]github.SetIssueStateInput{
		"invalid owner":           {Owner: "-octocat", Repository: "hello-world", IssueNumber: 7, State: "closed"},
		"zero issue number":       {Owner: "octocat", Repository: "hello-world", State: "closed"},
		"negative issue number":   {Owner: "octocat", Repository: "hello-world", IssueNumber: -1, State: "closed"},
		"unknown state":           {Owner: "octocat", Repository: "hello-world", IssueNumber: 7, State: "all"},
		"reopened with closed":    {Owner: "octocat", Repository: "hello-world", IssueNumber: 7, State: "closed", StateReason: "reopened"},
		"completed with open":     {Owner: "octocat", Repository: "hello-world", IssueNumber: 7, State: "open", StateReason: "completed"},
		"not planned with open":   {Owner: "octocat", Repository: "hello-world", IssueNumber: 7, State: "open", StateReason: "not_planned"},
		"unknown reason":          {Owner: "octocat", Repository: "hello-world", IssueNumber: 7, State: "closed", StateReason: "duplicate"},
		"capitalized state value": {Owner: "octocat", Repository: "hello-world", IssueNumber: 7, State: "Closed"},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newIssueStateProvider(t)
			client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)
			result, err := sdkgo.RunMutation(testsupport.NewDexContext("issues-flow", "defect"), client.SetIssueState(), githubConnection, input)
			require.NoError(t, err)
			require.Equal(t, github.SetIssueStateBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
			require.Empty(t, provider.sentBodies())
		})
	}
}

func closeIssueInput() github.SetIssueStateInput {
	return github.SetIssueStateInput{Owner: "octocat", Repository: "hello-world", IssueNumber: 7, State: "closed", StateReason: "completed"}
}

// issueStateProvider answers each PATCH with the next scripted status, then applies the update.
type issueStateProvider struct {
	server   *httptest.Server
	mutex    sync.Mutex
	statuses []int
	bodies   []map[string]string
}

func newIssueStateProvider(t *testing.T, statuses ...int) *issueStateProvider {
	t.Helper()
	provider := &issueStateProvider{statuses: statuses}
	provider.server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer repository-token", request.Header.Get("Authorization"))
		require.Equal(t, "application/vnd.github+json", request.Header.Get("Accept"))
		require.Equal(t, "2026-03-10", request.Header.Get("X-GitHub-Api-Version"))
		require.Equal(t, http.MethodPatch, request.Method)
		require.Equal(t, "/repos/octocat/hello-world/issues/7", request.URL.Path)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var body map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		provider.mutex.Lock()
		provider.bodies = append(provider.bodies, body)
		status := http.StatusOK
		if len(provider.statuses) > 0 {
			status, provider.statuses = provider.statuses[0], provider.statuses[1:]
		}
		provider.mutex.Unlock()
		if status != http.StatusOK {
			response.WriteHeader(status)
			_, _ = response.Write([]byte(`{"message":"` + providerBodySentinel + `"}`))
			return
		}
		stateReason := body["state_reason"]
		if stateReason == "" && body["state"] == "closed" {
			stateReason = "completed"
		} else if stateReason == "" {
			stateReason = "reopened"
		}
		writeJSON(t, response, issueJSON(7, "Crash", "", body["state"], map[string]any{"state_reason": stateReason, "updated_at": "2026-03-08T09:30:00Z"}))
	}))
	t.Cleanup(provider.server.Close)
	return provider
}

func (provider *issueStateProvider) sentBodies() []map[string]string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]map[string]string(nil), provider.bodies...)
}
