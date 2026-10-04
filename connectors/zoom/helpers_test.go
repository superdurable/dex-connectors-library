// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessToken = "zoom-test-access-token"

var zoomConnection = sdkgo.ConnectionRef{Provider: "zoom", Name: "zoom-test"}

// futureStart is far enough ahead that every test's start time is in the future.
var futureStart = time.Date(2036, time.February, 24, 17, 0, 0, 0, time.UTC)

type recordedRequest struct {
	method        string
	path          string
	query         map[string][]string
	authorization string
	headers       http.Header
	body          []byte
}

// recordingProvider is a credential-safe Zoom fake that records every request.
type recordingProvider struct {
	*httptest.Server
	t        *testing.T
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingProvider(t *testing.T, handler func(http.ResponseWriter, *http.Request, int)) *recordingProvider {
	t.Helper()
	provider := &recordingProvider{t: t}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recordedRequest{
			method: request.Method, path: request.URL.Path, query: request.URL.Query(),
			authorization: request.Header.Get("Authorization"), headers: request.Header.Clone(), body: body,
		})
		provider.mutex.Unlock()
		handler(response, request, index)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingProvider) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingProvider) request(t *testing.T, index int) recordedRequest {
	t.Helper()
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Greater(t, len(provider.requests), index)
	return provider.requests[index]
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err := response.Write([]byte(body))
	require.NoError(t, err)
}

func decodeRequestBody(t *testing.T, request recordedRequest) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(request.body, &body))
	return body
}

// meetingJSON is a Zoom meeting object carrying secret-like fields the connector must drop.
func meetingJSON(meetingID int64, topic string, startTime string) string {
	return `{"id":` + jsonNumber(meetingID) + `,"uuid":"aDYlohsHRtCd4ii1uC2+hA==","host_id":"30R7kT7bTIKSNUFEuH_Qlg",` +
		`"host_email":"host@example.com","topic":"` + topic + `","type":2,"status":"waiting",` +
		`"start_time":"` + startTime + `","duration":30,"timezone":"America/Los_Angeles","agenda":"Kickoff",` +
		`"created_at":"2026-09-30T17:04:05Z","join_url":"https://us05web.zoom.us/j/` + jsonNumber(meetingID) + `?pwd=join",` +
		`"start_url":"https://us05web.zoom.us/s/1?zak=SENTINEL-start-url","password":"SENTINEL-passcode",` +
		`"h323_password":"SENTINEL-h323","pstn_password":"SENTINEL-pstn","encrypted_password":"SENTINEL-encrypted",` +
		`"settings":{"host_video":true,"participant_video":false,"join_before_host":false,"mute_upon_entry":true,` +
		`"waiting_room":true,"auto_recording":"none","alternative_hosts":"SENTINEL-alternate@example.com"}}`
}

func jsonNumber(value int64) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func newZoomClient(t *testing.T, endpoint string) *zoom.Client {
	t.Helper()
	client, err := zoom.New(zoom.Config{Endpoint: endpoint}, staticZoomCredentials())
	require.NoError(t, err)
	return client
}

func staticZoomCredentials() sdkgo.StaticCredentialProvider[zoom.Credentials] {
	return sdkgo.StaticCredentialProvider[zoom.Credentials]{zoomConnection: {AccessToken: sdkgo.NewSecretString(testAccessToken)}}
}

// rejectionRefreshingCredentialProvider returns a rejected token until one forced refresh replaces it.
type rejectionRefreshingCredentialProvider struct {
	forcedRefreshes int
}

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (zoom.Credentials, error) {
	return zoom.Credentials{AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

// ResolveWithRefresh returns the token Resolve returns, which has not expired before Zoom rejects it.
func (provider *rejectionRefreshingCredentialProvider) ResolveWithRefresh(
	_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[zoom.Credentials],
) (zoom.Credentials, error) {
	return provider.Resolve(call)
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context, sdkgo.Call, sdkgo.CredentialRefreshDriver[zoom.Credentials],
) (zoom.Credentials, error) {
	provider.forcedRefreshes++
	return zoom.Credentials{AccessToken: sdkgo.NewSecretString("replacement-token")}, nil
}

// failingCredentialProvider fails every resolution with err, as a refresh outage or revoked grant does.
type failingCredentialProvider struct {
	err error
}

func (provider failingCredentialProvider) Resolve(sdkgo.Call) (zoom.Credentials, error) {
	return zoom.Credentials{}, provider.err
}

func (provider failingCredentialProvider) ResolveWithRefresh(
	context.Context, sdkgo.Call, sdkgo.CredentialRefreshDriver[zoom.Credentials],
) (zoom.Credentials, error) {
	return zoom.Credentials{}, provider.err
}

// zoomDexContext is a deterministic Dex context whose heartbeat value persists like a regular attempt's.
type zoomDexContext struct {
	context.Context
	step           string
	mutex          sync.Mutex
	heartbeatValue json.RawMessage
	heartbeats     int
}

func newZoomDexContext(step string) *zoomDexContext {
	return &zoomDexContext{Context: context.Background(), step: step}
}

func (*zoomDexContext) FlowID() string                          { return "zoom-flow" }
func (*zoomDexContext) RunID() string                           { return "run" }
func (*zoomDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (ctx *zoomDexContext) StepExecutionID() string             { return ctx.step }
func (*zoomDexContext) FromStepExecutionID() string             { return "" }
func (*zoomDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*zoomDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (*zoomDexContext) Attempt() int32                          { return 1 }
func (*zoomDexContext) HasTimerFired() bool                     { return false }
func (*zoomDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*zoomDexContext) WaitForMethodFailed() bool               { return false }
func (*zoomDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*zoomDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*zoomDexContext) RecordEvent(string, any) error { return nil }

// RecordHeartbeat stores the value as Dex would hand it to the next regular attempt; nil clears it.
func (ctx *zoomDexContext) RecordHeartbeat(value any) error {
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	ctx.heartbeats++
	if value == nil {
		ctx.heartbeatValue = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	ctx.heartbeatValue = encoded
	return nil
}

func (ctx *zoomDexContext) GetLastHeartbeatValue(valuePointer any) (bool, error) {
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	if ctx.heartbeatValue == nil {
		return false, nil
	}
	return true, json.Unmarshal(ctx.heartbeatValue, valuePointer)
}

func (ctx *zoomDexContext) lastHeartbeat() json.RawMessage {
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	return ctx.heartbeatValue
}

var _ dex.Context = (*zoomDexContext)(nil)
