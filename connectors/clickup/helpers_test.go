// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// testAPIToken has the documented pk_ shape; it is not a real token.
	testAPIToken      = "pk_" + "4411723_TESTTOKEN0123456789ABCDEFGHIJ"
	testWebhookSecret = "TESTWEBHOOKSECRET0123456789ABCDEF"
	testWorkspaceID   = "9014123456"
	testListID        = "901410000001"
	testTaskID        = "86b2x4k7q"
	// providerSentinel is ClickUp error text that must never reach a Result.
	providerSentinel = "SENTINEL provider text"
)

var clickupConnection = sdkgo.ConnectionRef{Provider: "clickup", Name: "clickup-test"}

// testDexContext is a Dex Step context whose heartbeat details persist across calls, like attempts of one Step execution.
type testDexContext struct {
	context.Context
	step string

	mutex     sync.Mutex
	heartbeat json.RawMessage
	recordErr error
}

func newTestDexContext(step string) *testDexContext {
	return &testDexContext{Context: context.Background(), step: step}
}

func (*testDexContext) FlowID() string                          { return "clickup-flow" }
func (*testDexContext) RunID() string                           { return "run" }
func (*testDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (dexContext *testDexContext) StepExecutionID() string      { return dexContext.step }
func (*testDexContext) FromStepExecutionID() string             { return "" }
func (*testDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*testDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (*testDexContext) Attempt() int32                          { return 1 }
func (*testDexContext) HasTimerFired() bool                     { return false }
func (*testDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*testDexContext) WaitForMethodFailed() bool               { return false }
func (*testDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*testDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*testDexContext) RecordEvent(string, any) error { return nil }

func (dexContext *testDexContext) RecordHeartbeat(value any) error {
	dexContext.mutex.Lock()
	defer dexContext.mutex.Unlock()
	if dexContext.recordErr != nil {
		return dexContext.recordErr
	}
	if value == nil {
		dexContext.heartbeat = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	dexContext.heartbeat = encoded
	return nil
}

func (dexContext *testDexContext) GetLastHeartbeatValue(valuePtr any) (bool, error) {
	dexContext.mutex.Lock()
	defer dexContext.mutex.Unlock()
	if dexContext.heartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(dexContext.heartbeat, valuePtr)
}

func (dexContext *testDexContext) hasHeartbeat() bool {
	dexContext.mutex.Lock()
	defer dexContext.mutex.Unlock()
	return dexContext.heartbeat != nil
}

var _ dex.Context = (*testDexContext)(nil)

// recordedRequest is one request the recording fake received.
type recordedRequest struct {
	method string
	path   string
	header http.Header
	body   string
}

// recordingClickUp answers each request with respond and records it.
type recordingClickUp struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingClickUp(t *testing.T, respond func(response http.ResponseWriter, request *http.Request, index int)) *recordingClickUp {
	t.Helper()
	provider := &recordingClickUp{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recordedRequest{
			method: request.Method, path: request.URL.RequestURI(), header: request.Header.Clone(), body: string(body),
		})
		provider.mutex.Unlock()
		respond(response, request, index)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingClickUp) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

func (provider *recordingClickUp) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func newClickUpClient(t *testing.T, baseURL string, options ...clickup.Option) *clickup.Client {
	t.Helper()
	client, err := clickup.New(clickup.Config{}, testCredentialProvider(), append([]clickup.Option{clickup.WithAPIBaseURL(baseURL)}, options...)...)
	require.NoError(t, err)
	return client
}

func testCredentialProvider() sdkgo.StaticCredentialProvider[clickup.Credentials] {
	return sdkgo.StaticCredentialProvider[clickup.Credentials]{
		clickupConnection: {APIToken: sdkgo.NewSecretString(testAPIToken), WebhookSecret: sdkgo.NewSecretString(testWebhookSecret)},
	}
}

func newTestConnection(t *testing.T) clickup.Connection {
	t.Helper()
	connection, err := clickup.NewConnection(newClickUpClient(t, "http://127.0.0.1:1"), clickupConnection)
	require.NoError(t, err)
	return connection
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err := io.WriteString(response, body)
	require.NoError(t, err)
}

// clickupError is ClickUp's error body with free text that must never reach a Result.
func clickupError(code string) string {
	return `{"err":"` + providerSentinel + `","ECODE":"` + code + `"}`
}

// testTask is the part of a ClickUp task that taskJSON renders.
type testTask struct {
	id, name, status, parent string
	createdAt                time.Time
	assigneeIDs              []int64
	tags                     []string
}

// taskJSON renders a ClickUp API v2 task in the shape of Get Task.
func taskJSON(t *testing.T, task testTask) string {
	t.Helper()
	encoded, err := json.Marshal(taskMap(task))
	require.NoError(t, err)
	return string(encoded)
}

func taskMap(task testTask) map[string]any {
	assignees := []any{}
	for _, assigneeID := range task.assigneeIDs {
		assignees = append(assignees, map[string]any{"id": assigneeID, "username": "Ada", "email": "ada@acme.example.com", "color": "#7b68ee"})
	}
	tags := []any{}
	for _, tag := range task.tags {
		tags = append(tags, map[string]any{"name": tag, "tag_fg": "#000000", "tag_bg": "#000000"})
	}
	createdAt := task.createdAt
	if createdAt.IsZero() {
		createdAt = time.UnixMilli(1767225600000)
	}
	var parent any
	if task.parent != "" {
		parent = task.parent
	}
	status := task.status
	if status == "" {
		status = "to do"
	}
	return map[string]any{
		"id": task.id, "custom_id": nil, "name": task.name, "url": "https://app.clickup.com/t/" + task.id,
		"status":       map[string]any{"status": status, "type": "open", "color": "#d3d3d3", "orderindex": 0},
		"priority":     map[string]any{"id": "2", "priority": "high", "color": "#ffcc00", "orderindex": "2"},
		"date_created": jsonMillis(createdAt), "date_updated": jsonMillis(createdAt.Add(time.Minute)), "date_closed": nil,
		"due_date": "1767484800000", "start_date": nil, "archived": false, "parent": parent,
		"creator":   map[string]any{"id": 4411723, "username": "Dex Bot", "email": "bot@acme.example.com"},
		"assignees": assignees, "tags": tags, "team_id": testWorkspaceID,
		"list":   map[string]any{"id": testListID, "name": "Escalations", "access": true},
		"folder": map[string]any{"id": "90140000077", "name": "Support", "hidden": false, "access": true},
		"space":  map[string]any{"id": "90140000011"},
	}
}

// jsonMillis renders a time the way ClickUp does: Unix milliseconds as a string.
func jsonMillis(instant time.Time) string {
	return strconv.FormatInt(instant.UnixMilli(), 10)
}
