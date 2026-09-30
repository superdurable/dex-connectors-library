//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageissue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationCloudID     = "11223344-a1b2-4b33-8c44-def123456789"
	integrationAccessToken = "integration-access-token"
	shortRequestTimeout    = 500 * time.Millisecond
	// slowRequestTimeout outlasts the fake's nine-second responses and Dex's roughly seven-second async local phase.
	slowRequestTimeout = 20 * time.Second
	slowProviderDelay  = 9 * time.Second

	markerReject           = "[reject]"
	markerRateLimit        = "[rate-limit]"
	markerServerError      = "[server-error]"
	markerCreateTimeout    = "[create-timeout]"
	markerSlowCreate       = "[slow-create]"
	markerCommentTimeout   = "[comment-timeout]"
	markerSlowTransition   = "[slow-transition]"
	decoyNearDuplicateKey  = "OPS-1"
	openDuplicateKey       = "OPS-2"
	openDuplicateSummary   = "Badge reader offline"
	firstCreatedIssueKey   = "OPS-4"
	nearDuplicateSummary   = "Fire panel wiring (2025)"
	integrationDestination = "In Progress"
)

func TestNewIssueIsCreatedOnceCommentedAndMovedWithRealDex(t *testing.T) {
	provider, flow, harness := newJiraIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	triage := runTriageFlow(t, ctx, harness, flow, "created", triageInput("Fire panel wiring"))
	require.Equal(t, PhaseTriaged, triage.Phase)
	require.False(t, triage.IsExistingIssueReused, "the near-duplicate %s is not the same issue", decoyNearDuplicateKey)
	require.Equal(t, firstCreatedIssueKey, triage.Issue.Key)
	require.Equal(t, "Panel B trips at 02:00.\nBreaker is warm.", triage.Issue.Description)
	require.NotEmpty(t, triage.CommentID)
	require.Equal(t, integrationDestination, triage.Status.Name)
	require.Equal(t, 1, provider.createCount("Fire panel wiring"))
	require.Equal(t, 1, provider.commentCount(firstCreatedIssueKey))
	require.Equal(t, 1, provider.transitionPostCount(firstCreatedIssueKey))
	require.Equal(t, []string{`project in ("OPS") AND statusCategory in (2, 4) AND summary ~ "\"Fire panel wiring\"" ORDER BY created DESC`}, provider.searchQueries())
}

func TestOpenIssueWithTheSameSummaryIsReusedWithRealDex(t *testing.T) {
	provider, flow, harness := newJiraIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	triage := runTriageFlow(t, ctx, harness, flow, "reused", triageInput(openDuplicateSummary))
	require.Equal(t, PhaseTriaged, triage.Phase)
	require.True(t, triage.IsExistingIssueReused)
	require.Equal(t, openDuplicateKey, triage.Issue.Key)
	require.Zero(t, triage.CreateAttempts)
	require.Zero(t, provider.createCount(openDuplicateSummary))
	require.Equal(t, 1, provider.commentCount(openDuplicateKey))
	require.Equal(t, integrationDestination, triage.Status.Name)
}

func TestRejectedCreateCompletesWithTheFieldIDsWithRealDex(t *testing.T) {
	provider, flow, harness := newJiraIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	summary := markerReject + " Fire panel wiring"
	triage := runTriageFlow(t, ctx, harness, flow, "rejected", triageInput(summary))
	require.Equal(t, PhaseRejected, triage.Phase)
	require.Equal(t, []string{"summary"}, triage.RejectedFieldIDs)
	require.Nil(t, triage.Issue)
	require.Equal(t, 1, provider.createCount(summary))
}

func TestRateLimitedCreateIsRetriedAfterRetryAfterWithRealDex(t *testing.T) {
	provider, flow, harness := newJiraIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	summary := markerRateLimit + " Door sensor noisy"
	triage := runTriageFlow(t, ctx, harness, flow, "rate-limited", triageInput(summary))
	require.Equal(t, PhaseTriaged, triage.Phase)
	require.Equal(t, 1, triage.CreateAttempts, "a 429 created nothing, so Dex retries the same Step execution")
	attempts := provider.createTimesFor(summary)
	require.Len(t, attempts, 2)
	require.GreaterOrEqual(t, attempts[1].Sub(attempts[0]), time.Second, "the retry waits for Retry-After")
}

func TestTimeoutAfterDispatchIsReconciledWithoutCreatingAgainWithRealDex(t *testing.T) {
	provider, flow, harness := newJiraIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)
	summary := markerCreateTimeout + " Lift jammed"
	flowID := startTriageFlow(t, ctx, harness, flow, "timeout", triageInput(summary))

	uncertain := waitForPhase(t, ctx, harness, flow, flowID, PhaseNeedsReconciliation)
	require.NotNil(t, uncertain.UncertainCreate)
	require.Equal(t, sdkgo.FailureTransport, uncertain.UncertainCreate.FailureKind)
	require.NotEmpty(t, uncertain.UncertainCreate.CallID)
	require.Equal(t, 1, provider.createCount(summary), "a timeout after dispatch is never retried")

	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ConfirmCreatedIssue, ConfirmCreatedIssueInput{IssueKey: "OPS-99"}, nil))
	waitForReconciliationNote(t, ctx, harness, flow, flowID, NoteReportedIssueMissing)
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ConfirmCreatedIssue, ConfirmCreatedIssueInput{IssueKey: decoyNearDuplicateKey}, nil))
	waitForReconciliationNote(t, ctx, harness, flow, flowID, NoteReportedIssueMismatch)

	createdKey := provider.issueKeyForSummary(summary)
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ConfirmCreatedIssue, ConfirmCreatedIssueInput{IssueKey: strings.ToLower(createdKey)}, nil))
	triage := waitForFlowOutput(t, ctx, harness, flowID)
	require.Equal(t, PhaseTriaged, triage.Phase)
	require.Equal(t, createdKey, triage.Issue.Key)
	require.Nil(t, triage.UncertainCreate)
	require.Equal(t, 1, triage.CreateAttempts)
	require.Equal(t, 1, provider.createCount(summary), "reconciliation adopted the existing issue")
}

func TestServerErrorIsCreatedAgainOnlyAfterOperatorApprovalWithRealDex(t *testing.T) {
	provider, flow, harness := newJiraIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)
	summary := markerServerError + " HVAC alarm"
	flowID := startTriageFlow(t, ctx, harness, flow, "server-error", triageInput(summary))

	uncertain := waitForPhase(t, ctx, harness, flow, flowID, PhaseNeedsReconciliation)
	require.Equal(t, sdkgo.FailureAvailability, uncertain.UncertainCreate.FailureKind)
	require.Equal(t, 1, provider.createCount(summary))

	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ApproveIssueCreateRetry, nil, nil))
	triage := waitForFlowOutput(t, ctx, harness, flowID)
	require.Equal(t, PhaseTriaged, triage.Phase)
	require.Equal(t, 2, triage.CreateAttempts)
	require.Equal(t, 2, provider.createCount(summary), "only the approved retry sent a second create")
}

// TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex guards sync durability: an async fallback
// attempt would send a create that outlasts Dex's roughly seven-second local phase a second time.
func TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex(t *testing.T) {
	provider, flow, harness := newJiraIntegrationHarness(t, integrationCloudID, slowRequestTimeout)
	ctx := integrationContext(t)

	summary := markerSlowCreate + " Generator test overdue"
	triage := runTriageFlow(t, ctx, harness, flow, "slow-create", triageInput(summary))
	require.Equal(t, PhaseTriaged, triage.Phase)
	require.Equal(t, 1, provider.createCount(summary), "one Step execution dispatches one create request")
}

// TestSlowTransitionBackupAttemptMovesTheIssueOnceWithRealDex proves transitionIssue is safe under async
// durability: a backup attempt reads the issue first and finds the transition already made.
func TestSlowTransitionBackupAttemptMovesTheIssueOnceWithRealDex(t *testing.T) {
	provider, flow, harness := newJiraIntegrationHarness(t, integrationCloudID, slowRequestTimeout)
	ctx := integrationContext(t)

	summary := markerSlowTransition + " Sprinkler inspection"
	triage := runTriageFlow(t, ctx, harness, flow, "slow-transition", triageInput(summary))
	require.Equal(t, PhaseTriaged, triage.Phase)
	require.Equal(t, integrationDestination, triage.Status.Name)
	issueKey := provider.issueKeyForSummary(summary)
	require.Equal(t, 1, provider.transitionPostCount(issueKey), "Jira saw one transition request")
	t.Logf("slow transition: transition reads=%d posts=%d", provider.transitionReadCount(issueKey), provider.transitionPostCount(issueKey))
}

func TestUnavailableTransitionCompletesWithTheOfferedTransitionsWithRealDex(t *testing.T) {
	provider, flow, harness := newJiraIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	input := triageInput("Exit sign dark")
	input.DestinationStatusName = "Blocked"
	triage := runTriageFlow(t, ctx, harness, flow, "unavailable", input)
	require.Equal(t, PhaseTransitionUnavailable, triage.Phase)
	require.Equal(t, "To Do", triage.Status.Name)
	require.Equal(t, []string{"Start Progress -> In Progress", "Done -> Done"}, triage.AvailableTransitionNames)
	require.Zero(t, provider.transitionPostCount(triage.Issue.Key))
}

func TestCommentTimeoutIsRecordedAndNeverResentWithRealDex(t *testing.T) {
	provider, flow, harness := newJiraIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	summary := markerCommentTimeout + " Freight door stuck"
	triage := runTriageFlow(t, ctx, harness, flow, "comment-timeout", triageInput(summary))
	require.Equal(t, PhaseTriaged, triage.Phase)
	require.True(t, triage.IsCommentOutcomeUnknown)
	require.Empty(t, triage.CommentID)
	issueKey := provider.issueKeyForSummary(summary)
	require.Equal(t, 1, provider.commentCount(issueKey), "an uncertain comment is never re-sent")
	require.Equal(t, integrationDestination, triage.Status.Name)
}

func TestBlankCloudIDUsesTheOnlyGrantedJiraSiteWithRealDex(t *testing.T) {
	provider, flow, harness := newJiraIntegrationHarness(t, "", shortRequestTimeout)
	ctx := integrationContext(t)

	triage := runTriageFlow(t, ctx, harness, flow, "blank-site", triageInput("Loading dock light out"))
	require.Equal(t, PhaseTriaged, triage.Phase)
	require.Equal(t, 1, provider.accessibleResourcesCount(), "the Worker's client caches the resolved site")
}

func triageInput(summary string) Input {
	return Input{
		Summary: summary, Description: "Panel B trips at 02:00.\nBreaker is warm.", ProjectKey: "OPS",
		Labels: []string{"facilities"}, TriageComment: "Paging the on-call electrician.", DestinationStatusName: integrationDestination,
	}
}

func runTriageFlow(t *testing.T, ctx context.Context, harness *jiraIntegrationHarness, flow *Flow, scenario string, input Input) IssueTriage {
	t.Helper()
	flowID := startTriageFlow(t, ctx, harness, flow, scenario, input)
	return waitForFlowOutput(t, ctx, harness, flowID)
}

func startTriageFlow(t *testing.T, ctx context.Context, harness *jiraIntegrationHarness, flow *Flow, scenario string, input Input) string {
	t.Helper()
	flowID := "jira-issue-triage-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

func waitForFlowOutput(t *testing.T, ctx context.Context, harness *jiraIntegrationHarness, flowID string) IssueTriage {
	t.Helper()
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s", flowID)
	var triage IssueTriage
	require.NoError(t, result.DecodeSingleOutput(&triage))
	return triage
}

func waitForPhase(t *testing.T, ctx context.Context, harness *jiraIntegrationHarness, flow *Flow, flowID string, phase string) IssueTriage {
	t.Helper()
	return waitForTriage(t, ctx, harness, flow, flowID, func(triage IssueTriage) bool { return triage.Phase == phase })
}

func waitForReconciliationNote(t *testing.T, ctx context.Context, harness *jiraIntegrationHarness, flow *Flow, flowID string, note string) IssueTriage {
	t.Helper()
	return waitForTriage(t, ctx, harness, flow, flowID, func(triage IssueTriage) bool {
		return triage.Phase == PhaseNeedsReconciliation && triage.ReconciliationNote == note
	})
}

func waitForTriage(t *testing.T, ctx context.Context, harness *jiraIntegrationHarness, flow *Flow, flowID string, isExpected func(IssueTriage) bool) IssueTriage {
	t.Helper()
	var triage IssueTriage
	require.Eventually(t, func() bool {
		triage = IssueTriage{}
		return harness.client.InvokeRPC(ctx, flowID, flow.GetIssueTriage, nil, &triage) == nil && isExpected(triage)
	}, 30*time.Second, 100*time.Millisecond, "Flow %s last triage %+v", flowID, triage)
	return triage
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// fakeJira is a credential-safe, stateful Jira fake. A marker in the summary selects a failure.
type fakeJira struct {
	*httptest.Server
	t                 *testing.T
	mutex             sync.Mutex
	nextIssueNumber   int
	issues            map[string]*fakeIssue
	createTimes       map[string][]time.Time
	commentCounts     map[string]int
	transitionPosts   map[string]int
	transitionReads   map[string]int
	searches          []string
	resourcesRequests int
	nextCommentNumber int
}

type fakeIssue struct {
	number    int
	summary   string
	statusID  string
	isIndexed bool
}

var (
	fakeStatuses = map[string][2]string{"1": {"To Do", "new"}, "3": {"In Progress", "indeterminate"}, "5": {"Done", "done"}}
	// fakeWorkflow maps a source status to its transitions: ID, name, destination.
	fakeWorkflow = map[string][][3]string{
		"1": {{"11", "Start Progress", "3"}, {"31", "Done", "5"}},
		"3": {{"21", "Stop Progress", "1"}, {"31", "Done", "5"}},
		"5": {{"51", "Reopen", "1"}},
	}
	summaryPhrasePattern = regexp.MustCompile(`summary ~ "\\"(.*)\\""`)
	issuePathPattern     = regexp.MustCompile(`^/ex/jira/` + integrationCloudID + `/rest/api/3/issue/([A-Za-z0-9-]+)(/comment|/transitions)?$`)
)

func newFakeJira(t *testing.T) *fakeJira {
	t.Helper()
	provider := &fakeJira{
		t: t, nextIssueNumber: 4, nextCommentNumber: 10500,
		issues: map[string]*fakeIssue{
			decoyNearDuplicateKey: {number: 1, summary: nearDuplicateSummary, statusID: "1", isIndexed: true},
			openDuplicateKey:      {number: 2, summary: openDuplicateSummary, statusID: "1", isIndexed: true},
			"OPS-3":               {number: 3, summary: openDuplicateSummary, statusID: "5", isIndexed: true},
		},
		createTimes: map[string][]time.Time{}, commentCounts: map[string]int{}, transitionPosts: map[string]int{}, transitionReads: map[string]int{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeJira) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+integrationAccessToken {
		provider.writeJSON(response, http.StatusUnauthorized, `{"code":401,"message":"SENTINEL Unauthorized"}`)
		return
	}
	sitePrefix := "/ex/jira/" + integrationCloudID + "/rest/api/3"
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/oauth/token/accessible-resources":
		provider.mutex.Lock()
		provider.resourcesRequests++
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusOK, `[{"id":"`+integrationCloudID+`","name":"Ops","url":"https://ops.atlassian.net","scopes":["read:jira-work","write:jira-work"]}]`)
	case request.Method == http.MethodPost && request.URL.Path == sitePrefix+"/search/jql":
		provider.searchIssues(response, request)
	case request.Method == http.MethodPost && request.URL.Path == sitePrefix+"/issue":
		provider.createIssue(response, request)
	default:
		match := issuePathPattern.FindStringSubmatch(request.URL.Path)
		if match == nil {
			provider.writeJSON(response, http.StatusNotFound, `{"errorMessages":["SENTINEL no route"]}`)
			return
		}
		provider.serveIssue(response, request, strings.ToUpper(match[1]), match[2])
	}
}

func (provider *fakeJira) searchIssues(response http.ResponseWriter, request *http.Request) {
	var body struct {
		JQL string `json:"jql"`
	}
	require.NoError(provider.t, json.NewDecoder(request.Body).Decode(&body))
	phrase := ""
	if match := summaryPhrasePattern.FindStringSubmatch(body.JQL); match != nil {
		phrase = strings.ToLower(match[1])
	}
	provider.mutex.Lock()
	provider.searches = append(provider.searches, body.JQL)
	var issues []string
	for key, issue := range provider.issues {
		// Search is eventually consistent: a just-created issue is not indexed yet.
		if issue.isIndexed && fakeStatuses[issue.statusID][1] != "done" && strings.Contains(strings.ToLower(issue.summary), phrase) {
			issues = append(issues, provider.issueJSON(key, issue, false))
		}
	}
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, `{"issues":[`+strings.Join(issues, ",")+`],"isLast":true}`)
}

func (provider *fakeJira) createIssue(response http.ResponseWriter, request *http.Request) {
	var body struct {
		Fields struct {
			Project struct {
				Key string `json:"key"`
			} `json:"project"`
			Summary     string          `json:"summary"`
			Description json.RawMessage `json:"description"`
		} `json:"fields"`
	}
	require.NoError(provider.t, json.NewDecoder(request.Body).Decode(&body))
	summary := body.Fields.Summary
	if body.Fields.Project.Key != "OPS" {
		provider.writeJSON(response, http.StatusBadRequest, `{"errorMessages":[],"errors":{"project":"SENTINEL valid project is required"}}`)
		return
	}
	provider.mutex.Lock()
	provider.createTimes[summary] = append(provider.createTimes[summary], time.Now())
	attempt := len(provider.createTimes[summary])
	provider.mutex.Unlock()
	switch {
	case strings.Contains(summary, markerReject):
		provider.writeJSON(response, http.StatusBadRequest, `{"errorMessages":[],"errors":{"summary":"SENTINEL summary is invalid"}}`)
	case strings.Contains(summary, markerRateLimit) && attempt == 1:
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"errorMessages":["SENTINEL rate limited"]}`)
	case strings.Contains(summary, markerServerError) && attempt == 1:
		provider.writeJSON(response, http.StatusInternalServerError, `{"errorMessages":["SENTINEL internal error"]}`)
	case strings.Contains(summary, markerCreateTimeout):
		provider.storeIssue(summary)
		// Jira created the issue, but the response never arrives before the connector gives up.
		<-request.Context().Done()
	case strings.Contains(summary, markerSlowCreate):
		key := provider.storeIssue(summary)
		time.Sleep(slowProviderDelay)
		provider.writeJSON(response, http.StatusCreated, provider.createdJSON(key))
	default:
		provider.writeJSON(response, http.StatusCreated, provider.createdJSON(provider.storeIssue(summary)))
	}
}

func (provider *fakeJira) serveIssue(response http.ResponseWriter, request *http.Request, key string, subresource string) {
	provider.mutex.Lock()
	issue, exists := provider.issues[key]
	provider.mutex.Unlock()
	if !exists {
		provider.writeJSON(response, http.StatusNotFound, `{"errorMessages":["SENTINEL Issue does not exist or you do not have permission to see it."],"errors":{}}`)
		return
	}
	switch {
	case request.Method == http.MethodGet && subresource == "":
		provider.mutex.Lock()
		includesTransitions := request.URL.Query().Get("expand") == "transitions"
		if includesTransitions {
			provider.transitionReads[key]++
		}
		body := provider.issueJSON(key, issue, includesTransitions)
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusOK, body)
	case request.Method == http.MethodPost && subresource == "/comment":
		provider.mutex.Lock()
		provider.commentCounts[key]++
		provider.nextCommentNumber++
		commentID := provider.nextCommentNumber
		provider.mutex.Unlock()
		if strings.Contains(issue.summary, markerCommentTimeout) {
			// Reading the body lets the server notice the client's disconnect and cancel the context.
			_, err := io.Copy(io.Discard, request.Body)
			require.NoError(provider.t, err)
			<-request.Context().Done()
			return
		}
		provider.writeJSON(response, http.StatusCreated, fmt.Sprintf(`{"id":"%d","created":"2026-09-30T10:05:00.000-0700","author":{"accountId":"5b10ac8d82e05b22cc7d4ef5"}}`, commentID))
	case request.Method == http.MethodPost && subresource == "/transitions":
		provider.transitionIssue(response, request, key, issue)
	default:
		provider.writeJSON(response, http.StatusMethodNotAllowed, `{}`)
	}
}

func (provider *fakeJira) transitionIssue(response http.ResponseWriter, request *http.Request, key string, issue *fakeIssue) {
	var body struct {
		Transition struct {
			ID string `json:"id"`
		} `json:"transition"`
	}
	require.NoError(provider.t, json.NewDecoder(request.Body).Decode(&body))
	provider.mutex.Lock()
	provider.transitionPosts[key]++
	destination := ""
	for _, transition := range fakeWorkflow[issue.statusID] {
		if transition[0] == body.Transition.ID {
			destination = transition[2]
		}
	}
	if destination != "" {
		issue.statusID = destination
	}
	provider.mutex.Unlock()
	if destination == "" {
		provider.writeJSON(response, http.StatusBadRequest, `{"errorMessages":["SENTINEL Transition is not valid for this issue."],"errors":{}}`)
		return
	}
	if strings.Contains(issue.summary, markerSlowTransition) {
		// Jira moved the issue, but the response arrives after Dex's async local phase ends.
		time.Sleep(slowProviderDelay)
	}
	response.WriteHeader(http.StatusNoContent)
}

func (provider *fakeJira) storeIssue(summary string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	number := provider.nextIssueNumber
	provider.nextIssueNumber++
	key := "OPS-" + strconv.Itoa(number)
	provider.issues[key] = &fakeIssue{number: number, summary: summary, statusID: "1"}
	return key
}

// issueJSON renders an issue; the caller holds the mutex.
func (provider *fakeJira) issueJSON(key string, issue *fakeIssue, includesTransitions bool) string {
	status := fakeStatuses[issue.statusID]
	fields := fmt.Sprintf(`"summary":%q,"status":{"id":%q,"name":%q,"statusCategory":{"key":%q}},`+
		`"issuetype":{"id":"10001","name":"Task"},"project":{"id":"10000","key":"OPS","name":"Operations"},`+
		`"labels":["facilities"],"created":"2026-09-30T09:15:00.000-0700","updated":"2026-09-30T09:15:00.000-0700",`+
		`"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[`+
		`{"type":"text","text":"Panel B trips at 02:00."},{"type":"hardBreak"},{"type":"text","text":"Breaker is warm."}]}]}`,
		issue.summary, issue.statusID, status[0], status[1])
	transitions := ""
	if includesTransitions {
		var rendered []string
		for _, transition := range fakeWorkflow[issue.statusID] {
			destination := fakeStatuses[transition[2]]
			rendered = append(rendered, fmt.Sprintf(`{"id":%q,"name":%q,"to":{"id":%q,"name":%q,"statusCategory":{"key":%q}}}`,
				transition[0], transition[1], transition[2], destination[0], destination[1]))
		}
		transitions = `,"transitions":[` + strings.Join(rendered, ",") + `]`
	}
	return fmt.Sprintf(`{"id":"%d","key":%q,"fields":{%s}%s}`, 10000+issue.number, key, fields, transitions)
}

func (provider *fakeJira) createdJSON(key string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return fmt.Sprintf(`{"id":"%d","key":%q,"self":"https://api.atlassian.com/ex/jira/%s/rest/api/3/issue/%s"}`,
		10000+provider.issues[key].number, key, integrationCloudID, key)
}

func (provider *fakeJira) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		provider.t.Logf("fake Jira response write failed: %v", err)
	}
}

func (provider *fakeJira) createCount(summary string) int {
	return len(provider.createTimesFor(summary))
}

func (provider *fakeJira) createTimesFor(summary string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]time.Time(nil), provider.createTimes[summary]...)
}

func (provider *fakeJira) commentCount(key string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.commentCounts[key]
}

func (provider *fakeJira) transitionPostCount(key string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.transitionPosts[key]
}

func (provider *fakeJira) transitionReadCount(key string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.transitionReads[key]
}

func (provider *fakeJira) searchQueries() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.searches...)
}

func (provider *fakeJira) accessibleResourcesCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.resourcesRequests
}

func (provider *fakeJira) issueKeyForSummary(summary string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	for key, issue := range provider.issues {
		if issue.summary == summary {
			return key
		}
	}
	provider.t.Fatalf("fake Jira has no issue with summary %q", summary)
	return ""
}

type jiraIntegrationHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newJiraIntegrationHarness(t *testing.T, cloudID string, requestTimeout time.Duration) (*fakeJira, *Flow, *jiraIntegrationHarness) {
	t.Helper()
	provider := newFakeJira(t)
	reference := sdkgo.ConnectionRef{Provider: "atlassian", Name: ConnectionName}
	providerClient, err := jira.New(
		jira.Config{CloudID: cloudID, Endpoint: provider.URL},
		sdkgo.StaticCredentialProvider[jira.Credentials]{reference: {
			OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
			AccessToken: sdkgo.NewSecretString(integrationAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token"),
		}},
		jira.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := jira.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection, ProjectSelection{})
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &jiraIntegrationHarness{
		registry: registry, cache: cache, workerAddress: workerAddress,
		serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: harness.serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(stopCtx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return provider, flow, harness
}

func availableIntegrationPort(t *testing.T) string {
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
