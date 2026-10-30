// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const driveTestToken = "drive-token-SENTINEL"

var driveConnection = sdkgo.ConnectionRef{Provider: "google", Name: "drive-files"}

// recordedDriveRequest is one request the fake Drive server received.
type recordedDriveRequest struct {
	method string
	path   string
	query  map[string][]string
	header http.Header
	body   []byte
}

// fakeDrive serves one handler per test and records every request.
type fakeDrive struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedDriveRequest
}

func newFakeDrive(t *testing.T, handler func(response http.ResponseWriter, request *http.Request, body []byte)) *fakeDrive {
	t.Helper()
	fake := &fakeDrive{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(response, "unreadable request", http.StatusBadRequest)
			return
		}
		fake.mutex.Lock()
		fake.requests = append(fake.requests, recordedDriveRequest{
			method: request.Method, path: request.URL.Path, query: request.URL.Query(), header: request.Header.Clone(), body: body,
		})
		fake.mutex.Unlock()
		response.Header().Set("X-Goog-Request-Id", "google-request")
		handler(response, request, body)
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (fake *fakeDrive) recorded() []recordedDriveRequest {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]recordedDriveRequest(nil), fake.requests...)
}

func (fake *fakeDrive) countRequests(method string, path string) int {
	count := 0
	for _, request := range fake.recorded() {
		if request.method == method && request.path == path {
			count++
		}
	}
	return count
}

func newDriveClient(t *testing.T, endpoint string, config ...drive.Config) *drive.Client {
	t.Helper()
	clientConfig := drive.Config{}
	if len(config) == 1 {
		clientConfig = config[0]
	}
	clientConfig.Endpoint = endpoint
	client, err := drive.New(clientConfig, sdkgo.StaticCredentialProvider[drive.Credentials]{
		driveConnection: {AccessToken: sdkgo.NewSecretString(driveTestToken)},
	})
	require.NoError(t, err)
	return client
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err := response.Write([]byte(body))
	require.NoError(t, err)
}

type dexContext struct {
	context.Context
	step string
}

func newDexContext(step string) *dexContext {
	return &dexContext{Context: context.Background(), step: step}
}
func (*dexContext) FlowID() string                                  { return "drive-flow" }
func (*dexContext) RunID() string                                   { return "run" }
func (*dexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *dexContext) StepExecutionID() string                 { return context.step }
func (*dexContext) FromStepExecutionID() string                     { return "" }
func (*dexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*dexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*dexContext) Attempt() int32                                  { return 1 }
func (*dexContext) HasTimerFired() bool                             { return false }
func (*dexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*dexContext) WaitForMethodFailed() bool                       { return false }
func (*dexContext) RecordHeartbeat(any) error                       { return nil }
func (*dexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*dexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*dexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*dexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*dexContext)(nil)
