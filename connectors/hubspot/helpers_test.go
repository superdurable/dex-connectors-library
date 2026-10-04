// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hubspot_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAccessToken = "pat-na1-test-token"
	// providerMessageSentinel appears only in fake provider message text, which must never reach a Failure.
	providerMessageSentinel = "SENTINEL provider message text"
)

var hubspotConnection = sdkgo.ConnectionRef{Provider: "hubspot", Name: "hubspot-crm"}

// recordingHubSpot is a credential-checking fake HubSpot API whose handler each test supplies.
type recordingHubSpot struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

type recordedRequest struct {
	method        string
	path          string
	query         string
	authorization string
	contentType   string
	body          []byte
}

func newRecordingHubSpot(t *testing.T, handler func(http.ResponseWriter, recordedRequest)) *recordingHubSpot {
	t.Helper()
	provider := &recordingHubSpot{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read fake HubSpot request body: %v", err)
		}
		recorded := recordedRequest{
			method: request.Method, path: request.URL.Path, query: request.URL.RawQuery,
			authorization: request.Header.Get("Authorization"), contentType: request.Header.Get("Content-Type"), body: body,
		}
		provider.mutex.Lock()
		provider.requests = append(provider.requests, recorded)
		provider.mutex.Unlock()
		handler(response, recorded)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingHubSpot) recordedRequests() []recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]recordedRequest(nil), provider.requests...)
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-HubSpot-Correlation-Id", "c033cdaa-2c40-4a64-ae48-b4cec88dad24")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		t.Errorf("write fake HubSpot response: %v", err)
	}
}

func newStaticTokenClient(t *testing.T, endpoint string, config hubspot.Config, options ...hubspot.Option) *hubspot.Client {
	t.Helper()
	config.Endpoint = endpoint
	client, err := hubspot.New(config, sdkgo.StaticCredentialProvider[hubspot.Credentials]{
		hubspotConnection: {AuthMethodID: hubspot.PrivateAppTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(testAccessToken)},
	}, options...)
	require.NoError(t, err)
	return client
}

// tokenEndpointTransport serves HubSpot's fixed OAuth token URL from a handler and sends every other request normally.
type tokenEndpointTransport struct {
	tokenHandler http.HandlerFunc
}

func (transport tokenEndpointTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme == "https" && request.URL.Host == "api.hubapi.com" && request.URL.Path == "/oauth/2026-09/token" {
		recorder := httptest.NewRecorder()
		transport.tokenHandler(recorder, request)
		return recorder.Result(), nil
	}
	return http.DefaultTransport.RoundTrip(request)
}

type stepDexContext struct {
	context.Context
	step string
}

func newStepDexContext(step string) *stepDexContext {
	return &stepDexContext{Context: context.Background(), step: step}
}

func (*stepDexContext) FlowID() string                                  { return "lead-flow" }
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
func (*stepDexContext) RecordHeartbeat(any) error                       { return nil }
func (*stepDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*stepDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*stepDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*stepDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*stepDexContext)(nil)
