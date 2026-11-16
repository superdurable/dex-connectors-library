//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package issuefiling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/github/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationToken = "github-integration-token"

// TestIssueFilingExampleCreatesTheIssueAfterCheckingEveryOpenPageWithRealDex reads two pages of open bug
// issues without a matching title, then creates the issue once.
func TestIssueFilingExampleCreatesTheIssueAfterCheckingEveryOpenPageWithRealDex(t *testing.T) {
	provider := newIssueProvider(t)
	provider.addOpenIssues(150, "bug")
	flow, harness := newIssueFilingHarness(t, provider.URL, github.RepositoryAuthMethodID)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	flowID := "github-issue-filing-created-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	requestID := "start-" + flowID
	options := dex.StartFlowOptions{RequestID: &requestID, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}}

	runID, err := harness.client.StartFlow(ctx, flow, flowID, bugReportInput(), options)
	require.NoError(t, err)
	repeatedRunID, err := harness.client.StartFlow(ctx, flow, flowID, bugReportInput(), options)
	require.NoError(t, err, "a retry of the same logical start attaches to the existing run")
	require.Equal(t, runID, repeatedRunID)

	report := completedReport(t, ctx, harness.client, flowID)
	require.Equal(t, StatusCreated, report.Status)
	require.Equal(t, 2, report.OpenIssuePagesRead)
	require.False(t, report.HasUncheckedOpenIssues)
	require.Nil(t, report.ExistingIssue)
	require.NotNil(t, report.CreatedIssue)
	require.Equal(t, 151, report.CreatedIssue.Number)
	require.Equal(t, "I_node151", report.CreatedIssue.NodeID)
	require.Equal(t, "https://github.com/octocat/hello-world/issues/151", report.CreatedIssue.URL)
	require.Equal(t, []string{"bug"}, report.CreatedIssue.Labels)
	require.Equal(t, 1, provider.count("POST"))
	created := provider.issuesTitled(bugReportInput().Title)
	require.Len(t, created, 1)
	require.Regexp(t, `^Saving a new draft without text closes the editor\.\n\n<!-- dex-connector-call: [0-9a-f]{64} -->$`, created[0]["body"])
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), integrationToken)
}

// TestIssueFilingExampleFindsAnAlreadyTrackedIssueWithRealDex finds the title on the second open page and
// creates nothing.
func TestIssueFilingExampleFindsAnAlreadyTrackedIssueWithRealDex(t *testing.T) {
	provider := newIssueProvider(t)
	provider.addOpenIssueTitled("  crash when saving an EMPTY draft ", "bug")
	provider.addOpenIssues(120, "bug")
	flow, harness := newIssueFilingHarness(t, provider.URL, github.RepositoryAuthMethodID)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "github-issue-filing-tracked-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	_, err := harness.client.StartFlow(ctx, flow, flowID, bugReportInput(), dex.StartFlowOptions{})
	require.NoError(t, err)
	report := completedReport(t, ctx, harness.client, flowID)
	require.Equal(t, StatusAlreadyTracked, report.Status)
	require.Equal(t, 2, report.OpenIssuePagesRead)
	require.NotNil(t, report.ExistingIssue)
	require.Equal(t, 1, report.ExistingIssue.Number)
	require.Nil(t, report.CreatedIssue)
	require.Zero(t, provider.count("POST"))
}

// TestIssueFilingExampleReadsBackARetriedCreateInsteadOfFilingADuplicateWithRealDex answers the first
// create with 502 after storing the issue and fails the first read-back, so Dex retries the Step; the
// retry reads the persisted claim, finds the marked issue, and sends nothing again.
func TestIssueFilingExampleReadsBackARetriedCreateInsteadOfFilingADuplicateWithRealDex(t *testing.T) {
	provider := newIssueProvider(t, storeThenAnswer502)
	provider.failReadBackLists(1)
	flow, harness := newIssueFilingHarness(t, provider.URL, github.RepositoryAuthMethodID)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	flowID := "github-issue-filing-read-back-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	_, err := harness.client.StartFlow(ctx, flow, flowID, bugReportInput(), dex.StartFlowOptions{})
	require.NoError(t, err)
	report := completedReport(t, ctx, harness.client, flowID)
	require.Equal(t, StatusCreated, report.Status)
	require.Equal(t, 1, provider.count("POST"), "the retry found the first issue instead of creating another")
	require.Len(t, provider.issuesTitled(bugReportInput().Title), 1)
	require.Equal(t, 1, report.CreatedIssue.Number)
	require.Equal(t, 2, provider.readBackLists(), "one failed read-back in the first attempt and one in the retry")
}

// TestIssueFilingExampleResendsOnlyAfterReadBackFindsNoIssueWithRealDex answers the first create with 502
// without storing it; the retry reads back, finds nothing, and sends once more.
func TestIssueFilingExampleResendsOnlyAfterReadBackFindsNoIssueWithRealDex(t *testing.T) {
	provider := newIssueProvider(t, answer502WithoutStoring)
	flow, harness := newIssueFilingHarness(t, provider.URL, github.RepositoryAuthMethodID)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	flowID := "github-issue-filing-resend-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	_, err := harness.client.StartFlow(ctx, flow, flowID, bugReportInput(), dex.StartFlowOptions{})
	require.NoError(t, err)
	report := completedReport(t, ctx, harness.client, flowID)
	require.Equal(t, StatusCreated, report.Status)
	require.Equal(t, 2, provider.count("POST"))
	require.Len(t, provider.issuesTitled(bugReportInput().Title), 1)
	sent := provider.sentBodies()
	require.Equal(t, sent[0], sent[1], "both sends carry the Call ID marker")
}

// TestIssueFilingExampleFailsOnAPublicConnectionWithoutWritingWithRealDex shows the unwired optional
// insufficientScope branch failing the Flow before any create.
func TestIssueFilingExampleFailsOnAPublicConnectionWithoutWritingWithRealDex(t *testing.T) {
	provider := newIssueProvider(t)
	provider.grantedScopes = "read:user, user:email"
	flow, harness := newIssueFilingHarness(t, provider.URL, github.PublicAuthMethodID)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "github-issue-filing-public-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	_, err := harness.client.StartFlow(ctx, flow, flowID, bugReportInput(), dex.StartFlowOptions{})
	require.NoError(t, err)
	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "insufficientScope")
	require.NotContains(t, result.ErrorMessage, integrationToken)
	require.Zero(t, provider.count("POST"))
}

// TestIssueFilingExampleClosesTheIssueItFiledWithRealDex files a resolved problem and closes the created
// issue. GitHub applies the first close but answers 502, so Dex retries the Step and sends the same state
// again.
func TestIssueFilingExampleClosesTheIssueItFiledWithRealDex(t *testing.T) {
	provider := newIssueProvider(t)
	provider.applyThenFailStateUpdates(1)
	flow, harness := newIssueFilingHarness(t, provider.URL, github.RepositoryAuthMethodID)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	flowID := "github-issue-filing-closed-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := bugReportInput()
	input.CloseWithStateReason = github.IssueStateReasonNotPlanned

	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	report := completedReport(t, ctx, harness.client, flowID)
	require.Equal(t, StatusClosed, report.Status)
	require.Equal(t, 1, report.CreatedIssue.Number)
	require.NotNil(t, report.ClosedIssue)
	require.Equal(t, 1, report.ClosedIssue.Number)
	require.Equal(t, "closed", report.ClosedIssue.State)
	require.Equal(t, "not_planned", report.ClosedIssue.StateReason)
	require.Equal(t, "https://github.com/octocat/hello-world/issues/1", report.ClosedIssue.URL)
	require.NotNil(t, report.ClosedIssue.UpdatedAt)
	require.Equal(t, 1, provider.count("POST"))
	require.Equal(t, 2, provider.count("PATCH"), "the retry repeats the absolute state")
	require.Equal(t, []string{`{"state":"closed","state_reason":"not_planned"}`, `{"state":"closed","state_reason":"not_planned"}`}, provider.sentStateUpdates())
	issues := provider.issuesTitled(input.Title)
	require.Len(t, issues, 1)
	require.Equal(t, "closed", issues[0]["state"])
	require.Equal(t, "not_planned", issues[0]["state_reason"])
}

// TestIssueFilingExampleRejectsAReopenReasonBeforeFilingWithRealDex fails the Flow before any provider
// request when the close reason cannot close an issue.
func TestIssueFilingExampleRejectsAReopenReasonBeforeFilingWithRealDex(t *testing.T) {
	provider := newIssueProvider(t)
	flow, harness := newIssueFilingHarness(t, provider.URL, github.RepositoryAuthMethodID)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "github-issue-filing-reopen-reason-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := bugReportInput()
	input.CloseWithStateReason = github.IssueStateReasonReopened

	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "closeWithStateReason")
	require.Zero(t, provider.count("GET")+provider.count("POST")+provider.count("PATCH"))
}

func bugReportInput() Input {
	return Input{
		Owner: "octocat", Repository: "hello-world", Title: "Crash when saving an empty draft",
		Body: "Saving a new draft without text closes the editor.", Labels: []string{"bug"},
	}
}

func completedReport(t *testing.T, ctx context.Context, client *dex.Client, flowID string) Report {
	t.Helper()
	result := waitForTerminalFlow(t, ctx, client, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var report Report
	require.NoError(t, result.DecodeSingleOutput(&report))
	return report
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

// issueWriteBehavior scripts one create answer of the fake GitHub issue API.
type issueWriteBehavior string

const (
	storeAndAnswer201       issueWriteBehavior = "store and answer 201"
	storeThenAnswer502      issueWriteBehavior = "store then answer 502"
	answer502WithoutStoring issueWriteBehavior = "answer 502 without storing"
)

// issueProvider is a local fake of the documented GitHub issue list, create, and update endpoints.
type issueProvider struct {
	*httptest.Server
	t                    *testing.T
	mutex                sync.Mutex
	grantedScopes        string
	behaviors            []issueWriteBehavior
	issues               map[int]map[string]any
	nextNumber           int
	requests             map[string]int
	sent                 []string
	readBackListFailures int
	readBackListCount    int
	stateUpdateFailures  int
	stateUpdates         []string
}

func newIssueProvider(t *testing.T, behaviors ...issueWriteBehavior) *issueProvider {
	t.Helper()
	provider := &issueProvider{t: t, grantedScopes: "repo", behaviors: behaviors, issues: map[int]map[string]any{}, nextNumber: 1, requests: map[string]int{}}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *issueProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+integrationToken {
		http.Error(response, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		return
	}
	response.Header().Set("X-OAuth-Scopes", provider.grantedScopes)
	response.Header().Set("Content-Type", "application/json")
	provider.mutex.Lock()
	provider.requests[request.Method]++
	provider.mutex.Unlock()
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/user":
		writeProviderBody(response, map[string]any{"login": "octocat", "id": 1})
	case request.Method == http.MethodGet && request.URL.Path == "/repos/octocat/hello-world/issues":
		provider.serveList(response, request)
	case request.Method == http.MethodPost && request.URL.Path == "/repos/octocat/hello-world/issues":
		provider.serveCreate(response, request)
	case request.Method == http.MethodPatch && strings.HasPrefix(request.URL.Path, "/repos/octocat/hello-world/issues/"):
		provider.serveStateUpdate(response, request)
	default:
		http.NotFound(response, request)
	}
}

func (provider *issueProvider) serveList(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if query.Get("creator") != "" {
		provider.readBackListCount++
		if provider.readBackListFailures > 0 {
			provider.readBackListFailures--
			response.WriteHeader(http.StatusBadGateway)
			return
		}
	}
	var labels []string
	if query.Get("labels") != "" {
		labels = strings.Split(query.Get("labels"), ",")
	}
	numbers := make([]int, 0, len(provider.issues))
	for number, issue := range provider.issues {
		user, _ := issue["user"].(map[string]any)
		if (query.Get("state") == "all" || issue["state"] == query.Get("state")) && hasEveryLabel(issue, labels) &&
			(query.Get("creator") == "" || user["login"] == query.Get("creator")) {
			numbers = append(numbers, number)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(numbers)))
	pageSize, _ := strconv.Atoi(query.Get("per_page"))
	page, _ := strconv.Atoi(query.Get("page"))
	start, end := min((page-1)*pageSize, len(numbers)), min(page*pageSize, len(numbers))
	if end < len(numbers) {
		response.Header().Set("Link", fmt.Sprintf(`<https://api.github.com/repositories/1/issues?page=%d>; rel="next"`, page+1))
	}
	issues := make([]map[string]any, 0, end-start)
	for _, number := range numbers[start:end] {
		issues = append(issues, provider.issues[number])
	}
	writeProviderBody(response, issues)
}

func (provider *issueProvider) serveCreate(response http.ResponseWriter, request *http.Request) {
	var sent struct {
		Title  string   `json:"title"`
		Body   string   `json:"body"`
		Labels []string `json:"labels"`
	}
	if err := json.NewDecoder(request.Body).Decode(&sent); err != nil {
		http.Error(response, `{"message":"Problems parsing JSON"}`, http.StatusBadRequest)
		return
	}
	provider.mutex.Lock()
	provider.sent = append(provider.sent, sent.Body)
	behavior := storeAndAnswer201
	if len(provider.behaviors) > 0 {
		behavior, provider.behaviors = provider.behaviors[0], provider.behaviors[1:]
	}
	var stored map[string]any
	if behavior != answer502WithoutStoring {
		stored = provider.storeLocked(sent.Title, sent.Body, "octocat", sent.Labels)
	}
	provider.mutex.Unlock()
	if behavior != storeAndAnswer201 {
		response.WriteHeader(http.StatusBadGateway)
		return
	}
	response.WriteHeader(http.StatusCreated)
	writeProviderBody(response, stored)
}

// serveStateUpdate applies a state update; a scripted failure applies it and still answers 502.
func (provider *issueProvider) serveStateUpdate(response http.ResponseWriter, request *http.Request) {
	number, err := strconv.Atoi(strings.TrimPrefix(request.URL.Path, "/repos/octocat/hello-world/issues/"))
	body, readErr := io.ReadAll(request.Body)
	var update struct {
		State       string `json:"state"`
		StateReason string `json:"state_reason"`
	}
	if err != nil || readErr != nil || json.Unmarshal(body, &update) != nil {
		http.Error(response, `{"message":"Problems parsing JSON"}`, http.StatusBadRequest)
		return
	}
	provider.mutex.Lock()
	provider.stateUpdates = append(provider.stateUpdates, string(body))
	issue, isKnown := provider.issues[number]
	if isKnown {
		issue["state"], issue["state_reason"] = update.State, update.StateReason
		issue["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	isFailing := provider.stateUpdateFailures > 0
	if isFailing {
		provider.stateUpdateFailures--
	}
	provider.mutex.Unlock()
	switch {
	case !isKnown:
		http.Error(response, `{"message":"Not Found"}`, http.StatusNotFound)
	case isFailing:
		response.WriteHeader(http.StatusBadGateway)
	default:
		writeProviderBody(response, issue)
	}
}

func (provider *issueProvider) storeLocked(title string, body string, author string, labels []string) map[string]any {
	number := provider.nextNumber
	provider.nextNumber++
	labelValues := make([]any, 0, len(labels))
	for _, label := range labels {
		labelValues = append(labelValues, map[string]any{"name": label})
	}
	issue := map[string]any{
		"id": 1000 + number, "node_id": "I_node" + strconv.Itoa(number), "number": number, "title": title, "body": body, "state": "open",
		"html_url": "https://github.com/octocat/hello-world/issues/" + strconv.Itoa(number), "user": map[string]any{"login": author},
		"labels": labelValues, "assignees": []any{}, "comments": 0,
		"created_at": time.Now().UTC().Format(time.RFC3339), "updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	provider.issues[number] = issue
	return issue
}

func (provider *issueProvider) addOpenIssues(count int, label string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	for index := 0; index < count; index++ {
		provider.storeLocked("Unrelated report "+strconv.Itoa(index), "Details", "hubot", []string{label})
	}
}

func (provider *issueProvider) addOpenIssueTitled(title string, label string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.storeLocked(title, "Reported earlier", "hubot", []string{label})
}

func (provider *issueProvider) failReadBackLists(count int) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.readBackListFailures = count
}

func (provider *issueProvider) applyThenFailStateUpdates(count int) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.stateUpdateFailures = count
}

func (provider *issueProvider) sentStateUpdates() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return slices.Clone(provider.stateUpdates)
}

func (provider *issueProvider) count(method string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[method]
}

func (provider *issueProvider) readBackLists() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.readBackListCount
}

func (provider *issueProvider) sentBodies() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return slices.Clone(provider.sent)
}

func (provider *issueProvider) issuesTitled(title string) []map[string]any {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var issues []map[string]any
	for _, issue := range provider.issues {
		if issue["title"] == title {
			issues = append(issues, issue)
		}
	}
	return issues
}

func hasEveryLabel(issue map[string]any, labels []string) bool {
	values, _ := issue["labels"].([]any)
	for _, label := range labels {
		if !slices.ContainsFunc(values, func(value any) bool {
			named, _ := value.(map[string]any)
			return named["name"] == label
		}) {
			return false
		}
	}
	return true
}

func writeProviderBody(response http.ResponseWriter, value any) {
	if err := json.NewEncoder(response).Encode(value); err != nil {
		panic(err)
	}
}

type issueFilingHarness struct {
	registry *dex.Registry
	cache    *blobcache.Cache
	worker   *dex.Worker
	result   chan error
	client   *dex.Client
}

func newIssueFilingHarness(t *testing.T, baseURL string, authMethodID string) (*Flow, *issueFilingHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "github", Name: ConnectionName}
	providerClient, err := github.New(github.Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[github.Credentials]{
		reference: {AuthMethodID: authMethodID, AccessToken: sdkgo.NewSecretString(integrationToken)},
	})
	require.NoError(t, err)
	connection, err := github.NewConnection(providerClient, reference)
	require.NoError(t, err)
	return newIssueFilingHarnessWithConnection(t, connection)
}

// newIssueFilingHarnessWithConnection runs the Flow with one GitHub Connection on a new Worker.
func newIssueFilingHarnessWithConnection(t *testing.T, connection github.Connection) (*Flow, *issueFilingHarness) {
	t.Helper()
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	harness := &issueFilingHarness{registry: registry, cache: cache, result: make(chan error, 1)}
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	go func() { harness.result <- harness.worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.result, harness.client.Close(), harness.cache.Close()))
	})
	registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancelRegistration()
	require.NoError(t, harness.worker.WaitPullRegistered(registrationContext))
	return flow, harness
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
