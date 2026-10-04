// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze_test

import (
	"bytes"
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
	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testBrand    = "acme"
	testEmail    = "agent@acme.example.com"
	testAPIToken = "reamazeTestToken0123456789"
)

var reamazeConnection = sdkgo.ConnectionRef{Provider: "reamaze", Name: "reamaze-test"}

// recordedRequest is one request a recordingReamaze received.
type recordedRequest struct {
	method string
	path   string
	query  map[string][]string
	header http.Header
	body   string
}

// recordingReamaze is a credential-safe httptest server that records requests and delegates replies.
type recordingReamaze struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingReamaze(t *testing.T, reply func(http.ResponseWriter, *http.Request, int)) *recordingReamaze {
	t.Helper()
	provider := &recordingReamaze{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		request.Body = io.NopCloser(bytes.NewReader(body))
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

func (provider *recordingReamaze) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingReamaze) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

func newReamazeClient(t *testing.T, baseURL string) *reamaze.Client {
	t.Helper()
	if !strings.HasSuffix(baseURL, "/api/v1") {
		baseURL += "/api/v1"
	}
	client, err := reamaze.New(reamaze.Config{Brand: testBrand}, testCredentialProvider(), reamaze.WithAPIBaseURL(baseURL))
	require.NoError(t, err)
	return client
}

func testCredentialProvider() sdkgo.StaticCredentialProvider[reamaze.Credentials] {
	return sdkgo.StaticCredentialProvider[reamaze.Credentials]{reamazeConnection: {Email: testEmail, APIToken: sdkgo.NewSecretString(testAPIToken)}}
}

// switchingCredentialProvider resolves the test credentials until a test sets resolveErr.
type switchingCredentialProvider struct {
	resolveErr error
}

func (provider *switchingCredentialProvider) Resolve(call sdkgo.Call) (reamaze.Credentials, error) {
	if provider.resolveErr != nil {
		return reamaze.Credentials{}, provider.resolveErr
	}
	return testCredentialProvider().Resolve(call)
}

func newSwitchingReamazeClient(t *testing.T, baseURL string) (*reamaze.Client, *switchingCredentialProvider) {
	t.Helper()
	credentials := &switchingCredentialProvider{}
	client, err := reamaze.New(reamaze.Config{Brand: testBrand}, credentials, reamaze.WithAPIBaseURL(baseURL+"/api/v1"))
	require.NoError(t, err)
	return client, credentials
}

// dispatchKeyOf is the key a single-dispatch write carries, derived from its Call ID.
func dispatchKeyOf(callID sdkgo.CallID) string {
	return "dex-" + string(callID)
}

// closedLoopbackURL returns an address nothing listens on, so a connection is refused before any request byte is sent.
func closedLoopbackURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return "http://" + address + "/api/v1"
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

func conversationJSON(slug string, status int, tags []string) map[string]any {
	if tags == nil {
		tags = []string{"billing"}
	}
	return map[string]any{
		"subject": "Double charge on order 88213", "slug": slug, "status": status, "created_at": "2026-01-26T14:02:00.123-08:00",
		"tag_list": tags, "message": map[string]any{"body": "I was charged twice."},
		"last_customer_message": map[string]any{"body": "SENTINEL customer text", "created_at": "2026-01-27T10:00:00.000Z"},
		"last_staff_message":    map[string]any{"body": "SENTINEL staff text", "created_at": "2026-01-27T11:00:00.000Z"},
		"author":                map[string]any{"name": "Jane Smith", "email": "jane@acme.example.com"},
		"assignee":              nil,
		"category":              map[string]any{"name": "Support", "slug": "support", "email": "support@acme.example.com", "channel": 1},
		"data":                  map[string]any{"order": "SENTINEL data"},
		"followers":             []any{map[string]any{"name": "Jane Smith", "email": "jane@acme.example.com"}},
	}
}

func messageJSON(body string, visibility int, originID any) map[string]any {
	return map[string]any{
		"body": body, "visibility": visibility, "origin": 7, "origin_id": originID, "created_at": "2026-01-27T12:00:00.000Z",
		"user":       map[string]any{"name": "Agent", "email": testEmail},
		"recipients": []any{}, "attachments": []any{},
		"conversation": map[string]any{"subject": "Double charge on order 88213", "slug": "double-charge", "created_at": "2026-01-26T14:02:00.000Z"},
	}
}

func pageJSON(listName string, entries []any, pageCount int) map[string]any {
	return map[string]any{"page_size": 30, "page_count": pageCount, "total_count": len(entries), listName: entries}
}

// reamazeDexContext is a dex.Context whose heartbeat reads the previous attempt's value, as Dex supplies it.
type reamazeDexContext struct {
	context.Context
	step              string
	previousHeartbeat json.RawMessage
	recordedHeartbeat json.RawMessage
	heartbeatCount    int
	rejectsHeartbeat  bool
	attempt           int32
}

func newReamazeDexContext(step string) *reamazeDexContext {
	return &reamazeDexContext{Context: context.Background(), step: step, attempt: 1}
}

// nextAttempt sees the last stored heartbeat; Dex keeps it across an attempt that records none.
func (context *reamazeDexContext) nextAttempt() *reamazeDexContext {
	lastHeartbeat := context.previousHeartbeat
	if context.heartbeatCount > 0 {
		lastHeartbeat = context.recordedHeartbeat
	}
	return &reamazeDexContext{
		Context: context.Context, step: context.step, previousHeartbeat: lastHeartbeat, attempt: context.attempt + 1,
	}
}

func (*reamazeDexContext) FlowID() string                          { return "reamaze-flow" }
func (*reamazeDexContext) RunID() string                           { return "run" }
func (*reamazeDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (context *reamazeDexContext) StepExecutionID() string         { return context.step }
func (*reamazeDexContext) FromStepExecutionID() string             { return "" }
func (*reamazeDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*reamazeDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (context *reamazeDexContext) Attempt() int32                  { return context.attempt }
func (*reamazeDexContext) HasTimerFired() bool                     { return false }
func (*reamazeDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*reamazeDexContext) WaitForMethodFailed() bool               { return false }
func (*reamazeDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*reamazeDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*reamazeDexContext) RecordEvent(string, any) error { return nil }

func (context *reamazeDexContext) RecordHeartbeat(value any) error {
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

func (context *reamazeDexContext) GetLastHeartbeatValue(target any) (bool, error) {
	if context.previousHeartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(context.previousHeartbeat, target)
}

var _ dex.Context = (*reamazeDexContext)(nil)
