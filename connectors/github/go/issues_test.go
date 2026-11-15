// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/github/go"
	"github.com/superdurable/dex-connectors-library/connectors/github/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
	"github.com/superdurable/dex/sdk-go/dex"
)

const issueMarkerPrefix = "<!-- dex-connector-call: "

func TestListIssuesFiltersBoundsBodiesAndLeavesOutPullRequests(t *testing.T) {
	updatedSince := time.Date(2026, 3, 1, 8, 30, 15, 900, time.FixedZone("PST", -8*60*60))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requireGitHubHeaders(t, request)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/repos/octocat/hello-world/issues", request.URL.Path)
		require.Equal(t, map[string][]string{
			"state": {"all"}, "labels": {"bug,needs triage"}, "creator": {"mona"}, "since": {"2026-03-01T16:30:15Z"},
			"sort": {"created"}, "direction": {"desc"}, "per_page": {"3"}, "page": {"2"},
		}, map[string][]string(request.URL.Query()))
		response.Header().Set("X-OAuth-Scopes", "read:user, user:email")
		response.Header().Set("Link", `<https://api.github.com/repositories/1/issues?page=3>; rel="next", <https://api.github.com/repositories/1/issues?page=9>; rel="last"`)
		writeJSON(t, response, []map[string]any{
			issueJSON(12, "Crash on save", "Steps: opené and save", "open", nil),
			{"number": 11, "title": "A pull request", "state": "open", "pull_request": map[string]any{"url": "https://api.github.com/repos/octocat/hello-world/pulls/11"}},
			issueJSON(9, "Slow startup", "", "closed", map[string]any{"state_reason": "completed", "closed_at": "2026-03-04T16:00:00Z", "labels": []any{}, "assignees": []any{}}),
		})
	}))
	defer server.Close()
	client := newClient(t, server.URL, github.Config{})

	result, err := sdkgo.RunQuery(testsupport.NewDexContext("issues-flow", "list-issues"), client.ListIssues(), githubConnection, github.ListIssuesInput{
		Owner: " octocat ", Repository: "hello-world", State: "all", Labels: []string{"bug", " needs triage "}, Creator: "mona",
		UpdatedSince: &updatedSince, PageSize: 3, Page: 2, MaxBodyCharacters: 10,
	})
	require.NoError(t, err)
	require.Equal(t, github.ListIssuesBranchListed, result.Branch, result.Failure)
	require.Equal(t, 3, result.Value.NextPage)
	require.Len(t, result.Value.Issues, 2, "the pull request is left out")
	first := result.Value.Issues[0]
	require.Equal(t, 12, first.Number)
	require.Equal(t, "I_node12", first.NodeID)
	require.Equal(t, "Crash on save", first.Title)
	require.Equal(t, "Steps: ope", first.Body)
	require.True(t, first.BodyTruncated)
	require.Equal(t, "open", first.State)
	require.Equal(t, "https://github.com/octocat/hello-world/issues/12", first.URL)
	require.Equal(t, "mona", first.AuthorLogin)
	require.Equal(t, []string{"bug"}, first.Labels)
	require.Equal(t, []string{"hubot"}, first.Assignees)
	require.Equal(t, 2, first.Comments)
	require.Equal(t, time.Date(2026, 3, 6, 10, 0, 0, 0, time.UTC), first.CreatedAt)
	require.Nil(t, first.ClosedAt)
	closed := result.Value.Issues[1]
	require.Equal(t, "completed", closed.StateReason)
	require.Empty(t, closed.Body)
	require.False(t, closed.BodyTruncated)
	require.Equal(t, time.Date(2026, 3, 4, 16, 0, 0, 0, time.UTC), *closed.ClosedAt)
	require.Empty(t, closed.Labels)
	requireRedacted(t, result)
}

func TestListIssuesUsesOpenStateAndDefaultPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, map[string][]string{
			"state": {"open"}, "sort": {"created"}, "direction": {"desc"}, "per_page": {"30"}, "page": {"1"},
		}, map[string][]string(request.URL.Query()))
		response.Header().Set("X-OAuth-Scopes", "repo")
		writeJSON(t, response, []any{})
	}))
	defer server.Close()
	client := newIssueClient(t, server.URL, github.RepositoryAuthMethodID)

	result, err := sdkgo.RunQuery(testsupport.NewDexContext("issues-flow", "list-open"), client.ListIssues(), githubConnection,
		github.ListIssuesInput{Owner: "octocat", Repository: "private-repository"})
	require.NoError(t, err)
	require.Equal(t, github.ListIssuesBranchListed, result.Branch, result.Failure)
	require.Empty(t, result.Value.Issues)
	require.NotNil(t, result.Value.Issues)
	require.Zero(t, result.Value.NextPage)
}

func TestListIssuesRequiresTheExactScopesOfTheConnectionMethod(t *testing.T) {
	for _, test := range []struct {
		name          string
		authMethodID  string
		grantedScopes string
		expected      sdkgo.BranchID
	}{
		{name: "public method", authMethodID: github.PublicAuthMethodID, grantedScopes: "read:user, user:email", expected: github.ListIssuesBranchListed},
		{name: "legacy blank method", grantedScopes: "user:email,read:user", expected: github.ListIssuesBranchListed},
		{name: "repository method", authMethodID: github.RepositoryAuthMethodID, grantedScopes: "repo", expected: github.ListIssuesBranchListed},
		{name: "public method with repo", authMethodID: github.PublicAuthMethodID, grantedScopes: "read:user, user:email, repo", expected: github.ListIssuesBranchInsufficientScope},
		{name: "repository method with public scopes", authMethodID: github.RepositoryAuthMethodID, grantedScopes: "read:user, user:email", expected: github.ListIssuesBranchInsufficientScope},
		{name: "repository method with extra scope", authMethodID: github.RepositoryAuthMethodID, grantedScopes: "repo, admin:org", expected: github.ListIssuesBranchInsufficientScope},
		{name: "unknown method", authMethodID: "github-app", grantedScopes: "repo", expected: github.ListIssuesBranchInsufficientScope},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("X-OAuth-Scopes", test.grantedScopes)
				writeJSON(t, response, []any{})
			}))
			defer server.Close()
			client := newIssueClient(t, server.URL, test.authMethodID)
			result, err := sdkgo.RunQuery(testsupport.NewDexContext("issues-flow", "scopes"), client.ListIssues(), githubConnection,
				github.ListIssuesInput{Owner: "octocat", Repository: "hello-world"})
			require.NoError(t, err)
			require.Equal(t, test.expected, result.Branch)
		})
	}
}

func TestExistingQueriesKeepRejectingTheRepositoryScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("X-OAuth-Scopes", "repo")
		writeJSON(t, response, []any{})
	}))
	defer server.Close()
	client := newIssueClient(t, server.URL, github.RepositoryAuthMethodID)
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("issues-flow", "releases"), client.ListReleases(), githubConnection,
		github.ListReleasesInput{Owner: "octocat", Repository: "hello-world"})
	require.NoError(t, err)
	require.Equal(t, github.ListReleasesBranchInsufficientScope, result.Branch, "a repo grant never reaches the public-only queries")
}

func TestListIssuesClassifiesProviderResponses(t *testing.T) {
	for _, test := range []struct {
		status   int
		header   map[string]string
		body     string
		expected sdkgo.BranchID
		retry    time.Duration
	}{
		{status: http.StatusUnauthorized, expected: github.ListIssuesBranchAuthorizationRevoked},
		{status: http.StatusForbidden, body: `{"message":"Resource not accessible"}`, expected: github.ListIssuesBranchInsufficientScope},
		{status: http.StatusNotFound, expected: github.ListIssuesBranchNotFound},
		{status: http.StatusGone, body: `{"message":"Issues are disabled for this repo"}`, expected: github.ListIssuesBranchProviderRejected},
		{status: http.StatusUnprocessableEntity, expected: github.ListIssuesBranchProviderRejected},
		{status: http.StatusOK, body: `{"message":"` + providerBodySentinel + `"}`, expected: github.ListIssuesBranchInvalidResponse},
		{status: http.StatusOK, body: `null`, expected: github.ListIssuesBranchInvalidResponse},
		{status: http.StatusOK, body: `[{"number":0,"state":"open","created_at":"2026-03-06T10:00:00Z","updated_at":"2026-03-06T10:00:00Z"}]`, expected: github.ListIssuesBranchInvalidResponse},
		{status: http.StatusOK, body: `[{"number":4,"state":"","created_at":"2026-03-06T10:00:00Z","updated_at":"2026-03-06T10:00:00Z"}]`, expected: github.ListIssuesBranchInvalidResponse},
		{status: http.StatusOK, body: `[{"number":4,"state":"open","updated_at":"2026-03-06T10:00:00Z"}]`, expected: github.ListIssuesBranchInvalidResponse},
		{status: http.StatusOK, header: map[string]string{"Link": `<https://api.github.com/repositories/1/issues?page=1>; rel="next"`}, body: `[]`, expected: github.ListIssuesBranchInvalidResponse},
		{status: http.StatusTooManyRequests, header: map[string]string{"Retry-After": "7"}, retry: 7 * time.Second},
		{status: http.StatusForbidden, body: `{"message":"You have exceeded a secondary rate limit."}`, retry: time.Minute},
		{status: http.StatusBadGateway},
	} {
		t.Run(fmt.Sprintf("%d %s", test.status, test.body), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				setGrantedScopes(response)
				for name, value := range test.header {
					response.Header().Set(name, value)
				}
				response.WriteHeader(test.status)
				_, _ = io.WriteString(response, test.body)
			}))
			defer server.Close()
			client := newClient(t, server.URL, github.Config{})
			result, err := sdkgo.RunQuery(testsupport.NewDexContext("issues-flow", "classify"), client.ListIssues(), githubConnection,
				github.ListIssuesInput{Owner: "octocat", Repository: "hello-world"})
			if test.expected == "" {
				require.Error(t, err)
				var retryAfter *dex.RetryAfterError
				if test.retry > 0 {
					require.ErrorAs(t, err, &retryAfter)
					require.Equal(t, test.retry, retryAfter.After)
				}
				require.NotContains(t, err.Error(), "one-use-token")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expected, result.Branch)
			requireRedacted(t, result)
		})
	}
}

func TestListIssuesValidatesInputBeforeProviderAccess(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	client := newClient(t, server.URL, github.Config{})
	zeroTime := time.Time{}
	valid := github.ListIssuesInput{Owner: "octocat", Repository: "hello-world"}
	for name, mutate := range map[string]func(*github.ListIssuesInput){
		"owner":              func(input *github.ListIssuesInput) { input.Owner = "octo cat" },
		"repository":         func(input *github.ListIssuesInput) { input.Repository = ".." },
		"state":              func(input *github.ListIssuesInput) { input.State = "Open" },
		"label comma":        func(input *github.ListIssuesInput) { input.Labels = []string{"bug,ui"} },
		"blank label":        func(input *github.ListIssuesInput) { input.Labels = []string{" "} },
		"label line break":   func(input *github.ListIssuesInput) { input.Labels = []string{"bug\nui"} },
		"too many labels":    func(input *github.ListIssuesInput) { input.Labels = make([]string, 101) },
		"creator":            func(input *github.ListIssuesInput) { input.Creator = "-mona" },
		"zero updated since": func(input *github.ListIssuesInput) { input.UpdatedSince = &zeroTime },
		"page size":          func(input *github.ListIssuesInput) { input.PageSize = 101 },
		"page":               func(input *github.ListIssuesInput) { input.Page = -1 },
		"body bound":         func(input *github.ListIssuesInput) { input.MaxBodyCharacters = 16385 },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			result, err := sdkgo.RunQuery(testsupport.NewDexContext("issues-flow", "invalid-"+name), client.ListIssues(), githubConnection, input)
			require.NoError(t, err)
			require.Equal(t, github.ListIssuesBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Zero(t, requests)
}

func TestCreateIssueSendsAMarkedBodyAndReturnsTheIssue(t *testing.T) {
	provider := newIssueProvider(t)
	client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
	ctx := singledispatchtest.NewContext("issues-flow", "create-issue")

	result, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, github.CreateIssueInput{
		Owner: "octocat", Repository: "hello-world", Title: "  Crash when saving an empty draft ",
		Body: "Steps:\n1. Open a draft.\n2. Save it.", Labels: []string{"bug", " needs triage "}, Assignees: []string{"hubot"},
	})
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueBranchCreated, result.Branch, result.Failure)
	require.Equal(t, github.CreatedIssue{
		Owner: "octocat", Repository: "hello-world", Title: "Crash when saving an empty draft", Number: 1, ID: 9001,
		NodeID: "I_node1", URL: "https://github.com/octocat/hello-world/issues/1", State: "open",
		CreatedAt: timePointer(time.Date(2026, 3, 7, 10, 15, 0, 0, time.UTC)), Labels: []string{"bug", "needs triage"}, Assignees: []string{"hubot"},
	}, result.Value)
	require.Equal(t, "I_node1", result.Receipt.ProviderObjectID)
	require.Equal(t, sdkgo.IdempotencyKey(result.Receipt.CallID), result.Receipt.IdempotencyKey)

	sent := provider.sentIssues()
	require.Len(t, sent, 1)
	require.Equal(t, "Crash when saving an empty draft", sent[0].Title)
	require.True(t, strings.HasPrefix(sent[0].Body, "Steps:\n1. Open a draft.\n2. Save it.\n\n"+issueMarkerPrefix), sent[0].Body)
	require.Regexp(t, `<!-- dex-connector-call: [0-9a-f]{64} -->$`, sent[0].Body)
	require.Equal(t, []string{"bug", "needs triage"}, sent[0].Labels)
	require.Equal(t, []string{"hubot"}, sent[0].Assignees)
	require.NotContains(t, sent[0].Body, "issues-flow", "the marker holds a digest, never the Flow ID")
	require.Equal(t, 1, provider.count("POST"))
	require.Zero(t, provider.count("GET"), "an accepted issue needs no read-back")
	require.Equal(t, singledispatch.Marker{CallID: result.Receipt.CallID, Attempt: 1, State: singledispatch.StateClaimed, ProviderID: "1", SendCount: 1}, lastMarker(t, ctx))
	requireRedacted(t, result)
}

func TestCreateIssueSendsOnlyTheMarkerForAnEmptyBody(t *testing.T) {
	provider := newIssueProvider(t)
	client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("issues-flow", "empty-body"), client.CreateIssue(), githubConnection,
		github.CreateIssueInput{Owner: "octocat", Repository: "hello-world", Title: "Title only"})
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueBranchCreated, result.Branch, result.Failure)
	sent := provider.sentIssues()
	require.Regexp(t, `^<!-- dex-connector-call: [0-9a-f]{64} -->$`, sent[0].Body)
	require.Nil(t, sent[0].Labels)
	require.Nil(t, sent[0].Assignees)
}

func TestCreateIssueRequiresTheRepositoryAuthMethod(t *testing.T) {
	for _, authMethodID := range []string{github.PublicAuthMethodID, ""} {
		provider := newIssueProvider(t)
		client := newIssueClient(t, provider.URL, authMethodID)
		result, err := sdkgo.RunMutation(singledispatchtest.NewContext("issues-flow", "public-method"), client.CreateIssue(), githubConnection, bugReportInput())
		require.NoError(t, err)
		require.Equal(t, github.CreateIssueBranchInsufficientScope, result.Branch)
		require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
		require.Equal(t, github.CreatedIssue{Owner: "octocat", Repository: "hello-world", Title: "Crash when saving an empty draft"}, result.Value)
		require.Zero(t, provider.count("POST")+provider.count("GET"))
	}
}

func TestCreateIssueReadsBackAnIssueThatAnUnknownOutcomeCreated(t *testing.T) {
	for _, behavior := range []issueWriteBehavior{storeThenAnswer502, storeThenCloseConnection} {
		t.Run(string(behavior), func(t *testing.T) {
			provider := newIssueProvider(t, behavior)
			provider.addIssueByAnotherAccount()
			client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
			ctx := singledispatchtest.NewContext("issues-flow", "unknown-created")

			result, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
			require.NoError(t, err)
			require.Equal(t, github.CreateIssueBranchCreated, result.Branch, result.Failure)
			require.Equal(t, 2, result.Value.Number)
			require.Equal(t, "true", result.Receipt.Metadata["foundByReadBack"])
			require.Equal(t, 1, provider.count("POST"), "read-back found the issue, so nothing was sent again")
			require.Equal(t, 1, provider.count("GET /user"))
			require.Equal(t, []string{"creator=octocat&direction=desc&page=1&per_page=30&sort=created&state=all"}, provider.listQueries())
		})
	}
}

func TestCreateIssueResendsAfterReadBackFindsNoIssueBelowTheSendCap(t *testing.T) {
	provider := newIssueProvider(t, answer502WithoutStoring)
	client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
	ctx := singledispatchtest.NewContext("issues-flow", "resend")

	_, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.Error(t, err, "an unconfirmed send below the cap retries")
	require.Equal(t, singledispatch.StateClaimed, lastMarker(t, ctx).State)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueBranchCreated, result.Branch, result.Failure)
	require.Equal(t, 2, provider.count("POST"))
	require.Len(t, provider.sentIssues(), 2)
	require.Equal(t, provider.sentIssues()[0].Body, provider.sentIssues()[1].Body, "every send carries the same marker")
	require.Len(t, provider.storedIssues(), 1)
	require.Equal(t, int32(2), lastMarker(t, ctx).SendCount)
}

func TestCreateIssueSelectsUncertainOnlyAtTheSendCap(t *testing.T) {
	provider := newIssueProvider(t, answer502WithoutStoring, answer502WithoutStoring, answer502WithoutStoring)
	client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
	ctx := singledispatchtest.NewContext("issues-flow", "send-cap")

	for attempt := 1; attempt <= 2; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
		require.Error(t, err, "attempt %d retries", attempt)
		ctx.AdvanceAttempt()
	}
	result, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Equal(t, github.CreatedIssue{Owner: "octocat", Repository: "hello-world", Title: "Crash when saving an empty draft"}, result.Value)
	require.Equal(t, 3, provider.count("POST"), "the manifest's maxSends caps the sends")

	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch, "a repeated Execute at the cap only reads back")
	require.Equal(t, 3, provider.count("POST"))
}

func TestCreateIssueStepMaxSendsOfOneNeverResends(t *testing.T) {
	provider := newIssueProvider(t, answer502WithoutStoring)
	client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("issues-flow", "one-send"), client.CreateIssue(), githubConnection, bugReportInput(), sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Equal(t, 1, provider.count("POST"))
}

func TestCreateIssueReadsBackTheSavedIssueNumberAfterALostStepResult(t *testing.T) {
	provider := newIssueProvider(t)
	client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
	ctx := singledispatchtest.NewContext("issues-flow", "lost-result")
	first, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueBranchCreated, first.Branch)

	// The Worker dies after the accepted send, so Dex runs Execute again with the persisted heartbeat.
	ctx.AdvanceAttempt()
	second, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueBranchCreated, second.Branch, second.Failure)
	require.Equal(t, first.Value, second.Value)
	require.Equal(t, 1, provider.count("POST"))
	require.Equal(t, 1, provider.count("GET /repos/octocat/hello-world/issues/1"))
	require.Zero(t, provider.count("GET /user"), "a saved issue number needs no marker scan")

	provider.deleteIssue(1)
	ctx.AdvanceAttempt()
	third, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, third.Branch, "an accepted issue that is gone is never created again")
	require.Equal(t, 1, provider.count("POST"))
}

func TestCreateIssueReadsBackAfterAWorkerLossWithoutAnySavedOutcome(t *testing.T) {
	provider := newIssueProvider(t)
	client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
	ctx := singledispatchtest.NewContext("issues-flow", "lost-heartbeats")
	_, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.NoError(t, err)

	// Simulate a lost claim and number: the next attempt sees no heartbeat from the attempt that sent.
	lost := singledispatchtest.NewContext("issues-flow", "lost-heartbeats")
	lost.AdvanceAttempt()
	result, err := sdkgo.RunMutation(lost, client.CreateIssue(), githubConnection, bugReportInput())
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueBranchCreated, result.Branch, result.Failure)
	require.Equal(t, 1, result.Value.Number)
	require.Equal(t, 1, provider.count("POST"), "the marker scan found the issue")
}

func TestCreateIssueRetriesUnprocessedWritesWithoutReadBack(t *testing.T) {
	for _, test := range []struct {
		behavior issueWriteBehavior
		delay    time.Duration
	}{
		{behavior: answer429, delay: 3 * time.Second},
		{behavior: answerSecondaryRateLimit, delay: time.Minute},
		{behavior: answer503},
	} {
		t.Run(string(test.behavior), func(t *testing.T) {
			provider := newIssueProvider(t, test.behavior)
			client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
			ctx := singledispatchtest.NewContext("issues-flow", "unprocessed")
			_, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
			require.Error(t, err)
			var retryAfter *dex.RetryAfterError
			if test.delay > 0 {
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.delay, retryAfter.After)
			}
			require.Equal(t, singledispatch.StateCanResend, lastMarker(t, ctx).State)
			ctx.AdvanceAttempt()
			result, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
			require.NoError(t, err)
			require.Equal(t, github.CreateIssueBranchCreated, result.Branch, result.Failure)
			require.Equal(t, 2, provider.count("POST"))
			require.Zero(t, provider.count("GET"), "a definite refusal needs no read-back")
			require.Equal(t, int32(1), lastMarker(t, ctx).SendCount, "an unprocessed send does not count")
		})
	}
}

func TestCreateIssueRetriesAProvablyUndispatchedRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	baseURL := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	client := newIssueClient(t, baseURL, github.RepositoryAuthMethodID)
	ctx := singledispatchtest.NewContext("issues-flow", "undispatched")
	_, err = sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.Error(t, err)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	require.Equal(t, singledispatch.StateCanResend, lastMarker(t, ctx).State)
	require.NotContains(t, err.Error(), "repository-token")
}

func TestCreateIssueRefreshesARejectedTokenBeforeSendingAgain(t *testing.T) {
	provider := newIssueProvider(t, answer401)
	credentials := &rejectedIssueCredentialProvider{}
	client, err := github.New(github.Config{BaseURL: provider.URL}, credentials)
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("issues-flow", "refresh")
	_, err = sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.Error(t, err)
	require.Equal(t, 1, credentials.forcedRefreshes)
	require.Equal(t, singledispatch.StateCanResend, lastMarker(t, ctx).State)

	provider = newIssueProvider(t, answer401)
	client = newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("issues-flow", "revoked"), client.CreateIssue(), githubConnection, bugReportInput())
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueBranchAuthorizationRevoked, result.Branch)
}

func TestCreateIssueClassifiesConclusiveRejections(t *testing.T) {
	for _, test := range []struct {
		behavior issueWriteBehavior
		branch   sdkgo.BranchID
		kind     sdkgo.FailureKind
	}{
		{behavior: answer403, branch: github.CreateIssueBranchInsufficientScope, kind: sdkgo.FailureAuthorization},
		{behavior: answer404, branch: github.CreateIssueBranchNotFound, kind: sdkgo.FailureNotFound},
		{behavior: answer410, branch: github.CreateIssueBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
		{behavior: answer422, branch: github.CreateIssueBranchProviderRejected, kind: sdkgo.FailureValidation},
	} {
		t.Run(string(test.behavior), func(t *testing.T) {
			provider := newIssueProvider(t, test.behavior)
			client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
			result, err := sdkgo.RunMutation(singledispatchtest.NewContext("issues-flow", "rejected"), client.CreateIssue(), githubConnection, bugReportInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, providerBodySentinel)
			require.Equal(t, 1, provider.count("POST"))
			require.Zero(t, provider.count("GET"))
			requireRedacted(t, result)
		})
	}
}

func TestCreateIssueNeverResendsAnAcceptedIssueWithAnUnusableAnswer(t *testing.T) {
	provider := newIssueProvider(t, storeThenAnswerUnusable201)
	client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("issues-flow", "unusable"), client.CreateIssue(), githubConnection, bugReportInput())
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueBranchCreated, result.Branch, result.Failure)
	require.Equal(t, 1, result.Value.Number)

	provider = newIssueProvider(t, answerUnusable201WithoutStoring)
	client = newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
	result, err = sdkgo.RunMutation(singledispatchtest.NewContext("issues-flow", "unusable-missing"), client.CreateIssue(), githubConnection, bugReportInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch, "an accepted write is never sent again, even below the cap")
	require.Equal(t, 1, provider.count("POST"))
}

func TestCreateIssueRetriesATransientReadBackWithoutSending(t *testing.T) {
	provider := newIssueProvider(t, answer502WithoutStoring)
	provider.failListsWith(http.StatusBadGateway, 2)
	client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
	ctx := singledispatchtest.NewContext("issues-flow", "read-back-retry")
	_, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.Error(t, err)
	ctx.AdvanceAttempt()
	_, err = sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.Error(t, err, "a failed read-back retries before any resend")
	require.Equal(t, 1, provider.count("POST"))
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueBranchCreated, result.Branch, result.Failure)
	require.Equal(t, 2, provider.count("POST"))
}

func TestCreateIssueSelectsUncertainWhenReadBackCannotVerify(t *testing.T) {
	provider := newIssueProvider(t, answer502WithoutStoring)
	client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
	ctx := singledispatchtest.NewContext("issues-flow", "unverifiable")
	_, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.Error(t, err)
	provider.failListsWith(http.StatusNotFound, 1)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Equal(t, 1, provider.count("POST"), "a repository the connection can no longer read is not written")
}

func TestCreateIssueFailsClosedOnAForeignCheckpoint(t *testing.T) {
	provider := newIssueProvider(t)
	client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
	ctx := singledispatchtest.NewContext("issues-flow", "foreign")
	require.NoError(t, ctx.SetPersistedCheckpoint(singledispatch.Marker{CallID: "another-call", Attempt: 1, State: singledispatch.StateNotSent}))
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateIssue(), githubConnection, bugReportInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Zero(t, provider.count("POST")+provider.count("GET"))
}

func TestCreateIssueValidatesInputBeforeProviderAccess(t *testing.T) {
	provider := newIssueProvider(t)
	client := newIssueClient(t, provider.URL, github.RepositoryAuthMethodID)
	for name, mutate := range map[string]func(*github.CreateIssueInput){
		"owner":            func(input *github.CreateIssueInput) { input.Owner = "" },
		"repository":       func(input *github.CreateIssueInput) { input.Repository = "hello world" },
		"title":            func(input *github.CreateIssueInput) { input.Title = "  " },
		"title line break": func(input *github.CreateIssueInput) { input.Title = "Crash\nwhen saving" },
		"long title":       func(input *github.CreateIssueInput) { input.Title = strings.Repeat("é", 257) },
		"long body":        func(input *github.CreateIssueInput) { input.Body = strings.Repeat("b", 65536-90) },
		"nul body":         func(input *github.CreateIssueInput) { input.Body = "a\x00b" },
		"invalid body":     func(input *github.CreateIssueInput) { input.Body = "\xff" },
		"blank label":      func(input *github.CreateIssueInput) { input.Labels = []string{""} },
		"long label":       func(input *github.CreateIssueInput) { input.Labels = []string{strings.Repeat("l", 51)} },
		"too many labels":  func(input *github.CreateIssueInput) { input.Labels = make([]string, 101) },
		"assignee":         func(input *github.CreateIssueInput) { input.Assignees = []string{"mona lisa"} },
		"too many assignees": func(input *github.CreateIssueInput) {
			input.Assignees = []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := bugReportInput()
			mutate(&input)
			result, err := sdkgo.RunMutation(singledispatchtest.NewContext("issues-flow", "invalid-"+name), client.CreateIssue(), githubConnection, input)
			require.NoError(t, err)
			require.Equal(t, github.CreateIssueBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	input := bugReportInput()
	input.Body = strings.Repeat("b", 65536-len("\n\n"+issueMarkerPrefix+strings.Repeat("0", 64)+" -->"))
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("issues-flow", "longest-body"), client.CreateIssue(), githubConnection, input)
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueBranchCreated, result.Branch, "the longest body fits with its marker")
	require.Equal(t, 1, provider.count("POST"))
}

func bugReportInput() github.CreateIssueInput {
	return github.CreateIssueInput{Owner: "octocat", Repository: "hello-world", Title: "Crash when saving an empty draft", Body: "Steps to reproduce."}
}

func newIssueClient(t *testing.T, baseURL string, authMethodID string) *github.Client {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[github.Credentials]{githubConnection: {
		AuthMethodID: authMethodID, AccessToken: sdkgo.NewSecretString("repository-token"),
	}}
	client, err := github.New(github.Config{BaseURL: baseURL}, credentials, github.WithClock(func() time.Time {
		return time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC)
	}))
	require.NoError(t, err)
	return client
}

func lastMarker(t *testing.T, ctx *singledispatchtest.Context) singledispatch.Marker {
	t.Helper()
	heartbeats := ctx.Heartbeats()
	require.NotEmpty(t, heartbeats)
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(heartbeats[len(heartbeats)-1], &marker))
	return marker
}

func timePointer(value time.Time) *time.Time { return &value }

func issueJSON(number int, title string, body string, state string, overrides map[string]any) map[string]any {
	issue := map[string]any{
		"id": 9000 + number, "node_id": "I_node" + strconv.Itoa(number), "number": number, "title": title, "body": body, "state": state,
		"html_url": "https://github.com/octocat/hello-world/issues/" + strconv.Itoa(number),
		"user":     map[string]any{"login": "mona"}, "labels": []any{map[string]any{"name": "bug"}},
		"assignees": []any{map[string]any{"login": "hubot"}}, "comments": 2,
		"created_at": "2026-03-06T10:00:00Z", "updated_at": "2026-03-06T11:00:00Z",
	}
	for key, value := range overrides {
		issue[key] = value
	}
	return issue
}

type rejectedIssueCredentialProvider struct{ forcedRefreshes int }

func (*rejectedIssueCredentialProvider) Resolve(sdkgo.Call) (github.Credentials, error) {
	return github.Credentials{AuthMethodID: github.RepositoryAuthMethodID, AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

func (provider *rejectedIssueCredentialProvider) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[github.Credentials]) (github.Credentials, error) {
	return provider.Resolve(call)
}

func (provider *rejectedIssueCredentialProvider) ResolveAfterRejection(context.Context, sdkgo.Call, sdkgo.CredentialRefreshDriver[github.Credentials]) (github.Credentials, error) {
	provider.forcedRefreshes++
	return github.Credentials{AuthMethodID: github.RepositoryAuthMethodID, AccessToken: sdkgo.NewSecretString("replacement-token")}, nil
}

// issueWriteBehavior scripts one POST answer of the fake issue API.
type issueWriteBehavior string

const (
	storeAndAnswer201               issueWriteBehavior = "store and answer 201"
	storeThenAnswer502              issueWriteBehavior = "store then answer 502"
	storeThenCloseConnection        issueWriteBehavior = "store then close the connection"
	storeThenAnswerUnusable201      issueWriteBehavior = "store then answer an unusable 201"
	answerUnusable201WithoutStoring issueWriteBehavior = "answer an unusable 201 without storing"
	answer502WithoutStoring         issueWriteBehavior = "answer 502 without storing"
	answer429                       issueWriteBehavior = "answer 429"
	answerSecondaryRateLimit        issueWriteBehavior = "answer a secondary rate limit"
	answer503                       issueWriteBehavior = "answer 503"
	answer401                       issueWriteBehavior = "answer 401"
	answer403                       issueWriteBehavior = "answer 403"
	answer404                       issueWriteBehavior = "answer 404"
	answer410                       issueWriteBehavior = "answer 410"
	answer422                       issueWriteBehavior = "answer 422"
)

type sentIssue struct {
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Labels    []string `json:"labels"`
	Assignees []string `json:"assignees"`
}

// issueProvider is a local fake of GitHub's issue create, get, and list endpoints and the user endpoint.
type issueProvider struct {
	*httptest.Server
	t              *testing.T
	mutex          sync.Mutex
	behaviors      []issueWriteBehavior
	sent           []sentIssue
	issues         map[int]map[string]any
	nextNumber     int
	requests       map[string]int
	lists          []string
	listFailure    int
	listFailuresTo int
}

func newIssueProvider(t *testing.T, behaviors ...issueWriteBehavior) *issueProvider {
	t.Helper()
	provider := &issueProvider{t: t, behaviors: behaviors, issues: map[int]map[string]any{}, nextNumber: 1, requests: map[string]int{}}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *issueProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	t := provider.t
	if !strings.HasSuffix(request.Header.Get("Authorization"), "-token") || request.Header.Get("Accept") != "application/vnd.github+json" {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	response.Header().Set("X-OAuth-Scopes", "repo")
	response.Header().Set("X-GitHub-Request-Id", "request-1")
	provider.mutex.Lock()
	provider.requests[request.Method]++
	provider.requests[request.Method+" "+request.URL.Path]++
	provider.mutex.Unlock()
	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/repos/octocat/hello-world/issues":
		provider.serveCreate(response, request)
	case request.Method == http.MethodGet && request.URL.Path == "/user":
		writeJSON(t, response, map[string]any{"login": "octocat", "id": 1})
	case request.Method == http.MethodGet && request.URL.Path == "/repos/octocat/hello-world/issues":
		provider.serveList(response, request)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/repos/octocat/hello-world/issues/"):
		number, _ := strconv.Atoi(strings.TrimPrefix(request.URL.Path, "/repos/octocat/hello-world/issues/"))
		provider.mutex.Lock()
		issue, found := provider.issues[number]
		provider.mutex.Unlock()
		if !found {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(t, response, issue)
	default:
		response.WriteHeader(http.StatusNotFound)
	}
}

func (provider *issueProvider) serveCreate(response http.ResponseWriter, request *http.Request) {
	t := provider.t
	require.Equal(t, "application/json", request.Header.Get("Content-Type"))
	require.Nil(t, request.GetBody)
	var sent sentIssue
	require.NoError(t, json.NewDecoder(request.Body).Decode(&sent))
	provider.mutex.Lock()
	provider.sent = append(provider.sent, sent)
	behavior := storeAndAnswer201
	if len(provider.behaviors) > 0 {
		behavior, provider.behaviors = provider.behaviors[0], provider.behaviors[1:]
	}
	var stored map[string]any
	if strings.HasPrefix(string(behavior), "store") {
		stored = provider.storeLocked(sent)
	}
	provider.mutex.Unlock()
	switch behavior {
	case storeAndAnswer201:
		response.WriteHeader(http.StatusCreated)
		writeJSON(t, response, stored)
	case storeThenAnswerUnusable201, answerUnusable201WithoutStoring:
		response.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(response, `{"message":"`+providerBodySentinel+`"}`)
	case storeThenAnswer502, answer502WithoutStoring:
		response.WriteHeader(http.StatusBadGateway)
	case storeThenCloseConnection:
		connection, _, err := http.NewResponseController(response).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	case answer429:
		response.Header().Set("Retry-After", "3")
		response.WriteHeader(http.StatusTooManyRequests)
	case answerSecondaryRateLimit:
		response.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(response, `{"message":"You have exceeded a secondary rate limit and have been temporarily blocked from content creation."}`)
	case answer503:
		response.WriteHeader(http.StatusServiceUnavailable)
	case answer401:
		response.WriteHeader(http.StatusUnauthorized)
	case answer403:
		response.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(response, `{"message":"`+providerBodySentinel+`"}`)
	case answer404:
		response.WriteHeader(http.StatusNotFound)
	case answer410:
		response.WriteHeader(http.StatusGone)
		_, _ = io.WriteString(response, `{"message":"`+providerBodySentinel+`"}`)
	case answer422:
		response.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(response, `{"message":"Validation Failed","errors":[{"value":"`+providerBodySentinel+`"}]}`)
	}
}

func (provider *issueProvider) serveList(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	provider.mutex.Lock()
	provider.lists = append(provider.lists, request.URL.RawQuery)
	if provider.listFailuresTo > 0 {
		provider.listFailuresTo--
		status := provider.listFailure
		provider.mutex.Unlock()
		response.WriteHeader(status)
		return
	}
	numbers := make([]int, 0, len(provider.issues))
	for number, issue := range provider.issues {
		if user, _ := issue["user"].(map[string]any); query.Get("creator") == "" || user["login"] == query.Get("creator") {
			numbers = append(numbers, number)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(numbers)))
	issues := make([]map[string]any, 0, len(numbers))
	for _, number := range numbers {
		issues = append(issues, provider.issues[number])
	}
	provider.mutex.Unlock()
	writeJSON(provider.t, response, issues)
}

func (provider *issueProvider) storeLocked(sent sentIssue) map[string]any {
	number := provider.nextNumber
	provider.nextNumber++
	labels := make([]any, 0, len(sent.Labels))
	for _, label := range sent.Labels {
		labels = append(labels, map[string]any{"name": label})
	}
	assignees := make([]any, 0, len(sent.Assignees))
	for _, assignee := range sent.Assignees {
		assignees = append(assignees, map[string]any{"login": assignee})
	}
	issue := map[string]any{
		"id": 9000 + number, "node_id": "I_node" + strconv.Itoa(number), "number": number, "title": sent.Title, "body": sent.Body,
		"state": "open", "html_url": "https://github.com/octocat/hello-world/issues/" + strconv.Itoa(number),
		"user": map[string]any{"login": "octocat"}, "labels": labels, "assignees": assignees, "comments": 0,
		"created_at": "2026-03-07T10:15:00Z", "updated_at": "2026-03-07T10:15:00Z",
	}
	provider.issues[number] = issue
	return issue
}

// addIssueByAnotherAccount stores an issue that carries no marker of this test's calls.
func (provider *issueProvider) addIssueByAnotherAccount() {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	issue := provider.storeLocked(sentIssue{Title: "Unrelated", Body: issueMarkerPrefix + strings.Repeat("f", 64) + " -->"})
	issue["user"] = map[string]any{"login": "hubot"}
}

func (provider *issueProvider) failListsWith(status int, count int) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.listFailure, provider.listFailuresTo = status, count
}

func (provider *issueProvider) deleteIssue(number int) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	delete(provider.issues, number)
}

func (provider *issueProvider) count(key string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[key]
}

func (provider *issueProvider) sentIssues() []sentIssue {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]sentIssue(nil), provider.sent...)
}

func (provider *issueProvider) storedIssues() map[int]map[string]any {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	stored := make(map[int]map[string]any, len(provider.issues))
	for number, issue := range provider.issues {
		stored[number] = issue
	}
	return stored
}

func (provider *issueProvider) listQueries() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.lists...)
}
