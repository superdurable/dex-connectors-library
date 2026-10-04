// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hiver"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// testAPIKey is a fake key shaped like no real provider credential.
const testAPIKey = "hiverTestKey0123456789"

var hiverConnection = sdkgo.ConnectionRef{Provider: "hiver", Name: "hiver-test"}

// recordedRequest is one request a recordingHiver received.
type recordedRequest struct {
	at     time.Time
	method string
	path   string
	query  map[string][]string
	header http.Header
	body   string
}

// formFields decodes a multipart/form-data body into its text fields.
func (request recordedRequest) formFields(t *testing.T) map[string]string {
	t.Helper()
	mediaType, parameters, err := mime.ParseMediaType(request.header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	reader := multipart.NewReader(strings.NewReader(request.body), parameters["boundary"])
	fields := map[string]string{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return fields
		}
		require.NoError(t, err)
		value, err := io.ReadAll(part)
		require.NoError(t, err)
		fields[part.FormName()] = string(value)
	}
}

// recordingHiver is a credential-safe httptest server that records requests and delegates replies.
type recordingHiver struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingHiver(t *testing.T, reply func(http.ResponseWriter, *http.Request, int)) *recordingHiver {
	t.Helper()
	provider := &recordingHiver{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recordedRequest{
			at: time.Now(), method: request.Method, path: request.URL.Path, query: request.URL.Query(), header: request.Header.Clone(), body: string(body),
		})
		provider.mutex.Unlock()
		reply(response, request, index)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingHiver) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingHiver) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

// newHiverClient uses a one-millisecond request interval so unit tests do not wait for Hiver's one-second spacing.
func newHiverClient(t *testing.T, baseURL string) *hiver.Client {
	t.Helper()
	client, err := hiver.New(hiver.Config{RequestIntervalMilliseconds: 1}, testCredentialProvider(), hiver.WithAPIBaseURL(baseURL+"/v1"))
	require.NoError(t, err)
	return client
}

func testCredentialProvider() sdkgo.StaticCredentialProvider[hiver.Credentials] {
	return sdkgo.StaticCredentialProvider[hiver.Credentials]{hiverConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
}

// closedLoopbackURL returns an address nothing listens on, so a connection is refused before any request byte is sent.
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
	response.Header().Set("Content-Type", "application/json")
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

// cutOffBody sends a status line, then closes the connection partway through the declared body.
func cutOffBody(t *testing.T, response http.ResponseWriter, status int) {
	t.Helper()
	response.Header().Set("Content-Length", "100")
	response.WriteHeader(status)
	_, err := io.WriteString(response, `{"Message":`)
	require.NoError(t, err)
	dropConnection(t, response)
}

// conversationJSON is one conversation as Hiver's documentation shows it, with numeric IDs.
func conversationJSON(id string, status string, assigneeID string, tagIDs []string) string {
	assignee := `null`
	if assigneeID != "" {
		assignee = `{"assignee_type":"user","assignee_id":` + assigneeID + `}`
	}
	encodedTags, err := json.Marshal(tagIDs)
	if err != nil {
		panic(err)
	}
	return `{"id":` + id + `,"assignee":` + assignee + `,"status":"` + status + `","tag_ids":` + string(encodedTags) +
		`,"gmail_thread_id":"19cfee91188070f8","private_permalink":"https://v2.hiverhq.com/permalinks/pvt/201c00fa",` +
		`"public_permalink":"https://v2.hiverhq.com/permalinks/pub/SENTINEL","message_ids":[{"hiver_message_id":834466048,"gmail_message_id":"19cfee91188070f8"}]}`
}

// hiverDexContext is a dex.Context whose heartbeat reads the previous attempt's value, as Dex supplies it.
type hiverDexContext struct {
	context.Context
	step              string
	previousHeartbeat json.RawMessage
	recordedHeartbeat json.RawMessage
	heartbeatCount    int
	rejectsHeartbeat  bool
	// losesRecordedHeartbeat accepts a heartbeat that Dex never stores, as when the Worker loses its stream.
	losesRecordedHeartbeat bool
	attempt                int32
}

func newHiverDexContext(step string) *hiverDexContext {
	return &hiverDexContext{Context: context.Background(), step: step, attempt: 1}
}

// nextAttempt is the following attempt of the same Step execution, which sees this attempt's last heartbeat.
func (context *hiverDexContext) nextAttempt() *hiverDexContext {
	return &hiverDexContext{Context: context.Context, step: context.step, previousHeartbeat: context.recordedHeartbeat, attempt: context.attempt + 1}
}

func (*hiverDexContext) FlowID() string                          { return "hiver-flow" }
func (*hiverDexContext) RunID() string                           { return "run" }
func (*hiverDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (context *hiverDexContext) StepExecutionID() string         { return context.step }
func (*hiverDexContext) FromStepExecutionID() string             { return "" }
func (*hiverDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*hiverDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (context *hiverDexContext) Attempt() int32                  { return context.attempt }
func (*hiverDexContext) HasTimerFired() bool                     { return false }
func (*hiverDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*hiverDexContext) WaitForMethodFailed() bool               { return false }
func (*hiverDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*hiverDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*hiverDexContext) RecordEvent(string, any) error { return nil }

func (context *hiverDexContext) RecordHeartbeat(value any) error {
	if context.rejectsHeartbeat {
		return errors.New("heartbeat stream closed")
	}
	context.heartbeatCount++
	if context.losesRecordedHeartbeat {
		return nil
	}
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

func (context *hiverDexContext) GetLastHeartbeatValue(target any) (bool, error) {
	if context.previousHeartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(context.previousHeartbeat, target)
}

var _ dex.Context = (*hiverDexContext)(nil)
