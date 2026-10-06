// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias_test

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
	"github.com/superdurable/dex-connectors-library/connectors/gorgias"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testDomain = "acme"
	testEmail  = "agent@acme.example.com"
	testAPIKey = "gorgiasTestKey0123456789abcdef"
)

var gorgiasConnection = sdkgo.ConnectionRef{Provider: "gorgias", Name: "gorgias-test"}

// recordedRequest is one request a recordingGorgias received.
type recordedRequest struct {
	method string
	path   string
	query  map[string][]string
	header http.Header
	body   string
}

// recordingGorgias is a credential-safe httptest server that records requests and delegates replies.
type recordingGorgias struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingGorgias(t *testing.T, reply func(http.ResponseWriter, *http.Request, int)) *recordingGorgias {
	t.Helper()
	provider := &recordingGorgias{}
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

func (provider *recordingGorgias) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingGorgias) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

func newGorgiasClient(t *testing.T, baseURL string) *gorgias.Client {
	t.Helper()
	return newGorgiasClientWithCredentials(t, baseURL, testCredentialProvider())
}

func newGorgiasClientWithCredentials(t *testing.T, baseURL string, credentials gorgias.CredentialSource) *gorgias.Client {
	t.Helper()
	if !strings.HasSuffix(baseURL, "/api") {
		baseURL += "/api"
	}
	client, err := gorgias.New(gorgias.Config{Domain: testDomain}, credentials, gorgias.WithAPIBaseURL(baseURL))
	require.NoError(t, err)
	return client
}

func testCredentialProvider() sdkgo.StaticCredentialProvider[gorgias.Credentials] {
	return sdkgo.StaticCredentialProvider[gorgias.Credentials]{
		gorgiasConnection: {Email: testEmail, APIKey: sdkgo.NewSecretString(testAPIKey)},
	}
}

// closedLoopbackURL returns an address nothing listens on, so a connection is refused before any request byte is sent.
func closedLoopbackURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return "http://" + address + "/api"
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	if response.Header().Get("X-Gorgias-Account-Api-Call-Limit") == "" {
		response.Header().Set("X-Gorgias-Account-Api-Call-Limit", "3/40")
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

// ticketJSON is a Gorgias ticket in the documented shape, with offset-less timestamps and SENTINEL extras.
func ticketJSON(id int64, status string, tags []string, messages []map[string]any) map[string]any {
	tagObjects := []map[string]any{}
	for index, tag := range tags {
		tagObjects = append(tagObjects, map[string]any{"id": index + 1, "name": tag, "decoration": map[string]any{"color": "#F58D86"}})
	}
	if messages == nil {
		messages = []map[string]any{}
	}
	return map[string]any{
		"id": id, "uri": "/api/tickets/1/", "external_id": "ERP-88213", "status": status, "priority": "normal", "channel": "email", "via": "email",
		"from_agent": false, "spam": false, "language": "en", "subject": "Double charge on order 88213",
		"customer":      map[string]any{"id": 3924, "email": "jane@acme.example.com", "name": "Jane Smith", "external_id": "cont_010", "note": "SENTINEL note"},
		"assignee_user": map[string]any{"id": 7, "email": "steve@acme.example.com", "name": "Steve"}, "assignee_team": nil,
		"tags": tagObjects, "messages": messages, "meta": map[string]any{"secret": "SENTINEL meta"},
		"created_datetime": "2026-01-26T14:02:00.384938", "updated_datetime": "2026-01-28T08:45:00.932637",
		"opened_datetime": nil, "last_received_message_datetime": "2026-01-27T10:00:00.000000", "last_message_datetime": "2026-01-27T10:05:00",
		"closed_datetime": nil, "snooze_datetime": nil, "trashed_datetime": nil, "satisfaction_survey": nil, "is_unread": true,
	}
}

func messageJSON(id int64, ticketID int64, channel string, fromAgent bool, created string, externalID any) map[string]any {
	return map[string]any{
		"id": id, "ticket_id": ticketID, "channel": channel, "via": "email", "public": channel != "internal-note", "from_agent": fromAgent,
		"sender": map[string]any{"id": 3924}, "subject": "Double charge on order 88213", "body_text": "Message " + created,
		"body_html": "<p>SENTINEL html</p>", "stripped_text": nil, "external_id": externalID,
		"source": map[string]any{"type": channel, "from": map[string]any{"address": "jane@acme.example.com"},
			"to": []map[string]any{{"address": "support@acme.example.com"}}},
		"created_datetime": created, "sent_datetime": created, "failed_datetime": nil,
	}
}

func encodeJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}

// gorgiasDexContext is a dex.Context whose heartbeat reads the previous attempt's value, as Dex supplies it.
type gorgiasDexContext struct {
	context.Context
	step              string
	previousHeartbeat json.RawMessage
	recordedHeartbeat json.RawMessage
	heartbeatCount    int
	rejectsHeartbeat  bool
	attempt           int32
}

func newGorgiasDexContext(step string) *gorgiasDexContext {
	return &gorgiasDexContext{Context: context.Background(), step: step, attempt: 1}
}

// newMarkedDexContext is a later attempt whose earlier attempt recorded the dispatch marker.
func newMarkedDexContext(step string, attempt int32) *gorgiasDexContext {
	return &gorgiasDexContext{
		Context: context.Background(), step: step, attempt: attempt, previousHeartbeat: json.RawMessage(`{"gorgiasDispatchedCallId":"earlier"}`),
	}
}

// nextAttempt is the following attempt of the same Step execution, which sees this attempt's last heartbeat.
func (context *gorgiasDexContext) nextAttempt() *gorgiasDexContext {
	return &gorgiasDexContext{
		Context: context.Context, step: context.step, previousHeartbeat: context.recordedHeartbeat, attempt: context.attempt + 1,
	}
}

func (*gorgiasDexContext) FlowID() string                          { return "gorgias-flow" }
func (*gorgiasDexContext) RunID() string                           { return "run" }
func (*gorgiasDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (context *gorgiasDexContext) StepExecutionID() string         { return context.step }
func (*gorgiasDexContext) FromStepExecutionID() string             { return "" }
func (*gorgiasDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*gorgiasDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (context *gorgiasDexContext) Attempt() int32                  { return context.attempt }
func (*gorgiasDexContext) HasTimerFired() bool                     { return false }
func (*gorgiasDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*gorgiasDexContext) WaitForMethodFailed() bool               { return false }
func (*gorgiasDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*gorgiasDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*gorgiasDexContext) RecordEvent(string, any) error { return nil }

func (context *gorgiasDexContext) RecordHeartbeat(value any) error {
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

func (context *gorgiasDexContext) GetLastHeartbeatValue(target any) (bool, error) {
	if context.previousHeartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(context.previousHeartbeat, target)
}

var _ dex.Context = (*gorgiasDexContext)(nil)
