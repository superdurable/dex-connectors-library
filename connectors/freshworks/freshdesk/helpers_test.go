// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testDomain = "acme"
	testAPIKey = "freshdeskTestKey0123456789"
)

var freshdeskConnection = sdkgo.ConnectionRef{Provider: "freshdesk", Name: "freshdesk-test"}

// recordedRequest is one request a recordingFreshdesk received.
type recordedRequest struct {
	method string
	path   string
	query  map[string][]string
	header http.Header
	body   string
}

// recordingFreshdesk is a credential-safe httptest server that records requests and delegates replies.
type recordingFreshdesk struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingFreshdesk(t *testing.T, reply func(http.ResponseWriter, *http.Request, int)) *recordingFreshdesk {
	t.Helper()
	provider := &recordingFreshdesk{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recordedRequest{
			method: request.Method, path: request.URL.Path, query: request.URL.Query(), header: request.Header.Clone(), body: string(body),
		})
		provider.mutex.Unlock()
		reply(response, request, index)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingFreshdesk) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingFreshdesk) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

func newFreshdeskClient(t *testing.T, baseURL string) *freshdesk.Client {
	t.Helper()
	if !strings.HasSuffix(baseURL, "/api/v2") {
		baseURL += "/api/v2"
	}
	client, err := freshdesk.New(freshdesk.Config{Domain: testDomain}, testCredentialProvider(), freshdesk.WithAPIBaseURL(baseURL))
	require.NoError(t, err)
	return client
}

func testCredentialProvider() sdkgo.StaticCredentialProvider[freshdesk.Credentials] {
	return sdkgo.StaticCredentialProvider[freshdesk.Credentials]{freshdeskConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
}

// closedLoopbackURL returns an address nothing listens on, so a connection is refused before any request byte is sent.
func closedLoopbackURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return "http://" + address + "/api/v2"
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	if response.Header().Get("X-Request-Id") == "" {
		response.Header().Set("X-Request-Id", "request-0001")
	}
	response.WriteHeader(status)
	_, err := io.WriteString(response, body)
	require.NoError(t, err)
}

func writeValue(t *testing.T, response http.ResponseWriter, status int, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	writeJSON(t, response, status, string(encoded))
}

// dropConnection closes the connection after the request arrived, as a lost response would.
func dropConnection(t *testing.T, response http.ResponseWriter) {
	t.Helper()
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(t, err)
	require.NoError(t, connection.Close())
}

func ticketJSON(id int64, status int, tags []string) map[string]any {
	if tags == nil {
		tags = []string{"billing"}
	}
	return map[string]any{
		"id": id, "subject": "Double charge on order 88213", "description": "<div>I was charged twice.</div>",
		"description_text": "I was charged twice.", "status": status, "priority": 2, "source": 1, "type": "Problem",
		"requester_id": 6007738334, "responder_id": 6001263404, "group_id": 156, "company_id": 2, "product_id": nil,
		"tags": tags, "is_escalated": false, "spam": false, "fr_escalated": false,
		"cc_emails": []string{"SENTINEL-cc@example.com"}, "custom_fields": map[string]any{"cf_note": "SENTINEL custom"},
		"created_at": "2026-01-26T14:02:00Z", "updated_at": "2026-01-28T08:45:00Z",
		"due_by": "2026-01-29T14:02:00Z", "fr_due_by": nil,
	}
}

func ticketBodyJSON(id int64, status int, tags []string) string {
	encoded, err := json.Marshal(ticketJSON(id, status, tags))
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// freshdeskDexContext is a dex.Context whose heartbeat reads the previous attempt's value, as Dex supplies it.
type freshdeskDexContext struct {
	context.Context
	step              string
	previousHeartbeat json.RawMessage
	recordedHeartbeat json.RawMessage
	heartbeatCount    int
	rejectsHeartbeat  bool
	attempt           int32
}

func newFreshdeskDexContext(step string) *freshdeskDexContext {
	return &freshdeskDexContext{Context: context.Background(), step: step, attempt: 1}
}

// nextAttempt is the following attempt of the same Step execution, which sees this attempt's last heartbeat.
func (context *freshdeskDexContext) nextAttempt() *freshdeskDexContext {
	return &freshdeskDexContext{
		Context: context.Context, step: context.step, previousHeartbeat: context.recordedHeartbeat, attempt: context.attempt + 1,
	}
}

func (*freshdeskDexContext) FlowID() string                          { return "freshdesk-flow" }
func (*freshdeskDexContext) RunID() string                           { return "run" }
func (*freshdeskDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (context *freshdeskDexContext) StepExecutionID() string         { return context.step }
func (*freshdeskDexContext) FromStepExecutionID() string             { return "" }
func (*freshdeskDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*freshdeskDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (context *freshdeskDexContext) Attempt() int32                  { return context.attempt }
func (*freshdeskDexContext) HasTimerFired() bool                     { return false }
func (*freshdeskDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*freshdeskDexContext) WaitForMethodFailed() bool               { return false }
func (*freshdeskDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*freshdeskDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*freshdeskDexContext) RecordEvent(string, any) error { return nil }

func (context *freshdeskDexContext) RecordHeartbeat(value any) error {
	if context.rejectsHeartbeat {
		return errors.New("heartbeat stream closed")
	}
	context.heartbeatCount++
	if value == nil {
		context.recordedHeartbeat = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.recordedHeartbeat = encoded
	return nil
}

func (context *freshdeskDexContext) GetLastHeartbeatValue(target any) (bool, error) {
	if context.previousHeartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(context.previousHeartbeat, target)
}

var _ dex.Context = (*freshdeskDexContext)(nil)
