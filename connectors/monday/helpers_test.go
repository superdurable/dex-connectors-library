// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAPIToken = "mondayTestToken0123456789abcdefSENTINELKEY"
	testBoardID  = "1234567890"
	testItemID   = "9876543210"
)

var mondayConnection = sdkgo.ConnectionRef{Provider: "monday", Name: "monday-test"}

// recordedRequest is one GraphQL request a recordingMonday received.
type recordedRequest struct {
	method    string
	path      string
	header    http.Header
	query     string
	variables map[string]any
}

// recordingMonday is a credential-safe httptest server that records GraphQL requests and delegates replies.
type recordingMonday struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingMonday(t *testing.T, reply func(http.ResponseWriter, recordedRequest, int)) *recordingMonday {
	t.Helper()
	provider := &recordingMonday{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		require.NoError(t, json.Unmarshal(body, &document), "every request is a GraphQL JSON body")
		recorded := recordedRequest{
			method: request.Method, path: request.URL.Path, header: request.Header.Clone(), query: document.Query, variables: document.Variables,
		}
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recorded)
		provider.mutex.Unlock()
		reply(response, recorded, index)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingMonday) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingMonday) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

func newMondayClient(t *testing.T, apiURL string, options ...monday.Option) *monday.Client {
	t.Helper()
	client, err := monday.New(monday.Config{}, testCredentialProvider(), append([]monday.Option{monday.WithAPIURL(apiURL + "/v2")}, options...)...)
	require.NoError(t, err)
	return client
}

func testCredentialProvider() sdkgo.StaticCredentialProvider[monday.Credentials] {
	return sdkgo.StaticCredentialProvider[monday.Credentials]{mondayConnection: {
		AuthMethodID: monday.PersonalAPITokenAuthMethodID, APIToken: sdkgo.NewSecretString(testAPIToken),
	}}
}

// closedLoopbackURL returns an address nothing listens on, so a connection is refused before any byte is sent.
func closedLoopbackURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return "http://" + address
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_, err := io.WriteString(response, body)
	require.NoError(t, err)
}

func writeData(t *testing.T, response http.ResponseWriter, data any) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"data": data, "extensions": map[string]any{"request_id": "request-0001"}})
	require.NoError(t, err)
	writeJSON(t, response, http.StatusOK, string(encoded))
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

func itemJSON(id string, name string) map[string]any {
	return map[string]any{
		"id": id, "name": name, "state": "active", "created_at": "2026-01-28T10:00:00Z", "updated_at": "2026-01-28T10:05:00Z",
		"url": "https://acme.monday.com/boards/" + testBoardID + "/pulses/" + id, "creator_id": "48202303",
		"board": map[string]any{"id": testBoardID}, "group": map[string]any{"id": "topics", "title": "February"},
		"column_values": []any{
			map[string]any{"id": "status", "type": "status", "text": "Working on it", "value": `{"index":0,"post_id":null,"changed_at":"2026-01-28T10:00:00Z"}`},
			map[string]any{"id": "date4", "type": "date", "text": "2026-02-18", "value": `{"date":"2026-02-18","changed_at":"2026-01-28T10:00:00Z"}`},
			map[string]any{"id": "text", "type": "text", "text": nil, "value": nil},
		},
	}
}

// mondayDexContext is a minimal dex.Context for running one operation outside a Worker.
type mondayDexContext struct {
	context.Context
	step string
}

func newMondayDexContext(step string) *mondayDexContext {
	return &mondayDexContext{Context: context.Background(), step: step}
}

func (*mondayDexContext) FlowID() string                          { return "monday-flow" }
func (*mondayDexContext) RunID() string                           { return "run" }
func (*mondayDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (context *mondayDexContext) StepExecutionID() string         { return context.step }
func (*mondayDexContext) FromStepExecutionID() string             { return "" }
func (*mondayDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*mondayDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (*mondayDexContext) Attempt() int32                          { return 1 }
func (*mondayDexContext) HasTimerFired() bool                     { return false }
func (*mondayDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*mondayDexContext) WaitForMethodFailed() bool               { return false }
func (*mondayDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*mondayDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*mondayDexContext) RecordEvent(string, any) error           { return nil }
func (*mondayDexContext) RecordHeartbeat(any) error               { return nil }
func (*mondayDexContext) GetLastHeartbeatValue(any) (bool, error) { return false, nil }

var _ dex.Context = (*mondayDexContext)(nil)
