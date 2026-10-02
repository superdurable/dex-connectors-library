// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel_test

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
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	excelTestToken  = "excel-token-SENTINEL"
	testDriveID     = "b!driveKey-123_abc"
	testWorkbookID  = "01BYE5RZ6QN3ZWBTUFOFD3GSPGOHDJD36K"
	testWorkbookURL = "/v1.0/drives/" + testDriveID + "/items/" + testWorkbookID + "/workbook"
	testRequestID   = "11111111-2222-4333-8444-555555555555"
)

var excelConnection = sdkgo.ConnectionRef{Provider: "microsoft", Name: "approvals"}

func newExcelClient(t *testing.T, providerURL string, config ...excel.Config) *excel.Client {
	t.Helper()
	clientConfig := excel.Config{}
	if len(config) == 1 {
		clientConfig = config[0]
	}
	client, err := excel.New(clientConfig, sdkgo.StaticCredentialProvider[excel.Credentials]{
		excelConnection: {AuthMethodID: excel.OAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(excelTestToken)},
	}, excel.WithLocalProviderURL(providerURL), excel.WithClock(func() time.Time { return time.Unix(1_790_000_000, 0) }))
	require.NoError(t, err)
	return client
}

// recordedRequest is one request a recording server received.
type recordedRequest struct {
	method   string
	path     string
	rawQuery string
	header   http.Header
	body     []byte
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
		recorded := recordedRequest{
			method: request.Method, path: request.URL.Path, rawQuery: request.URL.RawQuery,
			header: request.Header.Clone(), body: body,
		}
		server.mutex.Lock()
		server.requests = append(server.requests, recorded)
		server.mutex.Unlock()
		response.Header().Set("request-id", testRequestID)
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

// graphErrorBody is a Graph error whose message text must never reach a Failure.
func graphErrorBody(code string, secondLevelCode string) string {
	inner := ""
	if secondLevelCode != "" {
		inner = `,"innerError":{"code":"` + secondLevelCode + `","message":"SENTINEL inner"}`
	}
	return `{"error":{"code":"` + code + `","message":"SENTINEL provider message"` + inner + `}}`
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

func (*heartbeatDexContext) FlowID() string                          { return "excel-flow" }
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

func requireRetry(t *testing.T, err error, kind sdkgo.FailureKind) *sdkgo.RetryError {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, kind, retry.Failure.Kind)
	return retry
}

func requireRetryAfter(t *testing.T, err error, delay time.Duration) {
	t.Helper()
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, delay, retryAfter.After)
}

func requireSecretFree(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), excelTestToken)
	require.NotContains(t, string(encoded), "SENTINEL")
}
