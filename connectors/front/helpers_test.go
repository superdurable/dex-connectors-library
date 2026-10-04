// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/front"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// testAPIToken is deliberately not shaped like a Front JSON Web Token.
	testAPIToken       = "front-unit-test-token-0123456789"
	testConversationID = "cnv_55c8c149"
	testTeammateID     = "tea_2thf"
	testTagID          = "tag_13o8r1"
	providerSentinel   = "SENTINEL-front-message-text"
)

var frontConnection = sdkgo.ConnectionRef{Provider: "front", Name: "front-test"}

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

func (*testDexContext) FlowID() string                          { return "front-flow" }
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
	method        string
	path          string
	authorization string
	body          string
}

// recordingFront answers each request with respond and records it.
type recordingFront struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingFront(t *testing.T, respond func(response http.ResponseWriter, request *http.Request, index int)) *recordingFront {
	t.Helper()
	provider := &recordingFront{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recordedRequest{
			method: request.Method, path: request.URL.EscapedPath() + queryText(request), authorization: request.Header.Get("Authorization"), body: string(body),
		})
		provider.mutex.Unlock()
		request.Body = io.NopCloser(bytes.NewReader(body))
		respond(response, request, index)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func queryText(request *http.Request) string {
	if request.URL.RawQuery == "" {
		return ""
	}
	return "?" + request.URL.RawQuery
}

func (provider *recordingFront) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

func (provider *recordingFront) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func newFrontClient(t *testing.T, baseURL string, options ...front.Option) *front.Client {
	t.Helper()
	client, err := front.New(front.Config{}, testCredentialProvider(), append([]front.Option{front.WithAPIBaseURL(baseURL)}, options...)...)
	require.NoError(t, err)
	return client
}

func testCredentialProvider() sdkgo.StaticCredentialProvider[front.Credentials] {
	return sdkgo.StaticCredentialProvider[front.Credentials]{frontConnection: {APIToken: sdkgo.NewSecretString(testAPIToken)}}
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err := io.WriteString(response, body)
	require.NoError(t, err)
}

// writeFrontError answers with Front's _error envelope, whose message the connector must never repeat.
func writeFrontError(t *testing.T, response http.ResponseWriter, status int) {
	t.Helper()
	writeJSON(t, response, status, `{"_error":{"status":`+jsonNumber(status)+`,"title":"Error","message":"`+providerSentinel+`"}}`)
}

func jsonNumber(value int) string {
	encoded, _ := json.Marshal(value) // An int always encodes.
	return string(encoded)
}

// testConversation is the conversation conversationJSON renders.
type testConversation struct {
	id, status, statusID, assigneeID string
	tagIDs                           []string
	isSnoozed                        bool
}

// conversationJSON renders a Front conversation the way the Core API returns it.
func conversationJSON(t *testing.T, conversation testConversation) string {
	t.Helper()
	var assignee any
	if conversation.assigneeID != "" {
		assignee = map[string]any{"id": conversation.assigneeID, "email": "leela@planet-express.example.com", "first_name": "Leela", "last_name": "Turanga"}
	}
	tags := []any{}
	for _, tagID := range conversation.tagIDs {
		tags = append(tags, map[string]any{"id": tagID, "name": "name-of-" + tagID})
	}
	reminders := []any{}
	if conversation.isSnoozed {
		reminders = append(reminders, map[string]any{"scheduled_at": 1767312000})
	}
	document := map[string]any{
		"_links": map[string]any{"self": "https://acme.api.frontapp.com/conversations/" + conversation.id},
		"id":     conversation.id, "type": "conversation", "subject": "Double charge on order 88213", "status": conversation.status,
		"status_category": "open", "ticket_ids": []string{"TICKET-1"}, "assignee": assignee,
		"recipient": map[string]any{
			"_links": map[string]any{"related": map[string]any{"contact": "https://acme.api.frontapp.com/contacts/crd_1y8sp71"}},
			"handle": "jane@acme.example.com", "role": "from", "name": "Jane Smith",
		},
		"tags": tags, "links": []any{}, "custom_fields": map[string]any{}, "is_private": false, "scheduled_reminders": reminders,
		"created_at": 1767225600.123, "updated_at": 1767229200.5, "waiting_since": 1767226000, "metadata": map[string]any{},
	}
	if conversation.statusID != "" {
		document["status_id"] = conversation.statusID
	}
	encoded, err := json.Marshal(document)
	require.NoError(t, err)
	return string(encoded)
}
