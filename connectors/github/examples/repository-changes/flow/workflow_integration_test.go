//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package repositorychanges

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/github"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var (
	integrationWindowStart = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	integrationWindowEnd   = time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
)

// TestRepositoryChangesExampleRetriesRateLimitAndCompletesWithRealDex answers the first search with a
// secondary rate limit; Dex retries after Retry-After.
func TestRepositoryChangesExampleRetriesRateLimitAndCompletesWithRealDex(t *testing.T) {
	provider := newGitHubProvider(t)
	defer provider.Close()
	flow, harness := newRepositoryChangesHarness(t, provider.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	flowID := "github-repository-changes-success-" + testRunID
	requestID := "start-" + flowID
	input := Input{Owner: "octocat", Repository: "hello-world", WindowStart: integrationWindowStart, WindowEnd: integrationWindowEnd}
	options := dex.StartFlowOptions{RequestID: &requestID, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}}

	runID, err := harness.client.StartFlow(ctx, flow, flowID, input, options)
	require.NoError(t, err)
	repeatedRunID, err := harness.client.StartFlow(ctx, flow, flowID, input, options)
	require.NoError(t, err, "a retry of the same logical start attaches to the existing run")
	require.Equal(t, runID, repeatedRunID)

	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var report Report
	require.NoError(t, result.DecodeSingleOutput(&report))
	require.Equal(t, StatusCompleted, report.Status)
	require.Equal(t, input.Owner, report.Input.Owner)
	require.True(t, report.Input.WindowStart.Equal(integrationWindowStart))
	require.Equal(t, 57, report.MergedPullRequestCount)
	require.True(t, report.MorePullRequests)
	require.Len(t, report.PullRequests, 4)
	for index, want := range []int{44, 43, 42, 41} {
		pullRequest := report.PullRequests[index]
		require.Equal(t, want, pullRequest.PullRequest.Number)
		require.Equal(t, index < PullRequestsWithFiles, pullRequest.FilesLoaded, "pull request %d", want)
	}
	require.Equal(t, "docs/pull-44.md", report.PullRequests[0].Files[0].Filename)
	require.True(t, report.PullRequests[0].MoreFiles)
	require.False(t, report.PullRequests[1].MoreFiles)
	require.Empty(t, report.PullRequests[3].Files)
	require.Len(t, report.Commits, 2)
	require.False(t, report.MoreCommits)

	require.Equal(t, 2, provider.count("/search/issues hello-world"), "one rate-limited search and one retry")
	require.GreaterOrEqual(t, provider.searchRetryGap(), 900*time.Millisecond, "Dex honored GitHub's Retry-After delay")
	for _, number := range []string{"44", "43", "42"} {
		require.Equal(t, 1, provider.count("/repos/octocat/hello-world/pulls/"+number+"/files"), number)
	}
	require.Zero(t, provider.count("/repos/octocat/hello-world/pulls/41/files"))
	require.Equal(t, 1, provider.count("/repos/octocat/hello-world/commits"))
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "github-integration-token")
	require.NotContains(t, string(encoded), "mona@example.com")
}

// TestRepositoryChangesExampleReportsAnEmptyRepositoryWithRealDex completes a report for a repository
// whose commits GitHub answers with 409.
func TestRepositoryChangesExampleReportsAnEmptyRepositoryWithRealDex(t *testing.T) {
	provider := newGitHubProvider(t)
	defer provider.Close()
	flow, harness := newRepositoryChangesHarness(t, provider.URL)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "github-repository-changes-empty-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := Input{Owner: "octocat", Repository: "empty-repository", WindowStart: integrationWindowStart, WindowEnd: integrationWindowEnd}

	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var report Report
	require.NoError(t, result.DecodeSingleOutput(&report))
	require.Equal(t, StatusCompleted, report.Status)
	require.Empty(t, report.PullRequests)
	require.Empty(t, report.Commits)
	require.Equal(t, 1, provider.count("/repos/octocat/empty-repository/commits"))
}

// TestRepositoryChangesExampleFailsAnUnwiredProviderRejectionWithRealDex shows the unwired optional
// providerRejected branch failing the Flow without retrying.
func TestRepositoryChangesExampleFailsAnUnwiredProviderRejectionWithRealDex(t *testing.T) {
	provider := newGitHubProvider(t)
	defer provider.Close()
	flow, harness := newRepositoryChangesHarness(t, provider.URL)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "github-repository-changes-rejected-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := Input{Owner: "octocat", Repository: "missing-repository", WindowStart: integrationWindowStart, WindowEnd: integrationWindowEnd}

	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Equal(t, 1, provider.count("/search/issues missing-repository"))
	require.Zero(t, provider.count("/repos/octocat/missing-repository/commits"))
}

// TestRepositoryChangesExampleFailsARateLimitResetPastTheRetryBudgetWithRealDex uses a reset two hours
// away, beyond the 65-minute budget, so Dex fails fast.
func TestRepositoryChangesExampleFailsARateLimitResetPastTheRetryBudgetWithRealDex(t *testing.T) {
	provider := newGitHubProvider(t)
	defer provider.Close()
	flow, harness := newRepositoryChangesHarness(t, provider.URL)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "github-repository-changes-exhausted-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := Input{Owner: "octocat", Repository: "exhausted-repository", WindowStart: integrationWindowStart, WindowEnd: integrationWindowEnd}

	started := time.Now()
	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "GitHub rate limit was reached")
	require.NotContains(t, result.ErrorMessage, "github-integration-token")
	require.Less(t, time.Since(started), 30*time.Second, "Dex failed the Step instead of waiting for the reset")
	searches := provider.count("/search/issues exhausted-repository")
	require.GreaterOrEqual(t, searches, 1)
	require.LessOrEqual(t, searches, 2, "at most the local attempt and one immediate fallback retry")
	require.Zero(t, provider.count("/repos/octocat/exhausted-repository/commits"))
}

func waitForTerminalFlow(t *testing.T, ctx context.Context, client *dex.Client, flowID string) dex.FlowResult {
	t.Helper()
	for {
		result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err, "Flow %s did not reach a terminal status", flowID)
		return result
	}
}

// gitHubProvider is a local fake of the documented GitHub REST endpoints the example reads.
type gitHubProvider struct {
	*httptest.Server
	mutex          sync.Mutex
	requests       map[string]int
	searchAttempts []time.Time
}

func newGitHubProvider(t *testing.T) *gitHubProvider {
	t.Helper()
	provider := &gitHubProvider{requests: map[string]int{}}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	return provider
}

func (provider *gitHubProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer github-integration-token" {
		http.Error(response, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		return
	}
	response.Header().Set("X-OAuth-Scopes", "read:user, user:email")
	response.Header().Set("Content-Type", "application/json")
	path := request.URL.Path
	switch {
	case path == "/search/issues":
		repository := strings.TrimPrefix(strings.Fields(request.URL.Query().Get("q"))[0], "repo:octocat/")
		attempt := provider.record(path+" "+repository, repository == "hello-world")
		switch {
		case repository == "missing-repository":
			response.WriteHeader(http.StatusUnprocessableEntity)
			writeProviderBody(response, map[string]any{"message": "Validation Failed"})
		case repository == "exhausted-repository":
			response.Header().Set("X-RateLimit-Remaining", "0")
			response.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(2*time.Hour).Unix(), 10))
			response.WriteHeader(http.StatusForbidden)
			writeProviderBody(response, map[string]any{"message": "API rate limit exceeded for user ID 1."})
		case repository == "hello-world" && attempt == 1:
			response.Header().Set("Retry-After", "1")
			response.WriteHeader(http.StatusForbidden)
			writeProviderBody(response, map[string]any{"message": "You have exceeded a secondary rate limit."})
		case repository == "hello-world":
			response.Header().Set("Link", `<https://api.github.com/search/issues?page=2>; rel="next"`)
			items := []map[string]any{}
			for _, number := range []int{44, 43, 42, 41} {
				items = append(items, map[string]any{
					"number": number, "title": "Pull request " + strconv.Itoa(number), "body": "Summary",
					"html_url": "https://github.com/octocat/hello-world/pull/" + strconv.Itoa(number),
					"user":     map[string]any{"login": "mona"}, "labels": []any{map[string]any{"name": "enhancement"}},
					"pull_request": map[string]any{"merged_at": "2026-09-0" + strconv.Itoa(number-39) + "T12:00:00Z"},
				})
			}
			writeProviderBody(response, map[string]any{"total_count": 57, "incomplete_results": false, "items": items})
		default:
			writeProviderBody(response, map[string]any{"total_count": 0, "incomplete_results": false, "items": []any{}})
		}
	case strings.HasPrefix(path, "/repos/octocat/hello-world/pulls/") && strings.HasSuffix(path, "/files"):
		provider.record(path, false)
		number := strings.TrimSuffix(strings.TrimPrefix(path, "/repos/octocat/hello-world/pulls/"), "/files")
		if number == "44" {
			response.Header().Set("Link", `<https://api.github.com/repositories/1/pulls/44/files?page=2>; rel="next"`)
		}
		writeProviderBody(response, []map[string]any{
			{"filename": "docs/pull-" + number + ".md", "status": "modified", "additions": 2, "deletions": 1, "changes": 3, "patch": "@@ -1 +1 @@"},
		})
	case path == "/repos/octocat/hello-world/commits":
		provider.record(path, false)
		commits := []map[string]any{}
		for _, sha := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40)} {
			commits = append(commits, map[string]any{
				"sha": sha, "html_url": "https://github.com/octocat/hello-world/commit/" + sha, "author": map[string]any{"login": "mona"},
				"commit": map[string]any{
					"message":   "Change " + sha[:1],
					"author":    map[string]any{"name": "Mona Lisa", "email": "mona@example.com", "date": "2026-09-03T10:00:00Z"},
					"committer": map[string]any{"name": "GitHub", "email": "noreply@github.com", "date": "2026-09-03T10:00:00Z"},
				},
			})
		}
		writeProviderBody(response, commits)
	case path == "/repos/octocat/empty-repository/commits":
		provider.record(path, false)
		response.WriteHeader(http.StatusConflict)
		writeProviderBody(response, map[string]any{"message": "Git Repository is empty."})
	case path == "/repos/octocat/missing-repository/commits":
		provider.record(path, false)
		http.NotFound(response, request)
	default:
		http.NotFound(response, request)
	}
}

func (provider *gitHubProvider) record(key string, isSearchAttempt bool) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.requests[key]++
	if isSearchAttempt {
		provider.searchAttempts = append(provider.searchAttempts, time.Now())
	}
	return provider.requests[key]
}

func (provider *gitHubProvider) count(key string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[key]
}

func (provider *gitHubProvider) searchRetryGap() time.Duration {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if len(provider.searchAttempts) < 2 {
		return 0
	}
	return provider.searchAttempts[1].Sub(provider.searchAttempts[0])
}

func writeProviderBody(response http.ResponseWriter, value any) {
	if err := json.NewEncoder(response).Encode(value); err != nil {
		panic(err)
	}
}

type repositoryChangesHarness struct {
	registry *dex.Registry
	cache    *blobcache.Cache
	worker   *dex.Worker
	result   chan error
	client   *dex.Client
}

func newRepositoryChangesHarness(t *testing.T, baseURL string) (*Flow, *repositoryChangesHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "github", Name: ConnectionName}
	providerClient, err := github.New(github.Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[github.Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString("github-integration-token")},
	})
	require.NoError(t, err)
	connection, err := github.NewConnection(providerClient, reference)
	require.NoError(t, err)
	return newRepositoryChangesHarnessWithConnection(t, connection)
}

// newRepositoryChangesHarnessWithConnection runs the Flow with one GitHub Connection on a new Worker.
func newRepositoryChangesHarnessWithConnection(t *testing.T, connection github.Connection) (*Flow, *repositoryChangesHarness) {
	t.Helper()
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	workerAddress := net.JoinHostPort("127.0.0.1", availablePort(t))
	harness := &repositoryChangesHarness{registry: registry, cache: cache, result: make(chan error, 1)}
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	go func() { harness.result <- harness.worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.result, harness.client.Close(), harness.cache.Close()))
	})
	return flow, harness
}

func availablePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
