// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/github/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestCreateIssueCommentSendsAMarkedBodyAndReturnsTheComment(t *testing.T) {
	provider := newCommentProvider(t)
	client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)
	ctx := singledispatchtest.NewContext("issues-flow", "comment")

	result, err := sdkgo.RunMutation(ctx, client.CreateIssueComment(), githubConnection, triageNoteInput())
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueCommentBranchCreated, result.Branch, result.Failure)
	require.Equal(t, 7, result.Value.IssueNumber)
	require.Equal(t, int64(5001), result.Value.CommentID)
	require.Equal(t, "https://github.com/octocat/hello-world/issues/7#issuecomment-5001", result.Value.URL)
	require.NotNil(t, result.Value.CreatedAt)
	require.Equal(t, "5001", result.Receipt.ProviderObjectID)
	sent := provider.sentBodies()
	require.Len(t, sent, 1)
	require.Regexp(t, `^Reproduced on the latest release\.\n\n<!-- dex-connector-call: [0-9a-f]{64} -->$`, sent[0])
	require.NotContains(t, sent[0], "issues-flow", "the marker holds a digest, never the Flow ID")
	require.Equal(t, singledispatch.Marker{CallID: result.Receipt.CallID, Attempt: 1, State: singledispatch.StateClaimed, ProviderID: "5001", SendCount: 1}, lastMarker(t, ctx))
	requireRedacted(t, result)
}

func TestCreateIssueCommentReadsBackACommentThatAnUnknownOutcomeCreated(t *testing.T) {
	provider := newCommentProvider(t, commentStoreThen502)
	provider.addCommentsByAnotherAccount(250)
	client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)
	ctx := singledispatchtest.NewContext("issues-flow", "comment-unknown")

	result, err := sdkgo.RunMutation(ctx, client.CreateIssueComment(), githubConnection, triageNoteInput())
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueCommentBranchCreated, result.Branch, result.Failure)
	require.Equal(t, "true", result.Receipt.Metadata["foundByReadBack"])
	require.Equal(t, 1, provider.count(http.MethodPost))
	require.Equal(t, []string{"1", "3"}, provider.listedPages(), "read-back reads the first page, then the newest page")
}

func TestCreateIssueCommentResendsAfterReadBackFindsNothingBelowTheCap(t *testing.T) {
	provider := newCommentProvider(t, commentDrop502)
	client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)
	ctx := singledispatchtest.NewContext("issues-flow", "comment-resend")

	_, err := sdkgo.RunMutation(ctx, client.CreateIssueComment(), githubConnection, triageNoteInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateIssueComment(), githubConnection, triageNoteInput())
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueCommentBranchCreated, result.Branch, result.Failure)
	require.Equal(t, 2, provider.count(http.MethodPost))
	sent := provider.sentBodies()
	require.Equal(t, sent[0], sent[1], "both sends carry the Call ID marker")
	require.Equal(t, int32(2), lastMarker(t, ctx).SendCount)
}

func TestCreateIssueCommentSelectsUncertainOnlyAtTheSendCap(t *testing.T) {
	provider := newCommentProvider(t, commentDrop502, commentDrop502, commentDrop502)
	client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)
	ctx := singledispatchtest.NewContext("issues-flow", "comment-cap")
	for attempt := 1; attempt <= 2; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.CreateIssueComment(), githubConnection, triageNoteInput())
		require.Error(t, err, "attempt %d retries", attempt)
		ctx.AdvanceAttempt()
	}
	result, err := sdkgo.RunMutation(ctx, client.CreateIssueComment(), githubConnection, triageNoteInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Equal(t, github.CreatedIssueComment{IssueNumber: 7}, result.Value)
	require.Equal(t, 3, provider.count(http.MethodPost), "the manifest's maxSends caps the sends")

	oneSend := newCommentProvider(t, commentDrop502)
	oneSendClient := newIssueClient(t, oneSend.server.URL, github.RepositoryAuthMethodID)
	result, err = sdkgo.RunMutation(singledispatchtest.NewContext("issues-flow", "comment-one-send"), oneSendClient.CreateIssueComment(), githubConnection, triageNoteInput(), sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Equal(t, 1, oneSend.count(http.MethodPost))
}

func TestCreateIssueCommentReadsTheSavedCommentIDAfterALostStepResult(t *testing.T) {
	provider := newCommentProvider(t)
	client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)
	ctx := singledispatchtest.NewContext("issues-flow", "comment-lost")
	first, err := sdkgo.RunMutation(ctx, client.CreateIssueComment(), githubConnection, triageNoteInput())
	require.NoError(t, err)
	ctx.AdvanceAttempt()

	second, err := sdkgo.RunMutation(ctx, client.CreateIssueComment(), githubConnection, triageNoteInput())
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueCommentBranchCreated, second.Branch, second.Failure)
	require.Equal(t, first.Value.CommentID, second.Value.CommentID)
	require.Equal(t, 1, provider.count(http.MethodPost))
	require.Equal(t, []string{"/repos/octocat/hello-world/issues/comments/5001"}, provider.readComments())
}

func TestCreateIssueCommentRetriesUnprocessedWritesWithoutReadBack(t *testing.T) {
	for _, behavior := range []commentWriteBehavior{comment429, comment503} {
		t.Run(string(behavior), func(t *testing.T) {
			provider := newCommentProvider(t, behavior)
			client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)
			ctx := singledispatchtest.NewContext("issues-flow", "comment-unprocessed")
			_, err := sdkgo.RunMutation(ctx, client.CreateIssueComment(), githubConnection, triageNoteInput())
			require.Error(t, err)
			require.Equal(t, singledispatch.StateCanResend, lastMarker(t, ctx).State)
			require.Zero(t, provider.count(http.MethodGet))
		})
	}
}

func TestCreateIssueCommentClassifiesConclusiveRejectionsAndInput(t *testing.T) {
	for _, test := range []struct {
		behavior commentWriteBehavior
		branch   sdkgo.BranchID
		kind     sdkgo.FailureKind
	}{
		{behavior: comment403, branch: github.CreateIssueCommentBranchInsufficientScope, kind: sdkgo.FailureAuthorization},
		{behavior: comment404, branch: github.CreateIssueCommentBranchNotFound, kind: sdkgo.FailureNotFound},
		{behavior: comment410, branch: github.CreateIssueCommentBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
		{behavior: comment422, branch: github.CreateIssueCommentBranchProviderRejected, kind: sdkgo.FailureValidation},
	} {
		t.Run(string(test.behavior), func(t *testing.T) {
			provider := newCommentProvider(t, test.behavior)
			client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)
			result, err := sdkgo.RunMutation(singledispatchtest.NewContext("issues-flow", "comment-rejected"), client.CreateIssueComment(), githubConnection, triageNoteInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Zero(t, provider.count(http.MethodGet))
			requireRedacted(t, result)
		})
	}
	provider := newCommentProvider(t)
	publicClient := newIssueClient(t, provider.server.URL, github.PublicAuthMethodID)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("issues-flow", "comment-public"), publicClient.CreateIssueComment(), githubConnection, triageNoteInput())
	require.NoError(t, err)
	require.Equal(t, github.CreateIssueCommentBranchInsufficientScope, result.Branch)
	client := newIssueClient(t, provider.server.URL, github.RepositoryAuthMethodID)
	for name, input := range map[string]github.CreateIssueCommentInput{
		"blank body":        {Owner: "octocat", Repository: "hello-world", IssueNumber: 7, Body: " \n"},
		"zero issue number": {Owner: "octocat", Repository: "hello-world", Body: "Note"},
		"oversized body":    {Owner: "octocat", Repository: "hello-world", IssueNumber: 7, Body: strings.Repeat("a", 65536)},
	} {
		result, err := sdkgo.RunMutation(singledispatchtest.NewContext("issues-flow", "comment-defect"), client.CreateIssueComment(), githubConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, github.CreateIssueCommentBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.count(http.MethodPost))
}

func triageNoteInput() github.CreateIssueCommentInput {
	return github.CreateIssueCommentInput{Owner: "octocat", Repository: "hello-world", IssueNumber: 7, Body: "Reproduced on the latest release."}
}

// commentWriteBehavior scripts one POST answer of the fake comment API.
type commentWriteBehavior string

const (
	commentStore201     commentWriteBehavior = "store and answer 201"
	commentStoreThen502 commentWriteBehavior = "store then answer 502"
	commentDrop502      commentWriteBehavior = "answer 502 without storing"
	comment429          commentWriteBehavior = "answer 429"
	comment503          commentWriteBehavior = "answer 503"
	comment403          commentWriteBehavior = "answer 403"
	comment404          commentWriteBehavior = "answer 404"
	comment410          commentWriteBehavior = "answer 410"
	comment422          commentWriteBehavior = "answer 422"
)

// commentProvider is a local fake of GitHub's issue comment create, list, and get endpoints.
type commentProvider struct {
	server    *httptest.Server
	t         *testing.T
	mutex     sync.Mutex
	behaviors []commentWriteBehavior
	comments  []map[string]any
	nextID    int64
	requests  map[string]int
	sent      []string
	pages     []string
	reads     []string
}

func newCommentProvider(t *testing.T, behaviors ...commentWriteBehavior) *commentProvider {
	t.Helper()
	provider := &commentProvider{t: t, behaviors: behaviors, nextID: 5001, requests: map[string]int{}}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.server.Close)
	return provider
}

func (provider *commentProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	require.Equal(provider.t, "Bearer repository-token", request.Header.Get("Authorization"))
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.requests[request.Method]++
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/user":
		writeJSON(provider.t, response, map[string]any{"login": "octocat"})
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/repos/octocat/hello-world/issues/comments/"):
		provider.reads = append(provider.reads, request.URL.Path)
		commentID, _ := strconv.ParseInt(strings.TrimPrefix(request.URL.Path, "/repos/octocat/hello-world/issues/comments/"), 10, 64)
		for _, comment := range provider.comments {
			if comment["id"] == commentID {
				writeJSON(provider.t, response, comment)
				return
			}
		}
		http.NotFound(response, request)
	case request.Method == http.MethodGet && request.URL.Path == "/repos/octocat/hello-world/issues/7/comments":
		provider.serveList(response, request)
	case request.Method == http.MethodPost && request.URL.Path == "/repos/octocat/hello-world/issues/7/comments":
		provider.serveCreate(response, request)
	default:
		http.NotFound(response, request)
	}
}

func (provider *commentProvider) serveList(response http.ResponseWriter, request *http.Request) {
	pageSize, _ := strconv.Atoi(request.URL.Query().Get("per_page"))
	page, _ := strconv.Atoi(request.URL.Query().Get("page"))
	provider.pages = append(provider.pages, strconv.Itoa(page))
	lastPage := max((len(provider.comments)+pageSize-1)/pageSize, 1)
	if lastPage > 1 {
		response.Header().Set("Link", fmt.Sprintf(`<https://api.github.com/repositories/1/issues/7/comments?page=%d>; rel="last"`, lastPage))
	}
	start, end := min((page-1)*pageSize, len(provider.comments)), min(page*pageSize, len(provider.comments))
	writeJSON(provider.t, response, provider.comments[start:end])
}

func (provider *commentProvider) serveCreate(response http.ResponseWriter, request *http.Request) {
	var sent struct {
		Body string `json:"body"`
	}
	require.NoError(provider.t, json.NewDecoder(request.Body).Decode(&sent))
	provider.sent = append(provider.sent, sent.Body)
	behavior := commentStore201
	if len(provider.behaviors) > 0 {
		behavior, provider.behaviors = provider.behaviors[0], provider.behaviors[1:]
	}
	statuses := map[commentWriteBehavior]int{comment429: 429, comment503: 503, comment403: 403, comment404: 404, comment410: 410, comment422: 422, commentDrop502: 502}
	if status, isScripted := statuses[behavior]; isScripted {
		if status == http.StatusTooManyRequests {
			response.Header().Set("Retry-After", "3")
		}
		response.WriteHeader(status)
		_, _ = response.Write([]byte(`{"message":"` + providerBodySentinel + `"}`))
		return
	}
	stored := provider.storeLocked(sent.Body, "octocat")
	if behavior == commentStoreThen502 {
		response.WriteHeader(http.StatusBadGateway)
		return
	}
	response.WriteHeader(http.StatusCreated)
	writeJSON(provider.t, response, stored)
}

func (provider *commentProvider) storeLocked(body string, author string) map[string]any {
	commentID := provider.nextID
	provider.nextID++
	comment := map[string]any{
		"id": commentID, "body": body, "user": map[string]any{"login": author},
		"html_url":   "https://github.com/octocat/hello-world/issues/7#issuecomment-" + strconv.FormatInt(commentID, 10),
		"created_at": time.Date(2026, 3, 7, 10, 20, 0, 0, time.UTC).Format(time.RFC3339),
	}
	provider.comments = append(provider.comments, comment)
	return comment
}

func (provider *commentProvider) addCommentsByAnotherAccount(count int) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	for range count {
		provider.storeLocked("Another reply", "hubot")
	}
}

func (provider *commentProvider) count(method string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[method]
}

func (provider *commentProvider) sentBodies() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.sent...)
}

func (provider *commentProvider) listedPages() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.pages...)
}

func (provider *commentProvider) readComments() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.reads...)
}
