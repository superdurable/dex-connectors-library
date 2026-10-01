// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const docsTestToken = "docs-token-SENTINEL"

var docsConnection = sdkgo.ConnectionRef{Provider: "google", Name: "policy-docs"}

func newDocsClient(t *testing.T, endpoint string, config ...docs.Config) *docs.Client {
	t.Helper()
	clientConfig := docs.Config{}
	if len(config) == 1 {
		clientConfig = config[0]
	}
	clientConfig.DocsEndpoint, clientConfig.DriveEndpoint = endpoint, endpoint
	client, err := docs.New(clientConfig, sdkgo.StaticCredentialProvider[docs.Credentials]{
		docsConnection: {AccessToken: sdkgo.NewSecretString(docsTestToken)},
	})
	require.NoError(t, err)
	return client
}

// recordedRequest is one request a recording server received.
type recordedRequest struct {
	method string
	path   string
	query  map[string][]string
	header http.Header
	body   []byte
}

// recordingServer answers every request with one handler and records it.
type recordingServer struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingServer(t *testing.T, handler func(response http.ResponseWriter, request recordedRequest)) *recordingServer {
	t.Helper()
	server := &recordingServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(response, "unreadable request", http.StatusBadRequest)
			return
		}
		recorded := recordedRequest{method: request.Method, path: request.URL.Path, query: request.URL.Query(), header: request.Header.Clone(), body: body}
		server.mutex.Lock()
		server.requests = append(server.requests, recorded)
		server.mutex.Unlock()
		response.Header().Set("X-Goog-Request-Id", "google-request")
		handler(response, recorded)
	}))
	t.Cleanup(server.Close)
	return server
}

func (server *recordingServer) recorded() []recordedRequest {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return append([]recordedRequest(nil), server.requests...)
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err := response.Write([]byte(body))
	require.NoError(t, err)
}

// heartbeatDexContext is a dex.Context whose heartbeat reads the previous attempt's value, as Dex supplies it.
type heartbeatDexContext struct {
	context.Context
	step              string
	previousHeartbeat json.RawMessage
	recordedHeartbeat json.RawMessage
	heartbeatCount    int
	rejectsHeartbeat  bool
	attempt           int32
}

func newDexContext(step string) *heartbeatDexContext {
	return &heartbeatDexContext{Context: context.Background(), step: step, attempt: 1}
}

// nextAttempt is the following attempt of the same Step execution, which sees this attempt's last heartbeat.
func (ctx *heartbeatDexContext) nextAttempt() *heartbeatDexContext {
	return &heartbeatDexContext{Context: ctx.Context, step: ctx.step, previousHeartbeat: ctx.recordedHeartbeat, attempt: ctx.attempt + 1}
}

func (*heartbeatDexContext) FlowID() string                          { return "docs-flow" }
func (*heartbeatDexContext) RunID() string                           { return "run" }
func (*heartbeatDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (ctx *heartbeatDexContext) StepExecutionID() string             { return ctx.step }
func (*heartbeatDexContext) FromStepExecutionID() string             { return "" }
func (*heartbeatDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*heartbeatDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (ctx *heartbeatDexContext) Attempt() int32                      { return ctx.attempt }
func (*heartbeatDexContext) HasTimerFired() bool                     { return false }
func (*heartbeatDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*heartbeatDexContext) WaitForMethodFailed() bool               { return false }
func (*heartbeatDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*heartbeatDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*heartbeatDexContext) RecordEvent(string, any) error { return nil }

func (ctx *heartbeatDexContext) RecordHeartbeat(value any) error {
	if ctx.rejectsHeartbeat {
		return errors.New("heartbeat stream closed")
	}
	ctx.heartbeatCount++
	if value == nil {
		ctx.recordedHeartbeat = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	ctx.recordedHeartbeat = encoded
	return nil
}

func (ctx *heartbeatDexContext) GetLastHeartbeatValue(target any) (bool, error) {
	if ctx.previousHeartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(ctx.previousHeartbeat, target)
}

var _ dex.Context = (*heartbeatDexContext)(nil)

func requireRetry(t *testing.T, err error, kind sdkgo.FailureKind) {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, kind, retry.Failure.Kind)
}

func requireSecretFree(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), docsTestToken)
	require.NotContains(t, string(encoded), "SENTINEL")
}
