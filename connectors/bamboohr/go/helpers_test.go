// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr_test

import (
	"context"
	"encoding/base64"
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
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testCompanyDomain = "acme"
	// testAPIKey is split so secret scanners do not mistake the 40-hex fixture for a real key.
	testAPIKey = "0123456789abcdef" + "0123456789abcdef01234567"
)

var bambooHRConnection = sdkgo.ConnectionRef{Provider: "bamboohr", Name: "bamboohr-test"}

// recordedRequest is one request a recordingBambooHR received.
type recordedRequest struct {
	method string
	path   string
	query  map[string][]string
	header http.Header
	body   string
}

// recordingBambooHR is a credential-safe httptest server that records requests and delegates replies.
type recordingBambooHR struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingBambooHR(t *testing.T, reply func(http.ResponseWriter, *http.Request, int)) *recordingBambooHR {
	t.Helper()
	provider := &recordingBambooHR{}
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

func (provider *recordingBambooHR) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingBambooHR) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

func newBambooHRClient(t *testing.T, baseURL string) *bamboohr.Client {
	t.Helper()
	if !strings.HasSuffix(baseURL, "/api/v1") {
		baseURL += "/api/v1"
	}
	client, err := bamboohr.New(bamboohr.Config{CompanyDomain: testCompanyDomain}, testCredentialProvider(), bamboohr.WithAPIBaseURL(baseURL))
	require.NoError(t, err)
	return client
}

func testCredentialProvider() sdkgo.StaticCredentialProvider[bamboohr.Credentials] {
	return sdkgo.StaticCredentialProvider[bamboohr.Credentials]{bambooHRConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
}

func expectedAuthorization() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(testAPIKey+":x"))
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
	response.WriteHeader(status)
	_, err := io.WriteString(response, body)
	require.NoError(t, err)
}

// writeBambooHRError answers like BambooHR: a status, the free-text diagnostic header, and an optional body.
func writeBambooHRError(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("X-BambooHR-Error-Message", "SENTINEL diagnostic text")
	writeJSON(t, response, status, body)
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

// requireNoLeak asserts a Result never carries provider message text or the API key.
func requireNoLeak(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
	require.NotContains(t, string(encoded), testAPIKey)
}

// bambooHRDexContext is a dex.Context whose heartbeat reads the previous attempt's value, as Dex supplies it.
type bambooHRDexContext struct {
	context.Context
	step              string
	previousHeartbeat json.RawMessage
	recordedHeartbeat json.RawMessage
	heartbeatCount    int
	rejectsHeartbeat  bool
	attempt           int32
}

func newBambooHRDexContext(step string) *bambooHRDexContext {
	return &bambooHRDexContext{Context: context.Background(), step: step, attempt: 1}
}

// nextAttempt is the following attempt of the same Step execution, which sees this attempt's last heartbeat.
func (context *bambooHRDexContext) nextAttempt() *bambooHRDexContext {
	return &bambooHRDexContext{
		Context: context.Context, step: context.step, previousHeartbeat: context.recordedHeartbeat, attempt: context.attempt + 1,
	}
}

func (*bambooHRDexContext) FlowID() string                          { return "bamboohr-flow" }
func (*bambooHRDexContext) RunID() string                           { return "run" }
func (*bambooHRDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (context *bambooHRDexContext) StepExecutionID() string         { return context.step }
func (*bambooHRDexContext) FromStepExecutionID() string             { return "" }
func (*bambooHRDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*bambooHRDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (context *bambooHRDexContext) Attempt() int32                  { return context.attempt }
func (*bambooHRDexContext) HasTimerFired() bool                     { return false }
func (*bambooHRDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*bambooHRDexContext) WaitForMethodFailed() bool               { return false }
func (*bambooHRDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*bambooHRDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*bambooHRDexContext) RecordEvent(string, any) error { return nil }

func (context *bambooHRDexContext) RecordHeartbeat(value any) error {
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

func (context *bambooHRDexContext) GetLastHeartbeatValue(target any) (bool, error) {
	if context.previousHeartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(context.previousHeartbeat, target)
}

var _ dex.Context = (*bambooHRDexContext)(nil)
