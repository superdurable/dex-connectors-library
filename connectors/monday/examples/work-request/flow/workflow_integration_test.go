//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workrequest

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
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAPIToken = "mondayIntegrationToken0123456789abcdef"
	integrationBoardID  = "1234567890"
	otherBoardID        = "2234567890"
	integrationItemName = "Monthly Fire Drill Checklist - February"

	defaultRequestTimeout = 5 * time.Second
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowResponseDelay  = 9 * time.Second
	slowRequestTimeout = 20 * time.Second
)

func integrationInput() Input {
	return Input{
		BoardID: integrationBoardID, GroupID: "february", ItemName: integrationItemName,
		StatusColumnID: "status", StatusLabel: "Working on it", DueDateColumnID: "date4", DueDate: "2026-02-18",
		UpdateText: "Scheduled from the facilities request.",
	}
}

func TestNewRequestCreatesOneItemAndAddsOneUpdateWithRealDex(t *testing.T) {
	provider := newFakeMonday(t)
	harness := newWorkHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runWorkRequest(t, "new-request", integrationInput())
	require.Equal(t, WorkItemCreated, outcome.Action)
	require.False(t, outcome.NeedsReview)
	require.Equal(t, "february", outcome.GroupID)
	require.NotEmpty(t, outcome.UpdateID)
	require.Equal(t, 1, provider.itemCount())
	item := provider.item(outcome.ItemID)
	require.Equal(t, integrationItemName, item.name)
	require.Equal(t, "Working on it", item.columns["status"].text)
	require.Equal(t, "2026-02-18", item.columns["date4"].text)
	require.Len(t, item.updates, 1, "exactly one update")
	require.Equal(t, "Dex created this item: status Working on it, due 2026-02-18.<br><br>Scheduled from the facilities request.", item.updates[0].body)

	search := provider.lastRequest("list")
	require.Equal(t, map[string]any{"rules": []any{map[string]any{"column_id": "name", "operator": "any_of", "compare_value": []any{integrationItemName}}}},
		search.variables["queryParams"])
	require.JSONEq(t, `{"date4":{"date":"2026-02-18"},"status":{"label":"Working on it"}}`, provider.lastRequest("create").variables["columnValues"].(string))
	require.Equal(t, 1, provider.count("create"))
}

func TestExistingOpenItemIsScheduledAndDecoysAreUntouchedWithRealDex(t *testing.T) {
	provider := newFakeMonday(t)
	backlog := provider.seedItem(integrationBoardID, "backlog", integrationItemName, "active", "", "")
	completed := provider.seedItem(integrationBoardID, "january", integrationItemName, "active", "Done", "2025-02-18")
	archived := provider.seedItem(integrationBoardID, "backlog", integrationItemName, "archived", "", "")
	otherBoard := provider.seedItem(otherBoardID, "topics", integrationItemName, "active", "", "")
	lookalike := provider.seedItem(integrationBoardID, "backlog", integrationItemName+" (2025)", "active", "", "")
	harness := newWorkHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runWorkRequest(t, "existing-item", integrationInput())
	require.Equal(t, WorkItemScheduled, outcome.Action)
	require.Equal(t, backlog, outcome.ItemID, "the open same-name item is scheduled, not duplicated")
	item := provider.item(backlog)
	require.Equal(t, "Working on it", item.columns["status"].text)
	require.Equal(t, "2026-02-18", item.columns["date4"].text)
	require.Len(t, item.updates, 1)
	require.Equal(t, 5, provider.itemCount(), "no item was created")
	require.Zero(t, provider.count("create"))
	for _, decoy := range []string{completed, archived, otherBoard, lookalike} {
		snapshot := provider.item(decoy)
		require.Equal(t, snapshot.seededAt, snapshot.updatedAt, "item %s must not be touched", decoy)
		require.Empty(t, snapshot.updates)
	}
}

func TestSecondPageIsReadWhenTheFirstHoldsOnlyCompletedItemsWithRealDex(t *testing.T) {
	provider := newFakeMonday(t)
	for index := 0; index < searchPageLimit; index++ {
		provider.seedItem(integrationBoardID, "done", integrationItemName, "active", "Done", "2025-02-18")
	}
	open := provider.seedItem(integrationBoardID, "backlog", integrationItemName, "active", "Stuck", "")
	harness := newWorkHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runWorkRequest(t, "second-page", integrationInput())
	require.Equal(t, WorkItemScheduled, outcome.Action)
	require.Equal(t, open, outcome.ItemID)
	require.Equal(t, 1, provider.count("list"))
	require.Equal(t, 1, provider.count("listNext"), "the cursor page carries the first page's filter")
}

// TestSlowCreateIsDispatchedAgainAndCreatesOneItemWithRealDex is the duplicate-dispatch test: the shared key
// turns Dex's second dispatch into a 409 and a replay.
func TestSlowCreateIsDispatchedAgainAndCreatesOneItemWithRealDex(t *testing.T) {
	provider := newFakeMonday(t)
	provider.delaysFirstCreate = true
	harness := newWorkHarness(t, provider, slowRequestTimeout)

	startedAt := time.Now()
	outcome := harness.runWorkRequest(t, "slow-create", integrationInput())
	require.GreaterOrEqual(t, time.Since(startedAt), slowResponseDelay)
	require.Equal(t, WorkItemCreated, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("create"), 2, "Dex dispatched the create again past its local phase")
	require.Equal(t, 1, provider.count("appliedCreate"), "only the first request ran")
	require.Equal(t, 1, provider.itemCount(), "the repeated dispatch did not create a second item")
	require.Len(t, provider.distinctIdempotencyKeys("create"), 1, "every attempt sent the same key")
	require.Len(t, provider.item(outcome.ItemID).updates, 1)
	t.Logf("slow create: requests=%d conflicts=%d replays=%d replayedOutcome=%v",
		provider.count("create"), provider.count("conflict"), provider.count("replay"), outcome.WasCreateReplayed)
}

// TestSlowScheduleIsSafeToRepeatWithRealDex lets async Dex dispatch the column update again; the key and the absolute values both hold.
func TestSlowScheduleIsSafeToRepeatWithRealDex(t *testing.T) {
	provider := newFakeMonday(t)
	existing := provider.seedItem(integrationBoardID, "backlog", integrationItemName, "active", "", "")
	provider.delaysFirstSchedule = true
	harness := newWorkHarness(t, provider, slowRequestTimeout)

	outcome := harness.runWorkRequest(t, "slow-schedule", integrationInput())
	require.Equal(t, WorkItemScheduled, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("schedule"), 2, "Dex dispatched the update again past its local phase")
	require.Equal(t, 1, provider.count("appliedSchedule"))
	item := provider.item(existing)
	require.Equal(t, "Working on it", item.columns["status"].text)
	require.Len(t, item.updates, 1, "exactly one update")
}

func TestSlowUpdateIsPostedOnceWithRealDex(t *testing.T) {
	provider := newFakeMonday(t)
	provider.delaysFirstUpdate = true
	harness := newWorkHarness(t, provider, slowRequestTimeout)

	outcome := harness.runWorkRequest(t, "slow-update", integrationInput())
	require.Equal(t, WorkItemCreated, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("update"), 2, "Dex dispatched the update again past its local phase")
	require.Len(t, provider.item(outcome.ItemID).updates, 1, "the repeated dispatch did not post a second update")
	require.Len(t, provider.distinctIdempotencyKeys("update"), 1)
}

func TestLostCreateResponseIsReplayedAndCreatesOneItemWithRealDex(t *testing.T) {
	provider := newFakeMonday(t)
	provider.losesFirstCreateResponse = true
	harness := newWorkHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runWorkRequest(t, "lost-create", integrationInput())
	require.Equal(t, WorkItemCreated, outcome.Action)
	require.True(t, outcome.WasCreateReplayed, "the retry received monday.com's cached result")
	require.Equal(t, 2, provider.count("create"))
	require.Equal(t, 1, provider.count("replay"))
	require.Equal(t, 1, provider.itemCount())
}

// TestLostWorkerDuringCreateCreatesOneItemWithRealDex shows the key surviving a Worker lost mid-create.
func TestLostWorkerDuringCreateCreatesOneItemWithRealDex(t *testing.T) {
	provider := newFakeMonday(t)
	provider.holdsFirstCreate = make(chan struct{})
	harness := newWorkHarness(t, provider, slowRequestTimeout)
	flowID := harness.startWorkRequest(t, "lost-worker", integrationInput())
	require.Eventually(t, func() bool { return provider.count("create") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach monday.com")
	harness.replaceWorker(t)
	require.Eventually(t, func() bool { return provider.count("create") >= 2 }, time.Minute, 50*time.Millisecond,
		"an attempt on the new Worker must reach monday.com")
	close(provider.holdsFirstCreate)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome WorkRequestOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, WorkItemCreated, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.itemCount(), "the attempt on the new Worker did not create a second item")
	require.Len(t, provider.distinctIdempotencyKeys("create"), 1, "the key survives Worker replacement")
}

func TestRateLimitedCreateWaitsForMondaysDelayWithRealDex(t *testing.T) {
	provider := newFakeMonday(t)
	provider.rateLimitsFirstCreate = true
	harness := newWorkHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runWorkRequest(t, "rate-limited", integrationInput())
	require.Equal(t, WorkItemCreated, outcome.Action)
	times := provider.requestTimes("create")
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), time.Second, "the retry waited for retry_in_seconds")
	require.Equal(t, 1, provider.itemCount())
}

func TestUnusableCreateAnswerCompletesForReviewWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeMonday(t)
	provider.answersCreateUnusably = true
	harness := newWorkHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runWorkRequest(t, "unusable-create", integrationInput())
	require.Equal(t, WorkItemCreationUncertain, outcome.Action)
	require.True(t, outcome.NeedsReview)
	require.Equal(t, "createItem", outcome.ReviewReason)
	require.Equal(t, "monday.com accepted the item but returned an unusable item", outcome.ReviewDetail)
	require.Equal(t, 1, provider.count("create"), "an accepted create is never sent again")
	require.Equal(t, 1, provider.itemCount(), "monday.com did create the item, which a person must now find")
	require.Zero(t, provider.count("update"))
}

func TestRejectedItemFailsTheFlowWithoutMondayTextWithRealDex(t *testing.T) {
	provider := newFakeMonday(t)
	provider.rejectsCreate = true
	harness := newWorkHarness(t, provider, defaultRequestTimeout)
	flowID := harness.startWorkRequest(t, "rejected", integrationInput())

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	require.NotContains(t, result.ErrorMessage, integrationAPIToken)
	require.Equal(t, 1, provider.count("create"), "a conclusive rejection is not retried")
	require.Zero(t, provider.itemCount())
	t.Logf("rejected create failure: %s", result.ErrorMessage)
}

func TestInvalidRequestFailsBeforeCallingMondayWithRealDex(t *testing.T) {
	provider := newFakeMonday(t)
	harness := newWorkHarness(t, provider, defaultRequestTimeout)
	input := integrationInput()
	input.DueDate = "18/02/2026"
	flowID := harness.startWorkRequest(t, "invalid", input)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.totalRequests())
}

// fakeMonday is a stateful monday.com GraphQL fake that implements the documented Idempotency-Key cache.
type fakeMonday struct {
	*httptest.Server
	t     *testing.T
	mutex sync.Mutex
	// delayedRequests tracks handlers still sleeping or held, so a test can assert their final effect.
	delayedRequests sync.WaitGroup

	clock        time.Time
	nextItemID   int64
	nextUpdateID int64
	items        map[string]*fakeItem
	cursors      map[string]fakeCursor
	idempotency  map[string]*fakeIdempotencyEntry
	counts       map[string]int
	requests     map[string][]fakeRecordedRequest

	delaysFirstCreate        bool
	delaysFirstSchedule      bool
	delaysFirstUpdate        bool
	losesFirstCreateResponse bool
	rateLimitsFirstCreate    bool
	rejectsCreate            bool
	answersCreateUnusably    bool
	holdsFirstCreate         chan struct{}
}

type fakeItem struct {
	id        string
	boardID   string
	groupID   string
	name      string
	state     string
	columns   map[string]fakeColumn
	createdAt time.Time
	seededAt  time.Time
	updatedAt time.Time
	updates   []fakeUpdate
}

type fakeColumn struct {
	columnType string
	text       string
	value      string
}

type fakeUpdate struct {
	id   string
	body string
}

type fakeCursor struct {
	boardID string
	name    string
	offset  int
}

// fakeIdempotencyEntry is monday.com's per-key state: in flight, then a cached response for replay.
type fakeIdempotencyEntry struct {
	isInFlight bool
	status     int
	body       string
}

type fakeRecordedRequest struct {
	at             time.Time
	idempotencyKey string
	variables      map[string]any
}

// fakeResponse is what one GraphQL handler answers; dropsConnection simulates a lost response after the work ran.
type fakeResponse struct {
	status          int
	body            string
	dropsConnection bool
	isCacheable     bool
}

func newFakeMonday(t *testing.T) *fakeMonday {
	t.Helper()
	provider := &fakeMonday{
		t: t, clock: time.Date(2026, 1, 28, 9, 0, 0, 0, time.UTC), nextItemID: 5550000, nextUpdateID: 3300000,
		items: map[string]*fakeItem{}, cursors: map[string]fakeCursor{}, idempotency: map[string]*fakeIdempotencyEntry{},
		counts: map[string]int{}, requests: map[string][]fakeRecordedRequest{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeMonday) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != integrationAPIToken {
		provider.writeJSON(response, http.StatusUnauthorized, `{"errors":[{"message":"Not authenticated","extensions":{"code":"NOT_AUTHENTICATED"}}]}`)
		return
	}
	if request.Method != http.MethodPost || request.URL.Path != "/v2" || request.Header.Get("API-Version") != monday.APIVersion {
		provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"Bad request"}]}`)
		return
	}
	var document struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	body, err := io.ReadAll(request.Body)
	if err != nil || json.Unmarshal(body, &document) != nil {
		provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"x","extensions":{"code":"JsonParseException"}}]}`)
		return
	}
	key := request.Header.Get("Idempotency-Key")
	switch {
	case strings.HasPrefix(document.Query, "query ListBoardItems"):
		provider.record("list", key, document.Variables)
		provider.writeResponse(response, provider.listBoardItems(document.Variables))
	case strings.HasPrefix(document.Query, "query ListNextBoardItems"):
		provider.record("listNext", key, document.Variables)
		provider.writeResponse(response, provider.listNextBoardItems(document.Variables))
	case strings.HasPrefix(document.Query, "query GetItem"):
		provider.record("read", key, document.Variables)
		provider.writeResponse(response, provider.getItem(document.Variables))
	case strings.HasPrefix(document.Query, "mutation CreateItem"):
		attempt := provider.record("create", key, document.Variables)
		provider.runMutation(response, key, func() fakeResponse { return provider.createItem(document.Variables, attempt) })
	case strings.HasPrefix(document.Query, "mutation UpdateItemColumnValues"):
		attempt := provider.record("schedule", key, document.Variables)
		provider.runMutation(response, key, func() fakeResponse { return provider.updateItemColumnValues(document.Variables, attempt) })
	case strings.HasPrefix(document.Query, "mutation AddUpdate"):
		attempt := provider.record("update", key, document.Variables)
		provider.runMutation(response, key, func() fakeResponse { return provider.addUpdate(document.Variables, attempt) })
	default:
		provider.writeJSON(response, http.StatusOK, `{"errors":[{"message":"x","extensions":{"code":"GRAPHQL_VALIDATION_FAILED"}}]}`)
	}
}

// runMutation answers 409 while a key runs, replays it afterwards, and never caches a 429.
func (provider *fakeMonday) runMutation(response http.ResponseWriter, key string, execute func() fakeResponse) {
	if key == "" {
		provider.t.Errorf("a mutation arrived without an Idempotency-Key")
		provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"message":"missing key"}]}`)
		return
	}
	provider.mutex.Lock()
	entry := provider.idempotency[key]
	switch {
	case entry != nil && entry.isInFlight:
		provider.counts["conflict"]++
		provider.mutex.Unlock()
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusConflict, `{"errors":[{"message":"A request with this idempotency key is currently being processed","extensions":{"code":"IDEMPOTENCY_CONFLICT"}}]}`)
		return
	case entry != nil:
		provider.counts["replay"]++
		provider.mutex.Unlock()
		response.Header().Set("Idempotency-Replayed", "true")
		provider.writeJSON(response, entry.status, entry.body)
		return
	}
	entry = &fakeIdempotencyEntry{isInFlight: true}
	provider.idempotency[key] = entry
	provider.mutex.Unlock()

	answer := execute()
	provider.mutex.Lock()
	if answer.isCacheable {
		entry.isInFlight, entry.status, entry.body = false, answer.status, answer.body
	} else {
		delete(provider.idempotency, key)
	}
	provider.mutex.Unlock()
	if answer.dropsConnection {
		provider.dropConnection(response)
		return
	}
	provider.writeResponse(response, answer)
}

func (provider *fakeMonday) listBoardItems(variables map[string]any) fakeResponse {
	boardIDs, _ := variables["boardIds"].([]any)
	if len(boardIDs) != 1 {
		return fakeResponse{status: 200, body: `{"errors":[{"message":"x","extensions":{"code":"InvalidArgumentException"}}]}`}
	}
	boardID := fmt.Sprint(boardIDs[0])
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if boardID != integrationBoardID && boardID != otherBoardID {
		return provider.dataResponse(map[string]any{"boards": []any{}})
	}
	name := ""
	if queryParams, isMap := variables["queryParams"].(map[string]any); isMap {
		rules, _ := queryParams["rules"].([]any)
		require.Len(provider.t, rules, 1, "the example filters on the name alone")
		rule := rules[0].(map[string]any)
		require.Equal(provider.t, "name", rule["column_id"])
		require.Equal(provider.t, "any_of", rule["operator"])
		name = fmt.Sprint(rule["compare_value"].([]any)[0])
	}
	page := provider.itemsPage(fakeCursor{boardID: boardID, name: name}, int(variables["limit"].(float64)), variables["columnIds"])
	return provider.dataResponse(map[string]any{"boards": []any{map[string]any{"id": boardID, "items_page": page}}})
}

func (provider *fakeMonday) listNextBoardItems(variables map[string]any) fakeResponse {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	cursor, isKnown := provider.cursors[fmt.Sprint(variables["cursor"])]
	if !isKnown {
		return fakeResponse{status: 200, body: `{"errors":[{"message":"SENTINEL cursor expired","extensions":{"code":"InvalidArgumentException"}}]}`}
	}
	page := provider.itemsPage(cursor, int(variables["limit"].(float64)), variables["columnIds"])
	return provider.dataResponse(map[string]any{"next_items_page": page})
}

// itemsPage requires provider.mutex; like items_page, it returns active items only.
func (provider *fakeMonday) itemsPage(cursor fakeCursor, limit int, columnIDs any) map[string]any {
	var matches []*fakeItem
	for _, item := range provider.sortedItems() {
		if item.boardID == cursor.boardID && item.state == "active" && (cursor.name == "" || item.name == cursor.name) {
			matches = append(matches, item)
		}
	}
	end := min(cursor.offset+limit, len(matches))
	items := []any{}
	for _, item := range matches[cursor.offset:end] {
		items = append(items, provider.itemJSON(item, columnIDs))
	}
	page := map[string]any{"cursor": nil, "items": items}
	if end < len(matches) {
		token := "cursor-" + strconv.Itoa(len(provider.cursors)+1)
		provider.cursors[token] = fakeCursor{boardID: cursor.boardID, name: cursor.name, offset: end}
		page["cursor"] = token
	}
	return page
}

func (provider *fakeMonday) getItem(variables map[string]any) fakeResponse {
	itemIDs, _ := variables["itemIds"].([]any)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	items := []any{}
	if item, isFound := provider.items[fmt.Sprint(itemIDs[0])]; isFound && (variables["excludeNonactive"] != true || item.state == "active") {
		items = append(items, provider.itemJSON(item, variables["columnIds"]))
	}
	return provider.dataResponse(map[string]any{"items": items})
}

func (provider *fakeMonday) createItem(variables map[string]any, attempt int) fakeResponse {
	if provider.rejectsCreate {
		return fakeResponse{status: 200, isCacheable: true, body: `{"data":{"create_item":null},"errors":[{"message":"SENTINEL This status label does not exist","extensions":{"code":"ColumnValueException","status_code":200,"error_data":{"column_id":"status"}}}]}`}
	}
	if provider.rateLimitsFirstCreate && attempt == 1 {
		return fakeResponse{status: http.StatusTooManyRequests, body: `{"errors":[{"message":"Complexity budget exhausted","extensions":{"code":"COMPLEXITY_BUDGET_EXHAUSTED","retry_in_seconds":1,"status_code":429}}]}`}
	}
	if attempt == 1 && (provider.delaysFirstCreate || provider.holdsFirstCreate != nil) {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		if provider.holdsFirstCreate != nil {
			<-provider.holdsFirstCreate
		} else {
			time.Sleep(slowResponseDelay)
		}
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts["appliedCreate"]++
	provider.nextItemID++
	createdAt := provider.advanceClock()
	item := &fakeItem{
		id: strconv.FormatInt(provider.nextItemID, 10), boardID: fmt.Sprint(variables["boardId"]), groupID: "topics", name: fmt.Sprint(variables["itemName"]),
		state: "active", columns: map[string]fakeColumn{}, createdAt: createdAt, updatedAt: createdAt,
	}
	if groupID, hasGroup := variables["groupId"]; hasGroup {
		item.groupID = fmt.Sprint(groupID)
	}
	provider.applyColumnValues(item, variables["columnValues"])
	provider.items[item.id] = item
	if provider.answersCreateUnusably {
		return fakeResponse{status: 200, isCacheable: true, body: `{"data":{"create_item":{"id":"SENTINEL"}}}`}
	}
	answer := provider.dataResponse(map[string]any{"create_item": provider.itemJSON(item, []any{})})
	answer.isCacheable = true
	answer.dropsConnection = provider.losesFirstCreateResponse && attempt == 1
	return answer
}

func (provider *fakeMonday) updateItemColumnValues(variables map[string]any, attempt int) fakeResponse {
	if provider.delaysFirstSchedule && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	item, isFound := provider.items[fmt.Sprint(variables["itemId"])]
	if !isFound || item.boardID != fmt.Sprint(variables["boardId"]) {
		return fakeResponse{status: 200, isCacheable: true, body: `{"data":{"change_multiple_column_values":null},"errors":[{"message":"x","extensions":{"code":"ResourceNotFoundException","status_code":404}}]}`}
	}
	provider.counts["appliedSchedule"]++
	provider.applyColumnValues(item, variables["columnValues"])
	item.updatedAt = provider.advanceClock()
	answer := provider.dataResponse(map[string]any{"change_multiple_column_values": provider.itemJSON(item, variables["columnIds"])})
	answer.isCacheable = true
	return answer
}

func (provider *fakeMonday) addUpdate(variables map[string]any, attempt int) fakeResponse {
	if provider.delaysFirstUpdate && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	item, isFound := provider.items[fmt.Sprint(variables["itemId"])]
	if !isFound {
		return fakeResponse{status: 200, isCacheable: true, body: `{"data":{"create_update":null},"errors":[{"message":"x","extensions":{"code":"ResourceNotFoundException"}}]}`}
	}
	provider.nextUpdateID++
	update := fakeUpdate{id: strconv.FormatInt(provider.nextUpdateID, 10), body: fmt.Sprint(variables["body"])}
	item.updates = append(item.updates, update)
	createdAt := provider.advanceClock()
	answer := provider.dataResponse(map[string]any{"create_update": map[string]any{
		"id": update.id, "item_id": item.id, "created_at": createdAt.Format(time.RFC3339), "creator_id": "48202303",
	}})
	answer.isCacheable = true
	return answer
}

// applyColumnValues requires provider.mutex; it reads the JSON-encoded column_values string monday.com documents.
func (provider *fakeMonday) applyColumnValues(item *fakeItem, encoded any) {
	text, isString := encoded.(string)
	if encoded == nil {
		return
	}
	require.True(provider.t, isString, "column_values must be a JSON-encoded string")
	var values map[string]json.RawMessage
	require.NoError(provider.t, json.Unmarshal([]byte(text), &values))
	for columnID, raw := range values {
		var decoded map[string]any
		require.NoError(provider.t, json.Unmarshal(raw, &decoded))
		switch columnID {
		case "status":
			item.columns[columnID] = fakeColumn{columnType: "status", text: fmt.Sprint(decoded["label"]), value: string(raw)}
		case "date4":
			item.columns[columnID] = fakeColumn{columnType: "date", text: fmt.Sprint(decoded["date"]), value: string(raw)}
		default:
			provider.t.Errorf("unexpected column %s", columnID)
		}
	}
}

func (provider *fakeMonday) seedItem(boardID string, groupID string, name string, state string, status string, dueDate string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.nextItemID++
	seededAt := provider.advanceClock()
	item := &fakeItem{
		id: strconv.FormatInt(provider.nextItemID, 10), boardID: boardID, groupID: groupID, name: name, state: state,
		columns: map[string]fakeColumn{}, createdAt: seededAt, seededAt: seededAt, updatedAt: seededAt,
	}
	if status != "" {
		item.columns["status"] = fakeColumn{columnType: "status", text: status, value: `{"index":1}`}
	}
	if dueDate != "" {
		item.columns["date4"] = fakeColumn{columnType: "date", text: dueDate, value: `{"date":"` + dueDate + `"}`}
	}
	provider.items[item.id] = item
	return item.id
}

// itemJSON requires provider.mutex; columnIDs is the GraphQL variable, where null means every column.
func (provider *fakeMonday) itemJSON(item *fakeItem, columnIDs any) map[string]any {
	selected := map[string]bool{}
	selectsAll := columnIDs == nil
	if list, isList := columnIDs.([]any); isList {
		for _, columnID := range list {
			selected[fmt.Sprint(columnID)] = true
		}
	}
	columnValues := []any{}
	for _, columnID := range []string{"status", "date4"} {
		if !selectsAll && !selected[columnID] {
			continue
		}
		column, hasValue := item.columns[columnID]
		if !hasValue {
			columnType := map[string]string{"status": "status", "date4": "date"}[columnID]
			columnValues = append(columnValues, map[string]any{"id": columnID, "type": columnType, "text": nil, "value": nil})
			continue
		}
		columnValues = append(columnValues, map[string]any{"id": columnID, "type": column.columnType, "text": column.text, "value": column.value})
	}
	return map[string]any{
		"id": item.id, "name": item.name, "state": item.state, "created_at": item.createdAt.Format(time.RFC3339),
		"updated_at": item.updatedAt.Format(time.RFC3339), "url": "https://acme.monday.com/boards/" + item.boardID + "/pulses/" + item.id,
		"creator_id": "48202303", "board": map[string]any{"id": item.boardID}, "group": map[string]any{"id": item.groupID, "title": item.groupID},
		"column_values": columnValues,
	}
}

func (provider *fakeMonday) dataResponse(data any) fakeResponse {
	encoded, err := json.Marshal(map[string]any{"data": data, "extensions": map[string]any{"request_id": "fake-request"}})
	require.NoError(provider.t, err)
	return fakeResponse{status: http.StatusOK, body: string(encoded)}
}

// advanceClock requires provider.mutex; every write moves updated_at forward by one minute.
func (provider *fakeMonday) advanceClock() time.Time {
	provider.clock = provider.clock.Add(time.Minute)
	return provider.clock
}

// sortedItems requires provider.mutex.
func (provider *fakeMonday) sortedItems() []*fakeItem {
	items := make([]*fakeItem, 0, len(provider.items))
	for _, item := range provider.items {
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool { return items[left].id < items[right].id })
	return items
}

func (provider *fakeMonday) record(name string, key string, variables map[string]any) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[name]++
	provider.requests[name] = append(provider.requests[name], fakeRecordedRequest{at: time.Now(), idempotencyKey: key, variables: variables})
	return provider.counts[name]
}

func (provider *fakeMonday) writeResponse(response http.ResponseWriter, answer fakeResponse) {
	provider.writeJSON(response, answer.status, answer.body)
}

func (provider *fakeMonday) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	if _, err := io.WriteString(response, body); err != nil {
		provider.t.Logf("fake monday.com response write failed: %v", err)
	}
}

// dropConnection closes the connection after monday.com ran the mutation, as a lost response would.
func (provider *fakeMonday) dropConnection(response http.ResponseWriter) {
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(provider.t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(provider.t, err)
	require.NoError(provider.t, connection.Close())
}

func (provider *fakeMonday) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * slowResponseDelay):
		t.Fatal("a delayed fake monday.com request did not finish")
	}
}

func (provider *fakeMonday) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakeMonday) totalRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, requests := range provider.requests {
		total += len(requests)
	}
	return total
}

func (provider *fakeMonday) itemCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.items)
}

func (provider *fakeMonday) item(itemID string) fakeItem {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	item := *provider.items[itemID]
	item.updates = append([]fakeUpdate(nil), item.updates...)
	columns := map[string]fakeColumn{}
	for columnID, column := range item.columns {
		columns[columnID] = column
	}
	item.columns = columns
	return item
}

func (provider *fakeMonday) lastRequest(name string) fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	requests := provider.requests[name]
	require.NotEmpty(provider.t, requests, name)
	return requests[len(requests)-1]
}

func (provider *fakeMonday) distinctIdempotencyKeys(name string) map[string]bool {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	keys := map[string]bool{}
	for _, request := range provider.requests[name] {
		keys[request.idempotencyKey] = true
	}
	return keys
}

func (provider *fakeMonday) requestTimes(name string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var times []time.Time
	for _, request := range provider.requests[name] {
		times = append(times, request.at)
	}
	return times
}

// workHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type workHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newWorkHarness(t *testing.T, provider *fakeMonday, requestTimeout time.Duration) *workHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "monday", Name: ConnectionName}
	providerClient, err := monday.New(monday.Config{}, sdkgo.StaticCredentialProvider[monday.Credentials]{reference: {
		AuthMethodID: monday.PersonalAPITokenAuthMethodID, APIToken: sdkgo.NewSecretString(integrationAPIToken),
	}}, monday.WithAPIURL(provider.URL+"/v2"), monday.WithHTTPClient(&http.Client{Timeout: requestTimeout}))
	require.NoError(t, err)
	connection, err := monday.NewConnection(providerClient, reference)
	require.NoError(t, err)
	harness := &workHarness{flow: NewFlow(connection), serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
	harness.registry, err = dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.workerAddress = net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.startWorker(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return harness
}

func (harness *workHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress, WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	harness.worker, harness.workerResult = worker, workerResult
}

// replaceWorker force-stops the Worker without draining its handlers, like a crash, then starts a new one.
func (harness *workHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *workHarness) runWorkRequest(t *testing.T, scenario string, input Input) WorkRequestOutcome {
	t.Helper()
	flowID := harness.startWorkRequest(t, scenario, input)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome WorkRequestOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

func (harness *workHarness) startWorkRequest(t *testing.T, scenario string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "monday-work-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *workHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for {
		result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err, "Flow %s did not close", flowID)
		return result
	}
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
