// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAccessToken  = "dG9rOjBiMjY3intercomTestToken0123456789"
	testClientSecret = "intercomTestClientSecret0123456789"
	testAdminID      = "5017691"
	testConversation = "215472658213"
)

var intercomConnection = sdkgo.ConnectionRef{Provider: "intercom", Name: "intercom-test"}

// testDexContext is a Dex Step context whose heartbeat details persist across calls, like attempts of one Step execution.
type testDexContext struct {
	context.Context
	step string

	mutex           sync.Mutex
	heartbeat       json.RawMessage
	heartbeatWrites int
	recordErr       error
}

func newTestDexContext(step string) *testDexContext {
	return &testDexContext{Context: context.Background(), step: step}
}

func (*testDexContext) FlowID() string                          { return "intercom-flow" }
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
	dexContext.heartbeatWrites++
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

// recordingIntercom answers each request with respond and records it.
type recordingIntercom struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingIntercom(t *testing.T, respond func(response http.ResponseWriter, request *http.Request, index int)) *recordingIntercom {
	t.Helper()
	provider := &recordingIntercom{}
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

func (provider *recordingIntercom) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

func (provider *recordingIntercom) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func newIntercomClient(t *testing.T, baseURL string, options ...intercom.Option) *intercom.Client {
	t.Helper()
	client, err := intercom.New(intercom.Config{}, testCredentialProvider(), append([]intercom.Option{intercom.WithAPIBaseURL(baseURL)}, options...)...)
	require.NoError(t, err)
	return client
}

func testCredentialProvider() sdkgo.StaticCredentialProvider[intercom.Credentials] {
	return sdkgo.StaticCredentialProvider[intercom.Credentials]{
		intercomConnection: {AccessToken: sdkgo.NewSecretString(testAccessToken), ClientSecret: sdkgo.NewSecretString(testClientSecret)},
	}
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err := io.WriteString(response, body)
	require.NoError(t, err)
}

// testPart is one conversation part for conversationJSON.
type testPart struct {
	id, partType, body, authorType, authorID string
	createdAt                                int64
}

// conversationJSON renders an Intercom 2.16 conversation with parts in chronological order.
func conversationJSON(t *testing.T, conversationID string, state string, snoozedUntil any, parts ...testPart) string {
	t.Helper()
	encodedParts := []any{}
	for _, part := range parts {
		encodedParts = append(encodedParts, map[string]any{
			"type": "conversation_part", "id": part.id, "part_type": part.partType, "body": part.body, "created_at": part.createdAt,
			"author": map[string]any{"type": part.authorType, "id": part.authorID, "name": "Ada", "email": "ada@acme.example.com"},
		})
	}
	encoded, err := json.Marshal(map[string]any{
		"type": "conversation", "id": conversationID, "title": nil, "created_at": 1767225600, "updated_at": 1767229200,
		"waiting_since": nil, "snoozed_until": snoozedUntil, "open": state != "closed", "state": state, "read": true,
		"priority": "high", "admin_assignee_id": 0, "team_assignee_id": 5017690,
		"source": map[string]any{
			"type": "conversation", "id": "403918330", "delivered_as": "customer_initiated", "subject": "",
			"body":   "I was charged twice for order 88213.",
			"author": map[string]any{"type": "user", "id": "5ba682d23d7cf92bef87bfd4", "name": "Jane", "email": "jane@acme.example.com"},
		},
		"contacts":           map[string]any{"type": "contact.list", "contacts": []any{map[string]any{"type": "contact", "id": "5ba682d23d7cf92bef87bfd4", "external_id": "cont_010"}}},
		"tags":               map[string]any{"type": "tag.list", "tags": []any{map[string]any{"type": "tag", "id": "123456", "name": "billing"}}},
		"conversation_parts": map[string]any{"type": "conversation_part.list", "conversation_parts": encodedParts, "total_count": len(parts)},
	})
	require.NoError(t, err)
	return string(encoded)
}

func unixText(instant time.Time) string {
	return strconv.FormatInt(instant.Unix(), 10)
}

func containsAny(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
