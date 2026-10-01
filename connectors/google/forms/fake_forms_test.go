// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package forms_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/forms"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const formsTestToken = "forms-token-SENTINEL"

var formsConnection = sdkgo.ConnectionRef{Provider: "google", Name: "google-forms-intake"}

// recordedFormsRequest is one request the fake Google Forms server received.
type recordedFormsRequest struct {
	method string
	path   string
	query  map[string][]string
	header http.Header
}

// fakeForms serves one handler per test and records every request.
type fakeForms struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedFormsRequest
}

func newFakeForms(t *testing.T, handler func(response http.ResponseWriter, request *http.Request)) *fakeForms {
	t.Helper()
	fake := &fakeForms{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fake.mutex.Lock()
		fake.requests = append(fake.requests, recordedFormsRequest{
			method: request.Method, path: request.URL.Path, query: request.URL.Query(), header: request.Header.Clone(),
		})
		fake.mutex.Unlock()
		response.Header().Set("X-Goog-Request-Id", "google-request")
		handler(response, request)
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (fake *fakeForms) recorded() []recordedFormsRequest {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]recordedFormsRequest(nil), fake.requests...)
}

func newFormsClient(t *testing.T, endpoint string, config ...forms.Config) *forms.Client {
	t.Helper()
	clientConfig := forms.Config{}
	if len(config) == 1 {
		clientConfig = config[0]
	}
	clientConfig.Endpoint = endpoint
	client, err := forms.New(clientConfig, sdkgo.StaticCredentialProvider[forms.Credentials]{
		formsConnection: {AccessToken: sdkgo.NewSecretString(formsTestToken)},
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

// googleError is Google's canonical error envelope; SENTINEL text must never reach a Failure.
func googleError(code int, status string, reason string) string {
	return fmt.Sprintf(`{"error":{"code":%d,"message":"SENTINEL provider detail","status":%q,`+
		`"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":%q,"domain":"googleapis.com"}]}}`, code, status, reason)
}

type rejectionRefreshingCredentialProvider struct {
	mutex           sync.Mutex
	forcedRefreshes int
}

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (forms.Credentials, error) {
	return forms.Credentials{AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context,
	sdkgo.Call,
	sdkgo.CredentialRefreshDriver[forms.Credentials],
) (forms.Credentials, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.forcedRefreshes++
	return forms.Credentials{AccessToken: sdkgo.NewSecretString("replacement-token")}, nil
}

type dexContext struct {
	context.Context
	step string
}

func newDexContext(step string) *dexContext {
	return &dexContext{Context: context.Background(), step: step}
}
func (*dexContext) FlowID() string                                  { return "forms-flow" }
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
