// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAPIKey    = "0123456789abcdef0123456789abcdef"
	testToken     = "ATTAtrelloTestToken0123456789abcdef"
	testCardID    = "6512f0a1c2d3e4f5a6b7c8d9"
	testBoardID   = "6512f0a1c2d3e4f5a6b70001"
	testListID    = "6512f0a1c2d3e4f5a6b70101"
	otherListID   = "6512f0a1c2d3e4f5a6b70102"
	testLabelID   = "6512f0a1c2d3e4f5a6b70201"
	otherLabelID  = "6512f0a1c2d3e4f5a6b70202"
	testMemberID  = "6512f0a1c2d3e4f5a6b70301"
	testActionID  = "6512f0a1c2d3e4f5a6b70401"
	testRequestID = "3f6c2a8e-9b1d-4c7a-8e2f-5d4b3a2c1e0f"
)

var trelloConnection = sdkgo.ConnectionRef{Provider: "trello", Name: "trello-test"}

// expectedAuthorization is Trello's documented key and token header for the test credentials.
var expectedAuthorization = `OAuth oauth_consumer_key="` + testAPIKey + `", oauth_token="` + testToken + `"`

type recordedRequest struct {
	method        string
	path          string
	rawQuery      string
	authorization string
	contentType   string
	header        http.Header
	body          string
}

// recordingTrello is a credential-safe Trello fake whose handler sees each request and its index.
type recordingTrello struct {
	*httptest.Server
	t        *testing.T
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingTrello(t *testing.T, handler func(http.ResponseWriter, *http.Request, int)) *recordingTrello {
	t.Helper()
	provider := &recordingTrello{t: t}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		contents, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		request.Body = io.NopCloser(bytes.NewReader(contents))
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recordedRequest{
			method: request.Method, path: request.URL.Path, rawQuery: request.URL.RawQuery,
			authorization: request.Header.Get("Authorization"), contentType: request.Header.Get("Content-Type"),
			header: request.Header.Clone(), body: string(contents),
		})
		provider.mutex.Unlock()
		handler(response, request, index)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingTrello) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingTrello) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Greater(provider.t, len(provider.requests), index, "request %d was not sent", index)
	return provider.requests[index]
}

// writeJSON answers like Trello, including the atl-request-id header Trello returns on every response.
func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Atl-Request-Id", testRequestID)
	response.WriteHeader(status)
	_, err := io.WriteString(response, body)
	require.NoError(t, err)
}

// writeText answers with a plain-text error body, the way Trello reports most errors.
func writeText(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(status)
	_, err := io.WriteString(response, body)
	require.NoError(t, err)
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

// closedLoopbackURL returns an address nothing listens on, so a connection is refused before any byte is sent.
func closedLoopbackURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return "http://" + address + "/1"
}

// cardJSON renders one card the way Trello returns it with the connector's fields.
type cardJSONOptions struct {
	id       string
	name     string
	listID   string
	isClosed bool
	due      string
	labelIDs []string
	members  bool
}

func cardJSON(options cardJSONOptions) string {
	if options.id == "" {
		options.id = testCardID
	}
	if options.listID == "" {
		options.listID = testListID
	}
	due := "null"
	if options.due != "" {
		due = fmt.Sprintf("%q", options.due)
	}
	labelIDs, err := json.Marshal(append([]string{}, options.labelIDs...))
	if err != nil {
		panic(err)
	}
	labels := make([]string, 0, len(options.labelIDs))
	for _, labelID := range options.labelIDs {
		labels = append(labels, fmt.Sprintf(`{"id":%q,"idBoard":%q,"name":"Compliance","color":"green"}`, labelID, testBoardID))
	}
	members := ""
	if options.members {
		members = fmt.Sprintf(`,"members":[{"id":%q,"fullName":"Ada Lovelace","username":"ada","email":"SENTINEL@example.com"}],"list":{"id":%q,"name":"Approved"}`, testMemberID, options.listID)
	}
	return fmt.Sprintf(`{"id":%q,"shortLink":"LrrmgFyd","name":%q,"desc":"Badge reader at door 4 is offline.","idBoard":%q,"idList":%q,`+
		`"closed":%t,"due":%s,"dueComplete":false,"start":null,"idLabels":%s,"labels":[%s],"idMembers":[%q],"pos":16384,`+
		`"url":"https://trello.com/c/LrrmgFyd/19-replace-badge-reader","shortUrl":"https://trello.com/c/LrrmgFyd",`+
		`"dateLastActivity":"2026-09-30T16:15:00.000Z"%s}`,
		options.id, options.name, testBoardID, options.listID, options.isClosed, due, labelIDs, joinComma(labels), testMemberID, members)
}

func joinComma(values []string) string {
	var buffer bytes.Buffer
	for index, value := range values {
		if index > 0 {
			buffer.WriteByte(',')
		}
		buffer.WriteString(value)
	}
	return buffer.String()
}

func newTrelloClient(t *testing.T, endpoint string, options ...trello.Option) *trello.Client {
	t.Helper()
	client, err := trello.New(trello.Config{Endpoint: endpoint}, staticTrelloCredentials(), options...)
	require.NoError(t, err)
	return client
}

func staticTrelloCredentials() sdkgo.StaticCredentialProvider[trello.Credentials] {
	return sdkgo.StaticCredentialProvider[trello.Credentials]{trelloConnection: {
		APIKey: sdkgo.NewSecretString(testAPIKey), Token: sdkgo.NewSecretString(testToken),
	}}
}

// requireNoSentinel proves a Result carries neither provider message text nor a credential.
func requireNoSentinel(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
	require.NotContains(t, string(encoded), testAPIKey)
	require.NotContains(t, string(encoded), testToken)
}

func requireRetry(t *testing.T, err error, kind sdkgo.FailureKind) *sdkgo.RetryError {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, kind, retry.Failure.Kind)
	require.NotContains(t, retry.Failure.Message, "SENTINEL")
	return retry
}

// trelloDexContext is a dex.Context whose heartbeat reads the previous attempt's value, as Dex supplies it.
type trelloDexContext struct {
	context.Context
	step              string
	previousHeartbeat json.RawMessage
	recordedHeartbeat json.RawMessage
	heartbeatCount    int
	rejectsHeartbeat  bool
	attempt           int32
}

func newTrelloDexContext(step string) *trelloDexContext {
	return &trelloDexContext{Context: context.Background(), step: step, attempt: 1}
}

// nextAttempt is the following attempt of the same Step execution, which sees this attempt's last heartbeat.
func (context *trelloDexContext) nextAttempt() *trelloDexContext {
	return &trelloDexContext{
		Context: context.Context, step: context.step, previousHeartbeat: context.recordedHeartbeat, attempt: context.attempt + 1,
	}
}

func (*trelloDexContext) FlowID() string                                  { return "trello-flow" }
func (*trelloDexContext) RunID() string                                   { return "run" }
func (*trelloDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *trelloDexContext) StepExecutionID() string                 { return context.step }
func (*trelloDexContext) FromStepExecutionID() string                     { return "" }
func (*trelloDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*trelloDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (context *trelloDexContext) Attempt() int32                          { return context.attempt }
func (*trelloDexContext) HasTimerFired() bool                             { return false }
func (*trelloDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*trelloDexContext) WaitForMethodFailed() bool                       { return false }
func (*trelloDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*trelloDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*trelloDexContext) RecordEvent(string, any) error                   { return nil }

func (context *trelloDexContext) RecordHeartbeat(value any) error {
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

func (context *trelloDexContext) GetLastHeartbeatValue(target any) (bool, error) {
	if context.previousHeartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(context.previousHeartbeat, target)
}

var _ dex.Context = (*trelloDexContext)(nil)
