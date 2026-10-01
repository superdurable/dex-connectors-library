//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvedrequesttask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAccessToken = "integration-access-token"
	integrationProjectID   = "1201000000000001"
	intakeSectionID        = "1201000000000100"
	approvedSectionID      = "1201000000000101"
	adaUserID              = "1200000000000042"
	adaEmail               = "ada@example.com"
	unknownAssigneeEmail   = "nobody@example.com"
	shortRequestTimeout    = 500 * time.Millisecond
	// slowRequestTimeout outlasts the fake's nine-second responses and Dex's roughly seven-second async local phase.
	slowRequestTimeout = 20 * time.Second
	slowProviderDelay  = 9 * time.Second
	fakePageSize       = 2

	markerReject         = "[reject]"
	markerRateLimit      = "[rate-limit]"
	markerServerError    = "[server-error]"
	markerCreateTimeout  = "[create-timeout]"
	markerSlowCreate     = "[slow-create]"
	markerSlowUpdate     = "[slow-update]"
	markerSlowComment    = "[slow-comment]"
	markerCommentTimeout = "[comment-timeout]"

	decoyPrefixTaskID   = "1204000000001001"
	existingTaskID      = "1204000000001004"
	existingRequestID   = "REQ-2001"
	firstCreatedTaskNum = 1204000000002000
)

var taskPathPattern = regexp.MustCompile(`^/tasks/([0-9]+)(/stories)?$`)
var sectionPathPattern = regexp.MustCompile(`^/sections/([0-9]+)/addTask$`)

func TestNewRequestIsCreatedAssignedAndCommentedOnceWithRealDex(t *testing.T) {
	provider, flow, harness := newAsanaIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	record := runRequestFlow(t, ctx, harness, flow, "created", requestInput("REQ-1042", "Replace badge reader"))
	require.Equal(t, PhaseReady, record.Phase)
	require.False(t, record.IsExistingTaskReused, "neither [REQ-10420] nor a completed [REQ-1042] task is the same open request")
	require.Equal(t, 2, record.OpenTaskPagesRead, "the duplicate check followed Asana's offset to the second page")
	require.Equal(t, 1, record.CreateAttempts)
	require.NotEmpty(t, record.CommentID)
	require.False(t, record.IsCommentOutcomeUnknown)
	require.Equal(t, &asana.UserReference{ID: adaUserID, Name: "Ada Lovelace"}, record.Task.Assignee)
	require.Equal(t, approvedSectionID, record.Task.SectionID(integrationProjectID))
	require.Equal(t, "2026-10-15", record.Task.DueOn)

	taskName := "[REQ-1042] Replace badge reader"
	taskID := provider.taskIDForName(taskName)
	require.Equal(t, 1, provider.createCount(taskName))
	require.Equal(t, 1, provider.updateCount(taskID))
	require.Equal(t, 1, provider.sectionMoveCount(taskID))
	require.Equal(t, []string{"Approved by Grace Hopper.\n\nBudget code FAC-7."}, provider.commentTexts(taskID))
	queries := provider.listQueries()
	require.Len(t, queries, 2)
	require.Equal(t, "now", queries[0].Get("completed_since"))
	require.Equal(t, "100", queries[0].Get("limit"))
	require.Empty(t, queries[0].Get("offset"))
	require.Equal(t, "offset-2", queries[1].Get("offset"))
}

func TestOpenTaskWithTheRequestIDIsReusedWithRealDex(t *testing.T) {
	provider, flow, harness := newAsanaIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	record := runRequestFlow(t, ctx, harness, flow, "reused", requestInput(existingRequestID, "Badge reader offline"))
	require.Equal(t, PhaseReady, record.Phase)
	require.True(t, record.IsExistingTaskReused)
	require.Equal(t, existingTaskID, record.TaskID)
	require.Zero(t, record.CreateAttempts)
	require.Zero(t, provider.totalCreateCount())
	require.Equal(t, approvedSectionID, record.Task.SectionID(integrationProjectID), "assignment moved the reused task out of intake")
	require.Equal(t, 1, len(provider.commentTexts(existingTaskID)))
}

func TestRejectedCreateCompletesAsRejectedWithRealDex(t *testing.T) {
	provider, flow, harness := newAsanaIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	record := runRequestFlow(t, ctx, harness, flow, "rejected", requestInput("REQ-3001", markerReject+" Paint the lobby"))
	require.Equal(t, PhaseRejected, record.Phase)
	require.Empty(t, record.TaskID)
	require.Equal(t, 1, provider.createCount("[REQ-3001] "+markerReject+" Paint the lobby"))
}

func TestRateLimitedCreateIsRetriedAfterRetryAfterWithRealDex(t *testing.T) {
	provider, flow, harness := newAsanaIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	record := runRequestFlow(t, ctx, harness, flow, "rate-limited", requestInput("REQ-3002", markerRateLimit+" Door sensor noisy"))
	require.Equal(t, PhaseReady, record.Phase)
	require.Equal(t, 1, record.CreateAttempts, "a 429 created nothing, so Dex retries the same Step execution")
	attempts := provider.createTimesFor("[REQ-3002] " + markerRateLimit + " Door sensor noisy")
	require.Len(t, attempts, 2)
	require.GreaterOrEqual(t, attempts[1].Sub(attempts[0]), time.Second, "the retry waits for Retry-After")
}

func TestTimeoutAfterDispatchIsReconciledWithoutCreatingAgainWithRealDex(t *testing.T) {
	provider, flow, harness := newAsanaIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)
	taskName := "[REQ-3003] " + markerCreateTimeout + " Lift jammed"
	flowID := startRequestFlow(t, ctx, harness, flow, "timeout", requestInput("REQ-3003", markerCreateTimeout+" Lift jammed"))

	uncertain := waitForPhase(t, ctx, harness, flow, flowID, PhaseNeedsReconciliation)
	require.NotNil(t, uncertain.UncertainCreate)
	require.Equal(t, sdkgo.FailureTransport, uncertain.UncertainCreate.FailureKind)
	require.NotEmpty(t, uncertain.UncertainCreate.CallID)
	require.Equal(t, 1, provider.createCount(taskName), "a timeout after dispatch is never retried")

	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ConfirmCreatedTask, ConfirmCreatedTaskInput{TaskID: "1204999999999999"}, nil))
	waitForReconciliationNote(t, ctx, harness, flow, flowID, NoteReportedTaskMissing)
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ConfirmCreatedTask, ConfirmCreatedTaskInput{TaskID: decoyPrefixTaskID}, nil))
	waitForReconciliationNote(t, ctx, harness, flow, flowID, NoteReportedTaskMismatch)

	createdID := provider.taskIDForName(taskName)
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ConfirmCreatedTask, ConfirmCreatedTaskInput{TaskID: " " + createdID + " "}, nil))
	record := waitForFlowOutput(t, ctx, harness, flowID)
	require.Equal(t, PhaseReady, record.Phase)
	require.Equal(t, createdID, record.TaskID)
	require.Nil(t, record.UncertainCreate)
	require.Equal(t, 1, record.CreateAttempts)
	require.Equal(t, 1, provider.createCount(taskName), "reconciliation adopted the existing task")
}

func TestServerErrorIsCreatedAgainOnlyAfterOperatorApprovalWithRealDex(t *testing.T) {
	provider, flow, harness := newAsanaIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)
	taskName := "[REQ-3004] " + markerServerError + " HVAC alarm"
	flowID := startRequestFlow(t, ctx, harness, flow, "server-error", requestInput("REQ-3004", markerServerError+" HVAC alarm"))

	uncertain := waitForPhase(t, ctx, harness, flow, flowID, PhaseNeedsReconciliation)
	require.Equal(t, sdkgo.FailureAvailability, uncertain.UncertainCreate.FailureKind)
	require.Equal(t, 1, provider.createCount(taskName))

	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ApproveTaskCreateRetry, nil, nil))
	record := waitForFlowOutput(t, ctx, harness, flowID)
	require.Equal(t, PhaseReady, record.Phase)
	require.Equal(t, 2, record.CreateAttempts)
	require.Equal(t, 2, provider.createCount(taskName), "only the approved retry sent a second create")
}

// TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex guards sync durability: an async fallback attempt
// would send a create that outlasts Dex's roughly seven-second local phase a second time.
func TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex(t *testing.T) {
	provider, flow, harness := newAsanaIntegrationHarness(t, slowRequestTimeout)
	ctx := integrationContext(t)

	record := runRequestFlow(t, ctx, harness, flow, "slow-create", requestInput("REQ-3005", markerSlowCreate+" Generator test overdue"))
	require.Equal(t, PhaseReady, record.Phase)
	require.Equal(t, 1, provider.createCount("[REQ-3005] "+markerSlowCreate+" Generator test overdue"), "one Step execution dispatches one create request")
}

// TestSlowUpdateBackupAttemptLeavesTheSameTaskWithRealDex proves updateTask is safe under async durability:
// a backup attempt resends the same absolute section move and field values, and the task ends the same.
func TestSlowUpdateBackupAttemptLeavesTheSameTaskWithRealDex(t *testing.T) {
	provider, flow, harness := newAsanaIntegrationHarness(t, slowRequestTimeout)
	ctx := integrationContext(t)

	taskName := "[REQ-3006] " + markerSlowUpdate + " Sprinkler inspection"
	record := runRequestFlow(t, ctx, harness, flow, "slow-update", requestInput("REQ-3006", markerSlowUpdate+" Sprinkler inspection"))
	require.Equal(t, PhaseReady, record.Phase)
	taskID := provider.taskIDForName(taskName)
	require.Equal(t, 1, provider.createCount(taskName))
	require.Equal(t, 1, len(provider.commentTexts(taskID)), "the Flow moved on exactly once")
	bodies := provider.updateBodies(taskID)
	require.GreaterOrEqual(t, len(bodies), 2, "Dex dispatched a backup attempt while the first nine-second update was in flight")
	for _, body := range bodies {
		require.JSONEq(t, bodies[0], body, "every attempt sends the same absolute values")
	}
	finalTask := provider.snapshot(taskID)
	require.Equal(t, adaUserID, finalTask.assigneeID)
	require.Equal(t, approvedSectionID, finalTask.sectionID)
	require.Equal(t, "2026-10-15", finalTask.dueOn)
	t.Logf("slow update: section moves=%d updates=%d", provider.sectionMoveCount(taskID), len(bodies))
}

// TestSlowCommentIsSentOnceUnderSyncDurabilityWithRealDex guards addComment's sync durability.
func TestSlowCommentIsSentOnceUnderSyncDurabilityWithRealDex(t *testing.T) {
	provider, flow, harness := newAsanaIntegrationHarness(t, slowRequestTimeout)
	ctx := integrationContext(t)

	taskName := "[REQ-3007] " + markerSlowComment + " Exit sign dark"
	record := runRequestFlow(t, ctx, harness, flow, "slow-comment", requestInput("REQ-3007", markerSlowComment+" Exit sign dark"))
	require.Equal(t, PhaseReady, record.Phase)
	require.NotEmpty(t, record.CommentID)
	require.Equal(t, 1, len(provider.commentTexts(provider.taskIDForName(taskName))), "one Step execution dispatches one comment request")
}

func TestCommentTimeoutIsRecordedAndNeverResentWithRealDex(t *testing.T) {
	provider, flow, harness := newAsanaIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	taskName := "[REQ-3008] " + markerCommentTimeout + " Freight door stuck"
	record := runRequestFlow(t, ctx, harness, flow, "comment-timeout", requestInput("REQ-3008", markerCommentTimeout+" Freight door stuck"))
	require.Equal(t, PhaseReady, record.Phase)
	require.True(t, record.IsCommentOutcomeUnknown)
	require.Empty(t, record.CommentID)
	require.Equal(t, 1, len(provider.commentTexts(provider.taskIDForName(taskName))), "an uncertain comment is never re-sent")
}

func TestRejectedAssigneeCompletesWithTheCreatedTaskWithRealDex(t *testing.T) {
	provider, flow, harness := newAsanaIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	input := requestInput("REQ-3009", "Loading dock light out")
	input.AssigneeID = unknownAssigneeEmail
	record := runRequestFlow(t, ctx, harness, flow, "rejected-assignee", input)
	require.Equal(t, PhaseAssignmentRejected, record.Phase)
	taskID := provider.taskIDForName("[REQ-3009] Loading dock light out")
	require.Equal(t, taskID, record.TaskID)
	require.Empty(t, provider.commentTexts(taskID), "no approval comment on a task that could not be assigned")
}

func requestInput(requestID string, title string) Input {
	return Input{
		RequestID: requestID, Title: title, Details: "Badge reader at door 4 is offline.\nFacilities approved the spend.",
		ApprovedBy: "Grace Hopper", ApprovalNote: "Budget code FAC-7.", AssigneeID: adaEmail, DueOn: "2026-10-15",
		ProjectID: integrationProjectID, SectionID: approvedSectionID,
	}
}

func runRequestFlow(t *testing.T, ctx context.Context, harness *asanaIntegrationHarness, flow *Flow, scenario string, input Input) RequestTask {
	t.Helper()
	flowID := startRequestFlow(t, ctx, harness, flow, scenario, input)
	return waitForFlowOutput(t, ctx, harness, flowID)
}

func startRequestFlow(t *testing.T, ctx context.Context, harness *asanaIntegrationHarness, flow *Flow, scenario string, input Input) string {
	t.Helper()
	flowID := "asana-request-task-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

func waitForFlowOutput(t *testing.T, ctx context.Context, harness *asanaIntegrationHarness, flowID string) RequestTask {
	t.Helper()
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s", flowID)
	var record RequestTask
	require.NoError(t, result.DecodeSingleOutput(&record))
	return record
}

func waitForPhase(t *testing.T, ctx context.Context, harness *asanaIntegrationHarness, flow *Flow, flowID string, phase string) RequestTask {
	t.Helper()
	return waitForRecord(t, ctx, harness, flow, flowID, func(record RequestTask) bool { return record.Phase == phase })
}

func waitForReconciliationNote(t *testing.T, ctx context.Context, harness *asanaIntegrationHarness, flow *Flow, flowID string, note string) RequestTask {
	t.Helper()
	return waitForRecord(t, ctx, harness, flow, flowID, func(record RequestTask) bool {
		return record.Phase == PhaseNeedsReconciliation && record.ReconciliationNote == note
	})
}

func waitForRecord(t *testing.T, ctx context.Context, harness *asanaIntegrationHarness, flow *Flow, flowID string, isExpected func(RequestTask) bool) RequestTask {
	t.Helper()
	var record RequestTask
	require.Eventually(t, func() bool {
		record = RequestTask{}
		return harness.client.InvokeRPC(ctx, flowID, flow.GetRequestTask, nil, &record) == nil && isExpected(record)
	}, 30*time.Second, 100*time.Millisecond, "Flow %s last record %+v", flowID, record)
	return record
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// fakeAsana is a credential-safe, stateful Asana fake. A marker in the task name selects a failure.
type fakeAsana struct {
	*httptest.Server
	t                *testing.T
	mutex            sync.Mutex
	nextTaskNumber   int64
	nextStoryNumber  int64
	tasks            map[string]*fakeTask
	createTimes      map[string][]time.Time
	updateBodiesByID map[string][]string
	sectionMoves     map[string]int
	comments         map[string][]string
	queries          []url.Values
}

type fakeTask struct {
	name        string
	projectID   string
	sectionID   string
	assigneeID  string
	dueOn       string
	isCompleted bool
}

var fakeSections = map[string]string{intakeSectionID: "Intake", approvedSectionID: "Approved"}

func newFakeAsana(t *testing.T) *fakeAsana {
	t.Helper()
	provider := &fakeAsana{
		t: t, nextTaskNumber: firstCreatedTaskNum, nextStoryNumber: 1205000000000000,
		tasks: map[string]*fakeTask{
			decoyPrefixTaskID:  {name: "[REQ-10420] Replace badge reader", projectID: integrationProjectID, sectionID: intakeSectionID},
			"1204000000001002": {name: "Follow-up on REQ-1042", projectID: integrationProjectID, sectionID: intakeSectionID},
			"1204000000001003": {name: "[REQ-1042] Replace badge reader", projectID: integrationProjectID, sectionID: approvedSectionID, isCompleted: true},
			existingTaskID:     {name: "[" + existingRequestID + "] Badge reader offline", projectID: integrationProjectID, sectionID: intakeSectionID},
		},
		createTimes: map[string][]time.Time{}, updateBodiesByID: map[string][]string{}, sectionMoves: map[string]int{}, comments: map[string][]string{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeAsana) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+integrationAccessToken {
		provider.writeJSON(response, http.StatusUnauthorized, `{"errors":[{"message":"SENTINEL Not Authorized"}]}`)
		return
	}
	contents, err := io.ReadAll(request.Body)
	require.NoError(provider.t, err)
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/tasks":
		provider.listTasks(response, request)
	case request.Method == http.MethodPost && request.URL.Path == "/tasks":
		provider.createTask(response, request, contents)
	case request.Method == http.MethodPost && sectionPathPattern.MatchString(request.URL.Path):
		provider.addTaskToSection(response, sectionPathPattern.FindStringSubmatch(request.URL.Path)[1], contents)
	default:
		match := taskPathPattern.FindStringSubmatch(request.URL.Path)
		if match == nil {
			provider.writeJSON(response, http.StatusNotFound, `{"errors":[{"message":"SENTINEL no route"}]}`)
			return
		}
		provider.serveTask(response, request, match[1], match[2] != "", contents)
	}
}

func (provider *fakeAsana) listTasks(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	provider.mutex.Lock()
	provider.queries = append(provider.queries, query)
	var taskIDs []string
	for taskID, task := range provider.tasks {
		if task.projectID == query.Get("project") && (query.Get("completed_since") != "now" || !task.isCompleted) {
			taskIDs = append(taskIDs, taskID)
		}
	}
	sort.Strings(taskIDs)
	start := 0
	if offset := query.Get("offset"); offset != "" {
		start, _ = strconv.Atoi(strings.TrimPrefix(offset, "offset-"))
	}
	end := min(start+fakePageSize, len(taskIDs))
	var rendered []string
	for _, taskID := range taskIDs[start:end] {
		rendered = append(rendered, provider.taskJSON(taskID))
	}
	nextPage := "null"
	if end < len(taskIDs) {
		nextPage = fmt.Sprintf(`{"offset":"offset-%d","path":"/tasks?offset=offset-%d","uri":"https://app.asana.com/api/1.0/tasks?offset=offset-%d"}`, end, end, end)
	}
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, `{"data":[`+strings.Join(rendered, ",")+`],"next_page":`+nextPage+`}`)
}

func (provider *fakeAsana) createTask(response http.ResponseWriter, request *http.Request, contents []byte) {
	var body struct {
		Data struct {
			Name        string   `json:"name"`
			Projects    []string `json:"projects"`
			Memberships []struct {
				Project string `json:"project"`
				Section string `json:"section"`
			} `json:"memberships"`
			DueOn string `json:"due_on"`
		} `json:"data"`
	}
	require.NoError(provider.t, json.Unmarshal(contents, &body))
	name := body.Data.Name
	require.Len(provider.t, body.Data.Memberships, 1, "the request section is set at creation")
	provider.mutex.Lock()
	provider.createTimes[name] = append(provider.createTimes[name], time.Now())
	attempt := len(provider.createTimes[name])
	provider.mutex.Unlock()
	membership := body.Data.Memberships[0]
	switch {
	case strings.Contains(name, markerReject):
		provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"SENTINEL name: invalid"}]}`)
	case strings.Contains(name, markerRateLimit) && attempt == 1:
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"errors":[{"message":"SENTINEL You have made too many requests recently."}]}`)
	case strings.Contains(name, markerServerError) && attempt == 1:
		provider.writeJSON(response, http.StatusInternalServerError, `{"errors":[{"message":"SENTINEL","phrase":"6 sad squid snuggle softly"}]}`)
	case strings.Contains(name, markerCreateTimeout):
		provider.storeTask(name, membership.Project, membership.Section, body.Data.DueOn)
		// Asana created the task, but the response never arrives before the connector gives up.
		<-request.Context().Done()
	case strings.Contains(name, markerSlowCreate):
		taskID := provider.storeTask(name, membership.Project, membership.Section, body.Data.DueOn)
		time.Sleep(slowProviderDelay)
		provider.writeJSON(response, http.StatusCreated, provider.createdJSON(taskID))
	default:
		provider.writeJSON(response, http.StatusCreated, provider.createdJSON(provider.storeTask(name, membership.Project, membership.Section, body.Data.DueOn)))
	}
}

func (provider *fakeAsana) addTaskToSection(response http.ResponseWriter, sectionID string, contents []byte) {
	var body struct {
		Data struct {
			Task string `json:"task"`
		} `json:"data"`
	}
	require.NoError(provider.t, json.Unmarshal(contents, &body))
	provider.mutex.Lock()
	task, isKnownTask := provider.tasks[body.Data.Task]
	_, isKnownSection := fakeSections[sectionID]
	if isKnownTask && isKnownSection {
		task.sectionID = sectionID
		provider.sectionMoves[body.Data.Task]++
	}
	provider.mutex.Unlock()
	if !isKnownTask || !isKnownSection {
		provider.writeJSON(response, http.StatusNotFound, `{"errors":[{"message":"SENTINEL Unknown object"}]}`)
		return
	}
	provider.writeJSON(response, http.StatusOK, `{"data":{}}`)
}

func (provider *fakeAsana) serveTask(response http.ResponseWriter, request *http.Request, taskID string, isStories bool, contents []byte) {
	provider.mutex.Lock()
	task, exists := provider.tasks[taskID]
	provider.mutex.Unlock()
	if !exists {
		provider.writeJSON(response, http.StatusNotFound, `{"errors":[{"message":"SENTINEL task: Unknown object"}]}`)
		return
	}
	switch {
	case request.Method == http.MethodGet && !isStories:
		provider.mutex.Lock()
		body := provider.taskJSON(taskID)
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusOK, `{"data":`+body+`}`)
	case request.Method == http.MethodPut && !isStories:
		provider.updateTask(response, taskID, task, contents)
	case request.Method == http.MethodPost && isStories:
		provider.addComment(response, request, taskID, task, contents)
	default:
		provider.writeJSON(response, http.StatusMethodNotAllowed, `{}`)
	}
}

func (provider *fakeAsana) updateTask(response http.ResponseWriter, taskID string, task *fakeTask, contents []byte) {
	var body struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(provider.t, json.Unmarshal(contents, &body))
	var assignee string
	if raw, isSet := body.Data["assignee"]; isSet {
		require.NoError(provider.t, json.Unmarshal(raw, &assignee))
	}
	if assignee == unknownAssigneeEmail {
		provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"assignee: SENTINEL Not a recognized ID: nobody@example.com"}]}`)
		return
	}
	provider.mutex.Lock()
	provider.updateBodiesByID[taskID] = append(provider.updateBodiesByID[taskID], string(contents))
	if assignee == adaEmail || assignee == adaUserID {
		task.assigneeID = adaUserID
	}
	if raw, isSet := body.Data["due_on"]; isSet {
		require.NoError(provider.t, json.Unmarshal(raw, &task.dueOn))
	}
	rendered := provider.taskJSON(taskID)
	provider.mutex.Unlock()
	if strings.Contains(task.name, markerSlowUpdate) {
		// Asana applied the update, but the response arrives after Dex's async local phase ends.
		time.Sleep(slowProviderDelay)
	}
	provider.writeJSON(response, http.StatusOK, `{"data":`+rendered+`}`)
}

func (provider *fakeAsana) addComment(response http.ResponseWriter, request *http.Request, taskID string, task *fakeTask, contents []byte) {
	var body struct {
		Data struct {
			Text string `json:"text"`
		} `json:"data"`
	}
	require.NoError(provider.t, json.Unmarshal(contents, &body))
	provider.mutex.Lock()
	provider.comments[taskID] = append(provider.comments[taskID], body.Data.Text)
	provider.nextStoryNumber++
	storyID := provider.nextStoryNumber
	provider.mutex.Unlock()
	switch {
	case strings.Contains(task.name, markerCommentTimeout):
		<-request.Context().Done()
		return
	case strings.Contains(task.name, markerSlowComment):
		time.Sleep(slowProviderDelay)
	}
	provider.writeJSON(response, http.StatusCreated, fmt.Sprintf(`{"data":{"gid":"%d","resource_type":"story","resource_subtype":"comment_added",`+
		`"created_at":"2026-09-30T16:20:00.000Z","created_by":{"gid":%q,"name":"Integration Bot"}}}`, storyID, adaUserID))
}

func (provider *fakeAsana) storeTask(name string, projectID string, sectionID string, dueOn string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.nextTaskNumber++
	taskID := strconv.FormatInt(provider.nextTaskNumber, 10)
	provider.tasks[taskID] = &fakeTask{name: name, projectID: projectID, sectionID: sectionID, dueOn: dueOn}
	return taskID
}

// taskJSON renders a task; the caller holds the mutex.
func (provider *fakeAsana) taskJSON(taskID string) string {
	task := provider.tasks[taskID]
	assignee := "null"
	if task.assigneeID != "" {
		assignee = fmt.Sprintf(`{"gid":%q,"resource_type":"user","name":"Ada Lovelace"}`, task.assigneeID)
	}
	dueOn := "null"
	if task.dueOn != "" {
		dueOn = strconv.Quote(task.dueOn)
	}
	return fmt.Sprintf(`{"gid":%q,"name":%q,"resource_subtype":"default_task","completed":%t,"assignee":%s,"due_on":%s,`+
		`"memberships":[{"project":{"gid":%q,"name":"Facilities"},"section":{"gid":%q,"name":%q}}],`+
		`"notes":"Badge reader at door 4 is offline.","permalink_url":"https://app.asana.com/0/%s/%s",`+
		`"created_at":"2026-09-30T16:15:00.000Z","modified_at":"2026-09-30T16:15:00.000Z"}`,
		taskID, task.name, task.isCompleted, assignee, dueOn, task.projectID, task.sectionID, fakeSections[task.sectionID], task.projectID, taskID)
}

func (provider *fakeAsana) createdJSON(taskID string) string {
	return fmt.Sprintf(`{"data":{"gid":%q,"name":"created","permalink_url":"https://app.asana.com/0/%s/%s","created_at":"2026-09-30T16:15:00.000Z"}}`,
		taskID, integrationProjectID, taskID)
}

func (provider *fakeAsana) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		provider.t.Logf("fake Asana response write failed: %v", err)
	}
}

func (provider *fakeAsana) createCount(name string) int { return len(provider.createTimesFor(name)) }

func (provider *fakeAsana) createTimesFor(name string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]time.Time(nil), provider.createTimes[name]...)
}

func (provider *fakeAsana) totalCreateCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, times := range provider.createTimes {
		total += len(times)
	}
	return total
}

func (provider *fakeAsana) updateCount(taskID string) int { return len(provider.updateBodies(taskID)) }

func (provider *fakeAsana) updateBodies(taskID string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.updateBodiesByID[taskID]...)
}

func (provider *fakeAsana) sectionMoveCount(taskID string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.sectionMoves[taskID]
}

func (provider *fakeAsana) commentTexts(taskID string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.comments[taskID]...)
}

func (provider *fakeAsana) listQueries() []url.Values {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]url.Values(nil), provider.queries...)
}

func (provider *fakeAsana) snapshot(taskID string) fakeTask {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return *provider.tasks[taskID]
}

// taskIDForName returns the newest task with name, so a completed decoy with the same name is skipped.
func (provider *fakeAsana) taskIDForName(name string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	newestTaskID := ""
	for taskID, task := range provider.tasks {
		if task.name == name && taskID > newestTaskID {
			newestTaskID = taskID
		}
	}
	if newestTaskID == "" {
		provider.t.Fatalf("fake Asana has no task named %q", name)
	}
	return newestTaskID
}

type asanaIntegrationHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newAsanaIntegrationHarness(t *testing.T, requestTimeout time.Duration) (*fakeAsana, *Flow, *asanaIntegrationHarness) {
	t.Helper()
	provider := newFakeAsana(t)
	reference := sdkgo.ConnectionRef{Provider: "asana", Name: ConnectionName}
	providerClient, err := asana.New(
		asana.Config{Endpoint: provider.URL},
		sdkgo.StaticCredentialProvider[asana.Credentials]{reference: {AccessToken: sdkgo.NewSecretString(integrationAccessToken)}},
		asana.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := asana.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection, ProjectSelection{})
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &asanaIntegrationHarness{
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
