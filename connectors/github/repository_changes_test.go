// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package githubconnector_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	githubconnector "github.com/superdurable/dex-connectors-library/connectors/github"
	"github.com/superdurable/dex-connectors-library/connectors/github/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const providerBodySentinel = "SENTINEL-PROVIDER-BODY"

var (
	windowStart = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	windowEnd   = time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
)

func TestListMergedPullRequestsSearchesTheWindowAndBoundsBodies(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/search/issues", request.URL.Path)
		requireGitHubHeaders(t, request)
		query := request.URL.Query()
		require.Equal(t, "repo:octocat/hello-world is:pr is:merged merged:2026-09-01T00:00:00Z..2026-09-08T00:00:00Z", query.Get("q"))
		require.Equal(t, "created", query.Get("sort"))
		require.Equal(t, "desc", query.Get("order"))
		require.Equal(t, "2", query.Get("per_page"))
		require.Equal(t, "3", query.Get("page"))
		setGrantedScopes(response)
		response.Header().Set("X-GitHub-Request-Id", "search-request-1")
		response.Header().Set("Link", `<https://api.github.com/search/issues?q=x&page=2>; rel="prev", `+
			`<https://api.github.com/search/issues?q=x&per_page=2&page=4>; rel="next", <https://api.github.com/search/issues?q=x&page=9>; rel="last"`)
		writeJSON(t, response, map[string]any{
			"total_count": 17, "incomplete_results": true,
			"items": []map[string]any{
				mergedPullRequestJSON(42, "2026-09-07T10:00:00Z", "héllo wörld and more", []string{"enhancement", " ", "docs"}),
				mergedPullRequestJSON(41, "2026-09-02T09:30:00-07:00", "short", nil),
			},
		})
	}))
	defer server.Close()

	pacific := time.FixedZone("PDT", -7*60*60)
	client := newClient(t, server.URL, githubconnector.Config{MaxPullRequestBodyCharacters: 7})
	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("changes-flow", "merged-step"), client.ListMergedPullRequests(), githubConnection,
		githubconnector.ListMergedPullRequestsInput{
			Owner: " octocat ", Repository: "hello-world",
			MergedAfter:  time.Date(2026, 8, 31, 17, 0, 0, 999, pacific),
			MergedBefore: windowEnd.Add(900 * time.Millisecond), PageSize: 2, Page: 3,
		},
	)
	require.NoError(t, err)
	require.Equal(t, githubconnector.ListMergedPullRequestsBranchListed, result.Branch)
	require.Nil(t, result.Failure)
	require.Equal(t, 17, result.Value.TotalCount)
	require.True(t, result.Value.IncompleteResults)
	require.Equal(t, 4, result.Value.NextPage)
	require.Equal(t, "search-request-1", result.Receipt.ProviderRequestID)
	require.Len(t, result.Value.PullRequests, 2)
	first := result.Value.PullRequests[0]
	require.Equal(t, 42, first.Number)
	require.Equal(t, "Pull request 42", first.Title)
	require.Equal(t, "héllo w", first.Body)
	require.True(t, first.BodyTruncated)
	require.Equal(t, "https://github.com/octocat/hello-world/pull/42", first.URL)
	require.Equal(t, "mona", first.AuthorLogin)
	require.Equal(t, time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC), first.MergedAt)
	require.Equal(t, []string{"enhancement", "docs"}, first.Labels)
	second := result.Value.PullRequests[1]
	require.Equal(t, "short", second.Body)
	require.False(t, second.BodyTruncated)
	require.Equal(t, time.Date(2026, 9, 2, 16, 30, 0, 0, time.UTC), second.MergedAt)
	require.Empty(t, second.Labels)
	require.Equal(t, int32(1), requests.Load())
	requireRedacted(t, result)
}

func TestListMergedPullRequestsUsesDefaultPageAndReportsTheLastPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "30", request.URL.Query().Get("per_page"))
		require.Equal(t, "1", request.URL.Query().Get("page"))
		setGrantedScopes(response)
		response.Header().Set("Link", `<https://api.github.com/search/issues?page=1>; rel="first"`)
		writeJSON(t, response, map[string]any{"total_count": 0, "incomplete_results": false, "items": []any{}})
	}))
	defer server.Close()

	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("changes-flow", "merged-default-step"), newClient(t, server.URL, githubconnector.Config{}).ListMergedPullRequests(),
		githubConnection, mergedPullRequestsInput(),
	)
	require.NoError(t, err)
	require.Equal(t, githubconnector.ListMergedPullRequestsBranchListed, result.Branch)
	require.Empty(t, result.Value.PullRequests)
	require.NotNil(t, result.Value.PullRequests)
	require.Zero(t, result.Value.NextPage)
}

func TestListMergedPullRequestsRejectsPagesBeyondTheSearchResultLimit(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		setGrantedScopes(response)
		writeJSON(t, response, map[string]any{"total_count": 5000, "incomplete_results": false, "items": []any{}})
	}))
	defer server.Close()
	client := newClient(t, server.URL, githubconnector.Config{})

	for _, test := range []struct {
		pageSize, page int
		isAllowed      bool
	}{
		{pageSize: 100, page: 10, isAllowed: true}, {pageSize: 100, page: 11},
		{pageSize: 30, page: 34, isAllowed: true}, {pageSize: 30, page: 35},
		{pageSize: 1, page: 1000, isAllowed: true}, {pageSize: 1, page: 1001},
	} {
		input := mergedPullRequestsInput()
		input.PageSize, input.Page = test.pageSize, test.page
		result, err := sdkgo.RunQuery(
			testsupport.NewDexContext("changes-flow", "merged-cap-step"), client.ListMergedPullRequests(), githubConnection, input,
		)
		require.NoError(t, err)
		if test.isAllowed {
			require.Equal(t, githubconnector.ListMergedPullRequestsBranchListed, result.Branch, "%+v", test)
			continue
		}
		require.Equal(t, githubconnector.ListMergedPullRequestsBranchDefect, result.Branch, "%+v", test)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	}
	require.Equal(t, int32(3), requests.Load())
}

func TestListPullRequestFilesPaginatesAndBoundsPatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/repos/octocat/hello-world/pulls/42/files", request.URL.Path)
		requireGitHubHeaders(t, request)
		require.Equal(t, "50", request.URL.Query().Get("per_page"))
		require.Equal(t, "1", request.URL.Query().Get("page"))
		setGrantedScopes(response)
		response.Header().Set("X-GitHub-Request-Id", "files-request-1")
		response.Header().Set("Link", `<https://api.github.com/repositories/1/pulls/42/files?page=2>; rel="next", <https://api.github.com/repositories/1/pulls/42/files?page=3>; rel="last"`)
		writeJSON(t, response, []map[string]any{
			{"sha": "abc", "filename": "docs/renamed.md", "previous_filename": "docs/original.md", "status": "renamed", "additions": 3, "deletions": 1, "changes": 4, "patch": "@@ -1 +1 @@\n-añejo\n+nuevo"},
			{"filename": "assets/logo.png", "status": "added", "additions": 0, "deletions": 0, "changes": 0},
		})
	}))
	defer server.Close()

	client := newClient(t, server.URL, githubconnector.Config{MaxPatchCharacters: 16})
	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("changes-flow", "files-step"), client.ListPullRequestFiles(), githubConnection,
		githubconnector.ListPullRequestFilesInput{Owner: "octocat", Repository: "hello-world", Number: 42},
	)
	require.NoError(t, err)
	require.Equal(t, githubconnector.ListPullRequestFilesBranchListed, result.Branch)
	require.Equal(t, 2, result.Value.NextPage)
	require.Equal(t, "files-request-1", result.Receipt.ProviderRequestID)
	require.Equal(t, []githubconnector.PullRequestFile{
		{
			Filename: "docs/renamed.md", PreviousFilename: "docs/original.md", Status: "renamed",
			Additions: 3, Deletions: 1, Changes: 4, Patch: "@@ -1 +1 @@\n-añe", PatchTruncated: true,
		},
		{Filename: "assets/logo.png", Status: "added"},
	}, result.Value.Files)
	requireRedacted(t, result)
}

func TestListCommitsFiltersWindowPathAndRefAndBoundsMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/repos/octocat/hello-world/commits", request.URL.Path)
		requireGitHubHeaders(t, request)
		query := request.URL.Query()
		require.Equal(t, "2026-09-01T00:00:00Z", query.Get("since"))
		require.Equal(t, "2026-09-08T00:00:00Z", query.Get("until"))
		require.Equal(t, "docs/getting started.md", query.Get("path"))
		require.Equal(t, "release/v1", query.Get("sha"))
		require.Equal(t, "30", query.Get("per_page"))
		require.Equal(t, "2", query.Get("page"))
		setGrantedScopes(response)
		response.Header().Set("Link", `<https://api.github.com/repositories/1/commits?page=1>; rel="prev"`)
		writeJSON(t, response, []map[string]any{
			commitJSON(strings.Repeat("a", 40), "mona", "Add the feature\n\nLong body ✓ with detail"),
			commitJSON(strings.Repeat("b", 64), "", "Fix"),
		})
	}))
	defer server.Close()

	client := newClient(t, server.URL, githubconnector.Config{MaxCommitMessageCharacters: 26})
	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("changes-flow", "commits-step"), client.ListCommits(), githubConnection,
		githubconnector.ListCommitsInput{
			Owner: "octocat", Repository: "hello-world", Since: windowStart, Until: windowEnd,
			Path: "docs/getting started.md", Ref: "release/v1", Page: 2,
		},
	)
	require.NoError(t, err)
	require.Equal(t, githubconnector.ListCommitsBranchListed, result.Branch)
	require.Zero(t, result.Value.NextPage)
	require.Len(t, result.Value.Commits, 2)
	first := result.Value.Commits[0]
	require.Equal(t, strings.Repeat("a", 40), first.SHA)
	require.Equal(t, "Add the feature\n\nLong body", first.Message)
	require.True(t, first.MessageTruncated)
	require.Equal(t, "mona", first.AuthorLogin)
	require.Equal(t, "Mona Lisa", first.AuthorName)
	require.Equal(t, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC), first.AuthoredAt)
	require.Equal(t, time.Date(2026, 9, 3, 11, 0, 0, 0, time.UTC), first.CommittedAt)
	require.Equal(t, "https://github.com/octocat/hello-world/commit/"+strings.Repeat("a", 40), first.URL)
	second := result.Value.Commits[1]
	require.Empty(t, second.AuthorLogin)
	require.Equal(t, "Fix", second.Message)
	require.False(t, second.MessageTruncated)
	requireRedacted(t, result)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "mona@example.com")
}

func TestListCommitsTreatsAnEmptyRepositoryAsNoCommits(t *testing.T) {
	scopes := "read:user, user:email"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/repos/octocat/empty/commits", request.URL.Path)
		response.Header().Set("X-OAuth-Scopes", scopes)
		response.Header().Set("X-GitHub-Request-Id", "empty-request-1")
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusConflict)
		_, _ = response.Write([]byte(`{"message":"Git Repository is empty. ` + providerBodySentinel + `"}`))
	}))
	defer server.Close()
	client := newClient(t, server.URL, githubconnector.Config{})
	input := githubconnector.ListCommitsInput{Owner: "octocat", Repository: "empty", Since: windowStart, Until: windowEnd}

	result, err := sdkgo.RunQuery(testsupport.NewDexContext("changes-flow", "empty-step"), client.ListCommits(), githubConnection, input)
	require.NoError(t, err)
	require.Equal(t, githubconnector.ListCommitsBranchListed, result.Branch)
	require.Nil(t, result.Failure)
	require.NotNil(t, result.Value.Commits)
	require.Empty(t, result.Value.Commits)
	require.Zero(t, result.Value.NextPage)
	require.Equal(t, map[string]string{"repositoryEmpty": "true"}, result.Receipt.Metadata)
	require.Equal(t, "empty-request-1", result.Receipt.ProviderRequestID)
	requireRedacted(t, result)

	scopes = "read:user, user:email, repo"
	result, err = sdkgo.RunQuery(testsupport.NewDexContext("changes-flow", "empty-scope-step"), client.ListCommits(), githubConnection, input)
	require.NoError(t, err)
	require.Equal(t, githubconnector.ListCommitsBranchInsufficientScope, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
}

func TestRepositoryChangeQueriesClassifyConclusiveProviderResponses(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		scopes     string
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
	}{
		{name: "revoked", status: http.StatusUnauthorized, wantBranch: "authorizationRevoked", wantKind: sdkgo.FailureAuthentication},
		{name: "forbidden", status: http.StatusForbidden, wantBranch: "insufficientScope", wantKind: sdkgo.FailureAuthorization},
		{name: "not found", status: http.StatusNotFound, wantBranch: "notFound", wantKind: sdkgo.FailureNotFound},
		{name: "unsearchable repository", status: http.StatusUnprocessableEntity, wantBranch: "providerRejected", wantKind: sdkgo.FailureProviderRejection},
		{name: "renamed repository redirect", status: http.StatusMovedPermanently, wantBranch: "providerRejected", wantKind: sdkgo.FailureProviderRejection},
		{name: "broader grant", status: http.StatusOK, scopes: "read:user, user:email, repo", wantBranch: "insufficientScope", wantKind: sdkgo.FailureAuthorization},
		{name: "missing scopes header", status: http.StatusOK, scopes: "-", wantBranch: "insufficientScope", wantKind: sdkgo.FailureAuthorization},
	}
	for _, query := range repositoryChangeQueries() {
		for _, test := range tests {
			t.Run(query.name+"/"+test.name, func(t *testing.T) {
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
					requests.Add(1)
					switch test.scopes {
					case "":
						setGrantedScopes(response)
					case "-":
					default:
						response.Header().Set("X-OAuth-Scopes", test.scopes)
					}
					if test.status == http.StatusMovedPermanently {
						response.Header().Set("Location", "https://api.github.com/repositories/1")
					}
					if test.status == http.StatusOK {
						query.writeSuccess(t, response)
						return
					}
					response.WriteHeader(test.status)
					_, _ = response.Write([]byte(`{"message":"` + providerBodySentinel + `"}`))
				}))
				defer server.Close()
				outcome := query.invoke(newClient(t, server.URL, githubconnector.Config{}), "classification-step")
				require.NoError(t, outcome.err)
				require.Equal(t, test.wantBranch, outcome.branch)
				require.Equal(t, test.wantKind, outcome.failure.Kind)
				require.NotContains(t, outcome.failure.Message, providerBodySentinel)
				require.NotContains(t, outcome.encoded, providerBodySentinel)
				require.NotContains(t, outcome.encoded, "one-use-token")
				require.Equal(t, int32(1), requests.Load())
			})
		}
	}
}

func TestRepositoryChangeQueriesRetryRateLimitsAndUnavailabilityWithProviderDelay(t *testing.T) {
	// GitHub sends X-RateLimit-Reset on every response. The clock below is 30 seconds before
	// 1767225635 and one hour before 1767229205.
	secondaryLimitBody := `{"message":"You have exceeded a secondary rate limit. ` + providerBodySentinel + `"}`
	tests := []struct {
		name      string
		status    int
		headers   map[string]string
		body      string
		wantKind  sdkgo.FailureKind
		wantDelay time.Duration
	}{
		{name: "primary rate limit", status: http.StatusForbidden, headers: map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1767225635"}, wantKind: sdkgo.FailureRateLimit, wantDelay: 30 * time.Second},
		{name: "secondary rate limit", status: http.StatusForbidden, headers: map[string]string{"Retry-After": "9"}, wantKind: sdkgo.FailureRateLimit, wantDelay: 9 * time.Second},
		{
			name: "secondary rate limit without retry-after", status: http.StatusForbidden,
			headers: map[string]string{"X-RateLimit-Remaining": "4987", "X-RateLimit-Reset": "1767229205"}, body: secondaryLimitBody,
			wantKind: sdkgo.FailureRateLimit, wantDelay: time.Minute,
		},
		{name: "secondary rate limit without rate-limit headers", status: http.StatusForbidden, body: secondaryLimitBody, wantKind: sdkgo.FailureRateLimit, wantDelay: time.Minute},
		{name: "primary rate limit message without headers", status: http.StatusForbidden, body: `{"message":"API Rate Limit exceeded for user ID 1."}`, wantKind: sdkgo.FailureRateLimit, wantDelay: time.Minute},
		{name: "too many requests", status: http.StatusTooManyRequests, headers: map[string]string{"Retry-After": "7"}, wantKind: sdkgo.FailureRateLimit, wantDelay: 7 * time.Second},
		{
			name: "too many requests before the primary reset", status: http.StatusTooManyRequests,
			headers: map[string]string{"X-RateLimit-Remaining": "4987", "X-RateLimit-Reset": "1767229205"}, wantKind: sdkgo.FailureRateLimit, wantDelay: time.Minute,
		},
		{name: "rate limit without delay", status: http.StatusTooManyRequests, wantKind: sdkgo.FailureRateLimit, wantDelay: time.Minute},
		{name: "unavailable", status: http.StatusBadGateway, wantKind: sdkgo.FailureAvailability},
	}
	for _, query := range repositoryChangeQueries() {
		for _, test := range tests {
			t.Run(query.name+"/"+test.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
					for name, value := range test.headers {
						response.Header().Set(name, value)
					}
					response.WriteHeader(test.status)
					body := test.body
					if body == "" {
						body = providerBodySentinel
					}
					_, _ = response.Write([]byte(body))
				}))
				defer server.Close()
				client := newClient(t, server.URL, githubconnector.Config{}, githubconnector.WithClock(func() time.Time {
					return time.Unix(1767225605, 0)
				}))
				outcome := query.invoke(client, "retry-step")
				var retry *sdkgo.RetryError
				require.ErrorAs(t, outcome.err, &retry)
				require.Equal(t, test.wantKind, retry.Failure.Kind)
				require.NotContains(t, retry.Failure.Message, providerBodySentinel)
				require.NotContains(t, outcome.err.Error(), providerBodySentinel)
				var retryAfter *dex.RetryAfterError
				if test.wantDelay == 0 {
					require.False(t, errors.As(outcome.err, &retryAfter))
					return
				}
				require.ErrorAs(t, outcome.err, &retryAfter)
				require.Equal(t, test.wantDelay, retryAfter.After)
			})
		}
	}
}

func TestRepositoryChangeQueriesRejectMalformedResponses(t *testing.T) {
	for _, query := range repositoryChangeQueries() {
		for name, body := range query.malformedBodies {
			t.Run(query.name+"/"+name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
					setGrantedScopes(response)
					if name == "backward link" {
						response.Header().Set("Link", `<https://api.github.com/next?page=1>; rel="next"`)
					}
					if name == "unnumbered link" {
						response.Header().Set("Link", `<https://api.github.com/next?cursor=abc>; rel="next"`)
					}
					response.Header().Set("Content-Type", "application/json")
					_, _ = response.Write([]byte(body))
				}))
				defer server.Close()
				outcome := query.invoke(newClient(t, server.URL, githubconnector.Config{}), "malformed-step")
				require.NoError(t, outcome.err)
				require.Equal(t, sdkgo.BranchID("invalidResponse"), outcome.branch)
				require.Equal(t, sdkgo.FailureProtocol, outcome.failure.Kind)
				require.NotContains(t, outcome.encoded, providerBodySentinel)
			})
		}
		t.Run(query.name+"/oversized", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				setGrantedScopes(response)
				_, _ = response.Write([]byte(strings.Repeat("x", 128)))
			}))
			defer server.Close()
			outcome := query.invoke(newClient(t, server.URL, githubconnector.Config{MaxResponseBytes: 64}), "oversized-step")
			require.NoError(t, outcome.err)
			require.Equal(t, sdkgo.BranchID("invalidResponse"), outcome.branch)
			require.Equal(t, sdkgo.FailureResponseTooLarge, outcome.failure.Kind)
		})
	}
}

func TestRepositoryChangeQueriesValidateInputBeforeProviderAccess(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	client := newClient(t, server.URL, githubconnector.Config{})

	mergedInputs := map[string]func(*githubconnector.ListMergedPullRequestsInput){
		"blank owner":         func(input *githubconnector.ListMergedPullRequestsInput) { input.Owner = "" },
		"owner with slash":    func(input *githubconnector.ListMergedPullRequestsInput) { input.Owner = "octo/cat" },
		"qualifier injection": func(input *githubconnector.ListMergedPullRequestsInput) { input.Repository = "hello is:private" },
		"dot repository":      func(input *githubconnector.ListMergedPullRequestsInput) { input.Repository = ".." },
		"missing start":       func(input *githubconnector.ListMergedPullRequestsInput) { input.MergedAfter = time.Time{} },
		"missing end":         func(input *githubconnector.ListMergedPullRequestsInput) { input.MergedBefore = time.Time{} },
		"reversed window":     func(input *githubconnector.ListMergedPullRequestsInput) { input.MergedAfter = windowEnd },
		"oversized page":      func(input *githubconnector.ListMergedPullRequestsInput) { input.PageSize = 101 },
		"negative page":       func(input *githubconnector.ListMergedPullRequestsInput) { input.Page = -1 },
	}
	for name, mutate := range mergedInputs {
		input := mergedPullRequestsInput()
		mutate(&input)
		result, err := sdkgo.RunQuery(testsupport.NewDexContext("changes-flow", "invalid-merged-step"), client.ListMergedPullRequests(), githubConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, githubconnector.ListMergedPullRequestsBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}

	fileInputs := map[string]githubconnector.ListPullRequestFilesInput{
		"missing number":  {Owner: "octocat", Repository: "hello-world"},
		"negative number": {Owner: "octocat", Repository: "hello-world", Number: -4},
		"invalid owner":   {Owner: "-octocat", Repository: "hello-world", Number: 1},
		"oversized page":  {Owner: "octocat", Repository: "hello-world", Number: 1, PageSize: 500},
	}
	for name, input := range fileInputs {
		result, err := sdkgo.RunQuery(testsupport.NewDexContext("changes-flow", "invalid-files-step"), client.ListPullRequestFiles(), githubConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, githubconnector.ListPullRequestFilesBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}

	commitInputs := map[string]func(*githubconnector.ListCommitsInput){
		"invalid repository": func(input *githubconnector.ListCommitsInput) { input.Repository = "hello/world" },
		"reversed window":    func(input *githubconnector.ListCommitsInput) { input.Since, input.Until = windowEnd, windowStart },
		"empty window":       func(input *githubconnector.ListCommitsInput) { input.Until = input.Since },
		"control path":       func(input *githubconnector.ListCommitsInput) { input.Path = "docs/\nREADME.md" },
		"long path":          func(input *githubconnector.ListCommitsInput) { input.Path = strings.Repeat("p", 4097) },
		"ref with space":     func(input *githubconnector.ListCommitsInput) { input.Ref = "release v1" },
		"long ref":           func(input *githubconnector.ListCommitsInput) { input.Ref = strings.Repeat("r", 256) },
		"invalid page size":  func(input *githubconnector.ListCommitsInput) { input.PageSize = -1 },
	}
	for name, mutate := range commitInputs {
		input := githubconnector.ListCommitsInput{Owner: "octocat", Repository: "hello-world", Since: windowStart, Until: windowEnd}
		mutate(&input)
		result, err := sdkgo.RunQuery(testsupport.NewDexContext("changes-flow", "invalid-commits-step"), client.ListCommits(), githubConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, githubconnector.ListCommitsBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
	require.Zero(t, requests.Load())
}

func TestRepositoryChangeQueriesRetryTransportFailuresWithoutLeakingCredentials(t *testing.T) {
	transportClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "Bearer one-use-token", request.Header.Get("Authorization"))
		return nil, fmt.Errorf("transport failed with one-use-token")
	})}
	client := newClient(t, "https://api.github.test", githubconnector.Config{}, githubconnector.WithHTTPClient(transportClient))
	for _, query := range repositoryChangeQueries() {
		outcome := query.invoke(client, "transport-step")
		var retry *sdkgo.RetryError
		require.ErrorAs(t, outcome.err, &retry, query.name)
		require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind, query.name)
		require.NotContains(t, outcome.err.Error(), "one-use-token", query.name)
	}
}

func TestRepositoryChangeBoundsDefaultFromConfiguration(t *testing.T) {
	defaults := githubconnector.DefaultConfig()
	require.Equal(t, int64(4000), defaults.MaxPullRequestBodyCharacters)
	require.Equal(t, int64(4000), defaults.MaxPatchCharacters)
	require.Equal(t, int64(4000), defaults.MaxCommitMessageCharacters)
	credentials := sdkgo.StaticCredentialProvider[githubconnector.Credentials]{githubConnection: {
		AccessToken: sdkgo.NewSecretString("one-use-token"),
	}}
	_, err := githubconnector.New(githubconnector.Config{MaxPatchCharacters: -1}, credentials)
	require.ErrorContains(t, err, "maxPatchCharacters cannot be negative")

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		setGrantedScopes(response)
		writeJSON(t, response, []map[string]any{commitJSON(strings.Repeat("c", 40), "mona", strings.Repeat("é", 4001))})
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("changes-flow", "default-bounds-step"), newClient(t, server.URL, githubconnector.Config{}).ListCommits(),
		githubConnection, githubconnector.ListCommitsInput{Owner: "octocat", Repository: "hello-world", Since: windowStart, Until: windowEnd},
	)
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("é", 4000), result.Value.Commits[0].Message)
	require.True(t, result.Value.Commits[0].MessageTruncated)
}

type changeQuery struct {
	name            string
	invoke          func(client *githubconnector.Client, stepExecutionID string) changeQueryOutcome
	writeSuccess    func(t *testing.T, response http.ResponseWriter)
	malformedBodies map[string]string
}

type changeQueryOutcome struct {
	branch  sdkgo.BranchID
	failure *sdkgo.Failure
	encoded string
	err     error
}

func repositoryChangeQueries() []changeQuery {
	return []changeQuery{
		{
			name: "listMergedPullRequests",
			invoke: func(client *githubconnector.Client, stepExecutionID string) changeQueryOutcome {
				result, err := sdkgo.RunQuery(testsupport.NewDexContext("changes-flow", stepExecutionID), client.ListMergedPullRequests(), githubConnection, mergedPullRequestsInput())
				return newChangeQueryOutcome(result.Branch, result.Failure, result, err)
			},
			writeSuccess: func(t *testing.T, response http.ResponseWriter) {
				writeJSON(t, response, map[string]any{"total_count": 1, "incomplete_results": false, "items": []any{mergedPullRequestJSON(1, "2026-09-02T00:00:00Z", "body", nil)}})
			},
			malformedBodies: map[string]string{
				"not json":         providerBodySentinel,
				"trailing json":    `{"total_count":0,"incomplete_results":false,"items":[]} {}`,
				"missing items":    `{"total_count":0,"incomplete_results":false}`,
				"missing count":    `{"incomplete_results":false,"items":[]}`,
				"issue not pr":     `{"total_count":1,"incomplete_results":false,"items":[{"number":1,"title":"` + providerBodySentinel + `"}]}`,
				"unmerged pr":      `{"total_count":1,"incomplete_results":false,"items":[{"number":1,"pull_request":{"merged_at":null}}]}`,
				"invalid number":   `{"total_count":1,"incomplete_results":false,"items":[{"number":0,"pull_request":{"merged_at":"2026-09-02T00:00:00Z"}}]}`,
				"backward link":    `{"total_count":0,"incomplete_results":false,"items":[]}`,
				"unnumbered link":  `{"total_count":0,"incomplete_results":false,"items":[]}`,
				"invalid time":     `{"total_count":1,"incomplete_results":false,"items":[{"number":1,"pull_request":{"merged_at":"yesterday"}}]}`,
				"wrong item shape": `{"total_count":1,"incomplete_results":false,"items":["` + providerBodySentinel + `"]}`,
			},
		},
		{
			name: "listPullRequestFiles",
			invoke: func(client *githubconnector.Client, stepExecutionID string) changeQueryOutcome {
				result, err := sdkgo.RunQuery(testsupport.NewDexContext("changes-flow", stepExecutionID), client.ListPullRequestFiles(), githubConnection,
					githubconnector.ListPullRequestFilesInput{Owner: "octocat", Repository: "hello-world", Number: 42})
				return newChangeQueryOutcome(result.Branch, result.Failure, result, err)
			},
			writeSuccess: func(t *testing.T, response http.ResponseWriter) {
				writeJSON(t, response, []map[string]any{{"filename": "README.md", "status": "modified", "additions": 1, "deletions": 0, "changes": 1}})
			},
			malformedBodies: map[string]string{
				"not json":        providerBodySentinel,
				"null":            `null`,
				"object":          `{"message":"` + providerBodySentinel + `"}`,
				"missing name":    `[{"status":"added"}]`,
				"missing status":  `[{"filename":"README.md"}]`,
				"nul filename":    `[{"filename":"READ\u0000ME.md","status":"added"}]`,
				"backward link":   `[]`,
				"unnumbered link": `[]`,
			},
		},
		{
			name: "listCommits",
			invoke: func(client *githubconnector.Client, stepExecutionID string) changeQueryOutcome {
				result, err := sdkgo.RunQuery(testsupport.NewDexContext("changes-flow", stepExecutionID), client.ListCommits(), githubConnection,
					githubconnector.ListCommitsInput{Owner: "octocat", Repository: "hello-world", Since: windowStart, Until: windowEnd})
				return newChangeQueryOutcome(result.Branch, result.Failure, result, err)
			},
			writeSuccess: func(t *testing.T, response http.ResponseWriter) {
				writeJSON(t, response, []map[string]any{commitJSON(strings.Repeat("d", 40), "mona", "Commit")})
			},
			malformedBodies: map[string]string{
				"not json":         providerBodySentinel,
				"null":             `null`,
				"invalid sha":      `[{"sha":"` + providerBodySentinel + `","commit":{"author":{"date":"2026-09-02T00:00:00Z"},"committer":{"date":"2026-09-02T00:00:00Z"}}}]`,
				"missing author":   `[{"sha":"` + strings.Repeat("e", 40) + `","commit":{"committer":{"date":"2026-09-02T00:00:00Z"}}}]`,
				"missing commit":   `[{"sha":"` + strings.Repeat("e", 40) + `"}]`,
				"uppercase sha":    `[{"sha":"` + strings.Repeat("E", 40) + `","commit":{"author":{"date":"2026-09-02T00:00:00Z"},"committer":{"date":"2026-09-02T00:00:00Z"}}}]`,
				"backward link":    `[]`,
				"unnumbered link":  `[]`,
				"wrong item shape": `[1]`,
			},
		},
	}
}

func newChangeQueryOutcome(branch sdkgo.BranchID, failure *sdkgo.Failure, result any, err error) changeQueryOutcome {
	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		panic(marshalErr)
	}
	return changeQueryOutcome{branch: branch, failure: failure, encoded: string(encoded), err: err}
}

func mergedPullRequestsInput() githubconnector.ListMergedPullRequestsInput {
	return githubconnector.ListMergedPullRequestsInput{
		Owner: "octocat", Repository: "hello-world", MergedAfter: windowStart, MergedBefore: windowEnd,
	}
}

func mergedPullRequestJSON(number int, mergedAt string, body string, labels []string) map[string]any {
	labelValues := make([]map[string]any, 0, len(labels))
	for _, label := range labels {
		labelValues = append(labelValues, map[string]any{"id": len(labelValues) + 1, "name": label})
	}
	return map[string]any{
		"number": number, "title": fmt.Sprintf("  Pull request %d  ", number), "body": body, "state": "closed",
		"html_url": fmt.Sprintf("https://github.com/octocat/hello-world/pull/%d", number),
		"user":     map[string]any{"login": "mona", "id": 7}, "labels": labelValues,
		"pull_request": map[string]any{
			"url": fmt.Sprintf("https://api.github.com/repos/octocat/hello-world/pulls/%d", number), "merged_at": mergedAt,
		},
	}
}

func commitJSON(sha string, login string, message string) map[string]any {
	var author any
	if login != "" {
		author = map[string]any{"login": login, "id": 7}
	}
	return map[string]any{
		"sha": sha, "html_url": "https://github.com/octocat/hello-world/commit/" + sha, "author": author,
		"commit": map[string]any{
			"message":   message,
			"author":    map[string]any{"name": "Mona Lisa", "email": "mona@example.com", "date": "2026-09-02T03:00:00-07:00"},
			"committer": map[string]any{"name": "GitHub", "email": "noreply@github.com", "date": "2026-09-03T11:00:00Z"},
		},
	}
}

func requireGitHubHeaders(t *testing.T, request *http.Request) {
	t.Helper()
	require.Equal(t, "Bearer one-use-token", request.Header.Get("Authorization"))
	require.Equal(t, "application/vnd.github+json", request.Header.Get("Accept"))
	require.Equal(t, "2026-03-10", request.Header.Get("X-GitHub-Api-Version"))
}

func setGrantedScopes(response http.ResponseWriter) {
	response.Header().Set("X-OAuth-Scopes", "read:user, user:email")
}

func requireRedacted(t *testing.T, result any) {
	t.Helper()
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "one-use-token")
	require.NotContains(t, string(encoded), providerBodySentinel)
	require.NotContains(t, fmt.Sprintf("%#v", result), "one-use-token")
}
