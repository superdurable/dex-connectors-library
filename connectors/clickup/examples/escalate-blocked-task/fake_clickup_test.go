// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	// fakeAPIToken has the documented pk_ shape; it is not a real token.
	fakeAPIToken      = "pk_" + "4411723_FAKETOKEN0123456789ABCDEFGHIJ"
	fakeWebhookSecret = "FAKEWEBHOOKSECRET0123456789ABCDEF"
	fakeWebhookID     = "7fa3ec74-69a8-4530-a251-8a13730bd204"
	fakeWorkspaceID   = "9014123456"
	fakeEngineeringID = "901410000001"
	fakeEscalationID  = "901410000099"
	fakeManagerEmail  = "manager@acme.example.com"
	fakeManagerID     = int64(183)
	fakeManagerName   = "Ada Lovelace"
	// providerSentinel is ClickUp error text that must never reach a Flow.
	providerSentinel  = "SENTINEL provider text"
	slowResponseDelay = 9 * time.Second
)

// fakeTask is one task the fake stores.
type fakeTask struct {
	id, name, status, listID, markdown string
	priority                           int
	assignees                          []int64
	tags                               []string
	createdAt                          time.Time
}

type fakeComment struct {
	id, text  string
	createdAt time.Time
}

type fakeRequest struct {
	at   time.Time
	body string
}

// fakeClickUp is a stateful ClickUp API v2 that requires the raw personal token and records every request by kind.
type fakeClickUp struct {
	*httptest.Server
	t *testing.T

	mu       sync.Mutex
	tasks    map[string]*fakeTask
	comments map[string][]fakeComment
	requests map[string][]fakeRequest
	nextID   int
	delayed  sync.WaitGroup

	delaysFirstCreate, delaysFirstUpdate, delaysFirstComment bool
	losesFirstCreateResponse, failsFirstCreateBeforeApplying bool
	rateLimitsFirstSearch, rejectsCreate                     bool
	holdsFirstCreate                                         chan struct{}
}

func newFakeClickUp(t *testing.T) *fakeClickUp {
	t.Helper()
	provider := &fakeClickUp{t: t, tasks: map[string]*fakeTask{}, comments: map[string][]fakeComment{}, requests: map[string][]fakeRequest{}, nextID: 1000}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeClickUp) serve(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	require.NoError(provider.t, err)
	if request.Header.Get("Authorization") != fakeAPIToken {
		provider.writeJSON(response, http.StatusUnauthorized, `{"err":"Token invalid","ECODE":"OAUTH_025"}`)
		return
	}
	segments := strings.Split(strings.TrimPrefix(request.URL.Path, "/"), "/")
	switch {
	case request.Method == http.MethodGet && len(segments) == 1 && segments[0] == "team":
		provider.record("members", body)
		provider.writeJSON(response, http.StatusOK, `{"teams":[{"id":"`+fakeWorkspaceID+`","name":"Acme","members":[`+
			`{"user":{"id":`+strconv.FormatInt(fakeManagerID, 10)+`,"username":"`+fakeManagerName+`","email":"Manager@Acme.example.com","role":2}},`+
			`{"user":{"id":184,"username":"Ben","email":"ben@acme.example.com","role":3}}]}]}`)
	case request.Method == http.MethodGet && len(segments) == 3 && segments[0] == "team" && segments[2] == "task":
		provider.serveSearch(response, request, body)
	case len(segments) == 2 && segments[0] == "task" && request.Method == http.MethodGet:
		provider.record("read", body)
		provider.writeTask(response, segments[1])
	case len(segments) == 2 && segments[0] == "task" && request.Method == http.MethodPut:
		provider.serveUpdate(response, segments[1], body)
	case len(segments) == 4 && segments[0] == "task" && segments[2] == "tag":
		provider.serveTag(response, request.Method, segments[1], segments[3], body)
	case len(segments) == 3 && segments[0] == "task" && segments[2] == "comment":
		provider.serveComment(response, request.Method, segments[1], body)
	case len(segments) == 3 && segments[0] == "list" && segments[2] == "task" && request.Method == http.MethodPost:
		provider.serveCreate(response, segments[1], body)
	case len(segments) == 3 && segments[0] == "list" && segments[2] == "task" && request.Method == http.MethodGet:
		provider.serveListRead(response, request, segments[1], body)
	default:
		provider.writeJSON(response, http.StatusNotFound, `{"err":"Route not found","ECODE":"APP_001"}`)
	}
}

func (provider *fakeClickUp) serveSearch(response http.ResponseWriter, request *http.Request, body []byte) {
	isFirst := provider.record("search", body) == 1
	if isFirst && provider.rateLimitsFirstSearch {
		response.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(1500*time.Millisecond).Unix(), 10))
		provider.writeJSON(response, http.StatusTooManyRequests, `{"err":"`+providerSentinel+`","ECODE":"APP_002"}`)
		return
	}
	query := request.URL.Query()
	provider.mu.Lock()
	var matches []any
	for _, task := range provider.sortedTasks() {
		if lists := query["list_ids[]"]; len(lists) != 0 && !slices.Contains(lists, task.listID) {
			continue
		}
		if tags := query["tags[]"]; len(tags) != 0 && !slices.ContainsFunc(tags, func(tag string) bool { return slices.Contains(task.tags, tag) }) {
			continue
		}
		matches = append(matches, provider.taskMap(task))
	}
	provider.mu.Unlock()
	provider.writeValue(response, map[string]any{"tasks": nonNil(matches), "last_page": true})
}

func (provider *fakeClickUp) serveCreate(response http.ResponseWriter, listID string, body []byte) {
	isFirst := provider.record("create", body) == 1
	if provider.rejectsCreate {
		provider.writeJSON(response, http.StatusBadRequest, `{"err":"`+providerSentinel+`","ECODE":"ITEM_015"}`)
		return
	}
	if isFirst && provider.failsFirstCreateBeforeApplying {
		provider.writeJSON(response, http.StatusServiceUnavailable, `{"err":"`+providerSentinel+`","ECODE":"SERVER_1"}`)
		return
	}
	if isFirst && (provider.delaysFirstCreate || provider.holdsFirstCreate != nil) {
		provider.delayed.Add(1)
		defer provider.delayed.Done()
		if provider.holdsFirstCreate != nil {
			<-provider.holdsFirstCreate
		} else {
			time.Sleep(slowResponseDelay)
		}
	}
	var wire struct {
		Name            string   `json:"name"`
		MarkdownContent string   `json:"markdown_content"`
		Priority        int      `json:"priority"`
		Assignees       []int64  `json:"assignees"`
		Tags            []string `json:"tags"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &wire))
	provider.mu.Lock()
	task := &fakeTask{id: provider.newID(), name: wire.Name, status: "to do", listID: listID, markdown: wire.MarkdownContent,
		priority: wire.Priority, assignees: wire.Assignees, tags: wire.Tags, createdAt: time.Now()}
	provider.tasks[task.id] = task
	encoded := provider.taskMap(task)
	provider.mu.Unlock()
	if isFirst && provider.losesFirstCreateResponse {
		hijackAndClose(provider.t, response)
		return
	}
	provider.writeValue(response, encoded)
}

func (provider *fakeClickUp) serveListRead(response http.ResponseWriter, request *http.Request, listID string, body []byte) {
	provider.record("listRead", body)
	createdAfter, err := strconv.ParseInt(request.URL.Query().Get("date_created_gt"), 10, 64)
	require.NoError(provider.t, err, "the read-back is bounded by the dispatch time")
	provider.mu.Lock()
	var matches []any
	for _, task := range provider.sortedTasks() {
		if task.listID == listID && task.createdAt.UnixMilli() > createdAfter {
			matches = append(matches, provider.taskMap(task))
		}
	}
	provider.mu.Unlock()
	provider.writeValue(response, map[string]any{"tasks": nonNil(matches), "last_page": true})
}

func (provider *fakeClickUp) serveUpdate(response http.ResponseWriter, taskID string, body []byte) {
	if provider.record("update", body) == 1 && provider.delaysFirstUpdate {
		provider.delayed.Add(1)
		defer provider.delayed.Done()
		time.Sleep(slowResponseDelay)
	}
	var wire struct {
		Status    string `json:"status"`
		Priority  int    `json:"priority"`
		Assignees *struct {
			Add    []int64 `json:"add"`
			Remove []int64 `json:"rem"`
		} `json:"assignees"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &wire))
	provider.mu.Lock()
	task, isFound := provider.tasks[taskID]
	if isFound {
		if wire.Status != "" {
			task.status = wire.Status
		}
		if wire.Priority != 0 {
			task.priority = wire.Priority
		}
		if wire.Assignees != nil {
			for _, userID := range wire.Assignees.Add {
				if !slices.Contains(task.assignees, userID) {
					task.assignees = append(task.assignees, userID)
				}
			}
			task.assignees = slices.DeleteFunc(task.assignees, func(userID int64) bool { return slices.Contains(wire.Assignees.Remove, userID) })
		}
	}
	provider.mu.Unlock()
	provider.writeTask(response, taskID)
}

func (provider *fakeClickUp) serveTag(response http.ResponseWriter, method string, taskID string, tag string, body []byte) {
	provider.record("tag", body)
	provider.mu.Lock()
	task, isFound := provider.tasks[taskID]
	if isFound && method == http.MethodPost && !slices.Contains(task.tags, tag) {
		task.tags = append(task.tags, tag)
	}
	if isFound && method == http.MethodDelete {
		task.tags = slices.DeleteFunc(task.tags, func(existing string) bool { return existing == tag })
	}
	provider.mu.Unlock()
	if !isFound {
		provider.writeJSON(response, http.StatusNotFound, `{"err":"`+providerSentinel+`","ECODE":"ITEM_013"}`)
		return
	}
	provider.writeJSON(response, http.StatusOK, `{}`)
}

func (provider *fakeClickUp) serveComment(response http.ResponseWriter, method string, taskID string, body []byte) {
	if method == http.MethodGet {
		provider.record("commentRead", body)
		provider.mu.Lock()
		comments := []any{}
		for _, comment := range slices.Backward(provider.comments[taskID]) {
			comments = append(comments, map[string]any{
				"id": comment.id, "comment_text": comment.text, "date": strconv.FormatInt(comment.createdAt.UnixMilli(), 10),
				"user": map[string]any{"id": 4411723, "username": "Dex Bot"},
			})
		}
		provider.mu.Unlock()
		provider.writeValue(response, map[string]any{"comments": comments})
		return
	}
	if provider.record("comment", body) == 1 && provider.delaysFirstComment {
		provider.delayed.Add(1)
		defer provider.delayed.Done()
		time.Sleep(slowResponseDelay)
	}
	var wire struct {
		CommentText string `json:"comment_text"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &wire))
	provider.mu.Lock()
	comment := fakeComment{id: strconv.Itoa(90140000 + provider.nextID), text: wire.CommentText, createdAt: time.Now()}
	provider.nextID++
	provider.comments[taskID] = append(provider.comments[taskID], comment)
	provider.mu.Unlock()
	provider.writeValue(response, map[string]any{"id": comment.id, "hist_id": "26508", "date": comment.createdAt.UnixMilli()})
}

func (provider *fakeClickUp) writeTask(response http.ResponseWriter, taskID string) {
	provider.mu.Lock()
	task, isFound := provider.tasks[taskID]
	var encoded map[string]any
	if isFound {
		encoded = provider.taskMap(task)
	}
	provider.mu.Unlock()
	if !isFound {
		provider.writeJSON(response, http.StatusNotFound, `{"err":"`+providerSentinel+`","ECODE":"ITEM_013"}`)
		return
	}
	provider.writeValue(response, encoded)
}

// record stores one request of kind and returns how many of that kind arrived so far.
func (provider *fakeClickUp) record(kind string, body []byte) int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.requests[kind] = append(provider.requests[kind], fakeRequest{at: time.Now(), body: string(body)})
	return len(provider.requests[kind])
}

func (provider *fakeClickUp) newID() string {
	provider.nextID++
	return "86b2x" + strconv.Itoa(provider.nextID)
}

func (provider *fakeClickUp) sortedTasks() []*fakeTask {
	tasks := make([]*fakeTask, 0, len(provider.tasks))
	for _, task := range provider.tasks {
		tasks = append(tasks, task)
	}
	slices.SortFunc(tasks, func(left, right *fakeTask) int { return strings.Compare(left.id, right.id) })
	return tasks
}

// taskMap renders a task in ClickUp API v2's Get Task shape.
func (provider *fakeClickUp) taskMap(task *fakeTask) map[string]any {
	assignees := []any{}
	for _, userID := range task.assignees {
		assignees = append(assignees, map[string]any{"id": userID, "username": "User " + strconv.FormatInt(userID, 10), "email": "user@acme.example.com"})
	}
	tags := []any{}
	for _, tag := range task.tags {
		tags = append(tags, map[string]any{"name": tag, "tag_fg": "#000000", "tag_bg": "#000000"})
	}
	var priority any
	if task.priority != 0 {
		priority = map[string]any{"id": strconv.Itoa(task.priority), "priority": "urgent", "color": "#f50000", "orderindex": "1"}
	}
	return map[string]any{
		"id": task.id, "custom_id": nil, "name": task.name, "url": "https://app.clickup.com/t/" + task.id,
		"status": map[string]any{"status": task.status, "type": "custom", "color": "#d3d3d3", "orderindex": 1}, "priority": priority,
		"date_created": strconv.FormatInt(task.createdAt.UnixMilli(), 10), "date_updated": strconv.FormatInt(task.createdAt.UnixMilli(), 10),
		"assignees": assignees, "tags": tags, "parent": nil, "team_id": fakeWorkspaceID, "markdown_description": task.markdown,
		"list": map[string]any{"id": task.listID, "name": "List"}, "folder": map[string]any{"id": "90140000077"}, "space": map[string]any{"id": "90140000011"},
		"custom_fields": []any{},
	}
}

func (provider *fakeClickUp) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err := io.WriteString(response, body)
	require.NoError(provider.t, err)
}

func (provider *fakeClickUp) writeValue(response http.ResponseWriter, value any) {
	encoded, err := json.Marshal(value)
	require.NoError(provider.t, err)
	provider.writeJSON(response, http.StatusOK, string(encoded))
}

// seedTask stores a task in a List and returns its ID.
func (provider *fakeClickUp) seedTask(name string, status string, listID string, tags ...string) string {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	task := &fakeTask{id: provider.newID(), name: name, status: status, listID: listID, tags: tags, createdAt: time.Now().Add(-time.Hour)}
	provider.tasks[task.id] = task
	return task.id
}

// task returns a copy of a stored task.
func (provider *fakeClickUp) task(taskID string) fakeTask {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	task := *provider.tasks[taskID]
	task.assignees, task.tags = slices.Clone(task.assignees), slices.Clone(task.tags)
	return task
}

// tasksInList returns the stored tasks of a List, ordered by ID.
func (provider *fakeClickUp) tasksInList(listID string) []fakeTask {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	var tasks []fakeTask
	for _, task := range provider.sortedTasks() {
		if task.listID == listID {
			tasks = append(tasks, *task)
		}
	}
	return tasks
}

func (provider *fakeClickUp) commentsOf(taskID string) []fakeComment {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return slices.Clone(provider.comments[taskID])
}

func (provider *fakeClickUp) count(kind string) int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return len(provider.requests[kind])
}

func (provider *fakeClickUp) totalRequests() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	total := 0
	for _, requests := range provider.requests {
		total += len(requests)
	}
	return total
}

func (provider *fakeClickUp) lastRequestBody(kind string) string {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	requests := provider.requests[kind]
	require.NotEmpty(provider.t, requests, "no %s request", kind)
	return requests[len(requests)-1].body
}

func (provider *fakeClickUp) requestTimes(kind string) []time.Time {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	var times []time.Time
	for _, request := range provider.requests[kind] {
		times = append(times, request.at)
	}
	return times
}

// waitForDelayedRequests waits for held requests to finish, so their side effects are visible.
func (provider *fakeClickUp) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayed.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatal("a delayed fake ClickUp request did not finish")
	}
}

// signedStatusEvent is ClickUp's taskStatusUpdated webhook body for one change and its X-Signature.
func signedStatusEvent(taskID string, historyItemID string, statusAfter string, secret string) ([]byte, string) {
	body := []byte(`{"event":"taskStatusUpdated","history_items":[{"id":"` + historyItemID + `","type":1,"date":"` +
		strconv.FormatInt(time.Now().UnixMilli(), 10) + `","field":"status","parent_id":"` + fakeEngineeringID + `","data":{"status_type":"custom"},` +
		`"source":null,"user":{"id":184,"username":"Ben","email":"ben@acme.example.com"},"before":{"status":"in progress","type":"custom"},` +
		`"after":{"status":"` + statusAfter + `","type":"custom"}}],"task_id":"` + taskID + `","webhook_id":"` + fakeWebhookID + `"}`)
	return body, taskEventSignature(body, secret)
}

func taskEventSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body) // A hash write never fails.
	return hex.EncodeToString(mac.Sum(nil))
}

// hijackAndClose drops the connection after the fake applied the request, so its outcome is unknown.
func hijackAndClose(t *testing.T, response http.ResponseWriter) {
	t.Helper()
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(t, err)
	require.NoError(t, connection.Close())
}

func nonNil(values []any) []any {
	if values == nil {
		return []any{}
	}
	return values
}
