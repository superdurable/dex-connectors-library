// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAPIToken    = "pipedrive-unit-test-api-token"
	testAccessToken = "v1u:pipedrive-unit-test-access-token"
	// providerMessageSentinel appears only in fake provider message text, which must never reach a Failure.
	providerMessageSentinel = "SENTINEL provider message text"
	testCorrelationID       = "d853687f-f72e-4ed9-bbad-be98638737c8"
)

var pipedriveConnection = sdkgo.ConnectionRef{Provider: "pipedrive", Name: "pipedrive-crm"}

// recordingPipedrive is a fake Pipedrive API whose handler each test supplies.
type recordingPipedrive struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

type recordedRequest struct {
	method        string
	path          string
	query         map[string][]string
	apiToken      string
	authorization string
	contentType   string
	body          []byte
}

func newRecordingPipedrive(t *testing.T, handler func(http.ResponseWriter, recordedRequest)) *recordingPipedrive {
	t.Helper()
	provider := &recordingPipedrive{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read fake Pipedrive request body: %v", err)
		}
		recorded := recordedRequest{
			method: request.Method, path: request.URL.Path, query: request.URL.Query(),
			apiToken: request.Header.Get("X-Api-Token"), authorization: request.Header.Get("Authorization"),
			contentType: request.Header.Get("Content-Type"), body: body,
		}
		provider.mutex.Lock()
		provider.requests = append(provider.requests, recorded)
		provider.mutex.Unlock()
		handler(response, recorded)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingPipedrive) recordedRequests() []recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]recordedRequest(nil), provider.requests...)
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Correlation-Id", testCorrelationID)
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		t.Errorf("write fake Pipedrive response: %v", err)
	}
}

func writeRecord(t *testing.T, response http.ResponseWriter, record string) {
	t.Helper()
	writeJSON(t, response, http.StatusOK, `{"success":true,"data":`+record+`}`)
}

func writeError(t *testing.T, response http.ResponseWriter, status int) {
	t.Helper()
	writeJSON(t, response, status, `{"success":false,"error":"`+providerMessageSentinel+`","errorCode":`+strconv.Itoa(status)+`,"error_info":"`+providerMessageSentinel+`"}`)
}

func newAPITokenClient(t *testing.T, endpoint string, options ...pipedrive.Option) *pipedrive.Client {
	t.Helper()
	client, err := pipedrive.New(pipedrive.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[pipedrive.Credentials]{
		pipedriveConnection: {AuthMethodID: pipedrive.APITokenAuthMethodID, APIToken: sdkgo.NewSecretString(testAPIToken)},
	}, options...)
	require.NoError(t, err)
	return client
}

func oauthCredentials(apiDomain string) pipedrive.Credentials {
	return pipedrive.Credentials{
		AuthMethodID: pipedrive.OAuthAuthMethodID, OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("stored-refresh-token"), APIDomain: apiDomain,
	}
}

// tokenEndpointTransport serves Pipedrive's fixed OAuth token URL from a handler and sends every other request normally.
type tokenEndpointTransport struct {
	tokenHandler http.HandlerFunc
}

func (transport tokenEndpointTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme == "https" && request.URL.Host == "oauth.pipedrive.com" && request.URL.Path == "/oauth/token" {
		recorder := httptest.NewRecorder()
		transport.tokenHandler(recorder, request)
		return recorder.Result(), nil
	}
	if request.URL.Hostname() != "127.0.0.1" {
		return nil, errors.New("unit tests reach only the fake Pipedrive")
	}
	return http.DefaultTransport.RoundTrip(request)
}

// stepDexContext is a Step context whose heartbeat survives across attempts of one Step execution.
type stepDexContext struct {
	context.Context
	step      string
	mutex     sync.Mutex
	heartbeat json.RawMessage
}

func newStepDexContext(step string) *stepDexContext {
	return &stepDexContext{Context: context.Background(), step: step}
}

func (*stepDexContext) FlowID() string                                  { return "deal-intake-flow" }
func (*stepDexContext) RunID() string                                   { return "run" }
func (*stepDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (ctx *stepDexContext) StepExecutionID() string                     { return ctx.step }
func (*stepDexContext) FromStepExecutionID() string                     { return "" }
func (*stepDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*stepDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*stepDexContext) Attempt() int32                                  { return 1 }
func (*stepDexContext) HasTimerFired() bool                             { return false }
func (*stepDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*stepDexContext) WaitForMethodFailed() bool                       { return false }
func (*stepDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*stepDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*stepDexContext) RecordEvent(string, any) error                   { return nil }

func (ctx *stepDexContext) RecordHeartbeat(value any) error {
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	if value == nil {
		ctx.heartbeat = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	ctx.heartbeat = encoded
	return err
}

func (ctx *stepDexContext) GetLastHeartbeatValue(destination any) (bool, error) {
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	if ctx.heartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(ctx.heartbeat, destination)
}

func (ctx *stepDexContext) hasHeartbeat() bool {
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	return ctx.heartbeat != nil
}

var _ dex.Context = (*stepDexContext)(nil)
