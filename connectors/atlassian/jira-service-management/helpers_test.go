// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

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
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testCloudID         = "11223344-a1b2-4b33-8c44-def123456789"
	testAccessToken     = "jsm-access-token"
	testPlatformPrefix  = "/ex/jira/" + testCloudID + "/rest/api/3"
	testServiceDeskPath = "/ex/jira/" + testCloudID + "/rest/servicedeskapi"
	testCustomerAccount = "qm:a713c8ea-1075-4e30-9d96-891a7d181739:5ad6d69abfa3980ce712caae"
)

var jsmConnection = sdkgo.ConnectionRef{Provider: "atlassian", Name: "jsm-test"}

type recordedRequest struct {
	method        string
	path          string
	rawQuery      string
	authorization string
	experimental  string
	body          string
}

// recordingProvider is a credential-safe Jira Service Management fake whose handler sees each request and its index.
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
		contents, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		request.Body = io.NopCloser(bytes.NewReader(contents))
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recordedRequest{
			method: request.Method, path: request.URL.Path, rawQuery: request.URL.RawQuery,
			authorization: request.Header.Get("Authorization"), experimental: request.Header.Get("X-ExperimentalApi"), body: string(contents),
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

func (provider *recordingProvider) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Greater(provider.t, len(provider.requests), index, "request %d was not sent", index)
	return provider.requests[index]
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Arequestid", "request-1")
	response.WriteHeader(status)
	_, err := response.Write([]byte(body))
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
	return "http://" + address
}

func issueJSON(id string, key string, summary string, labels string) string {
	return fmt.Sprintf(`{"id":%q,"key":%q,"fields":{"summary":%q,`+
		`"status":{"id":"10001","name":"Waiting for support","statusCategory":{"key":"new"}},"project":{"id":"10000","key":"ITH"},`+
		`"assignee":{"accountId":"5b10ac8d82e05b22cc7d4ef5","displayName":"Ada","emailAddress":"SENTINEL-ada@example.com"},`+
		`"reporter":{"accountId":%q,"displayName":"Jane Smith","emailAddress":"SENTINEL-jane@example.com"},`+
		`"priority":{"id":"3","name":"Medium"},"labels":%s,`+
		`"created":"2026-09-30T09:15:00.000-0700","updated":"2026-09-30T10:00:00.000-0700",`+
		`"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[`+
		`{"type":"text","text":"My laptop will not boot."},{"type":"hardBreak"},{"type":"text","text":"Error 0x7B."}]}]}}}`,
		id, key, summary, testCustomerAccount, labels)
}

func newTestClient(t *testing.T, endpoint string, options ...jiraservicemanagement.Option) *jiraservicemanagement.Client {
	t.Helper()
	return newTestClientForSite(t, endpoint, testCloudID, options...)
}

func newTestClientForSite(t *testing.T, endpoint string, cloudID string, options ...jiraservicemanagement.Option) *jiraservicemanagement.Client {
	t.Helper()
	client, err := jiraservicemanagement.New(jiraservicemanagement.Config{CloudID: cloudID, Endpoint: endpoint}, staticTestCredentials(), options...)
	require.NoError(t, err)
	return client
}

func staticTestCredentials() sdkgo.StaticCredentialProvider[jiraservicemanagement.Credentials] {
	return sdkgo.StaticCredentialProvider[jiraservicemanagement.Credentials]{jsmConnection: {
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}}
}

// requireNoSentinel proves a Result carries neither provider message text nor the access token.
func requireNoSentinel(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
	require.NotContains(t, string(encoded), testAccessToken)
}

func requireRetry(t *testing.T, err error, kind sdkgo.FailureKind) *sdkgo.RetryError {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, kind, retry.Failure.Kind)
	require.NotContains(t, retry.Failure.Message, "SENTINEL")
	return retry
}

// rejectionRefreshingCredentialProvider hands out a replacement token after a provider rejection.
type rejectionRefreshingCredentialProvider struct {
	forcedRefreshes int
}

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (jiraservicemanagement.Credentials, error) {
	return jiraservicemanagement.Credentials{AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

// ResolveWithRefresh returns the token Resolve returns, which has not expired before the provider rejects it.
func (provider *rejectionRefreshingCredentialProvider) ResolveWithRefresh(
	_ context.Context,
	call sdkgo.Call,
	_ sdkgo.CredentialRefreshDriver[jiraservicemanagement.Credentials],
) (jiraservicemanagement.Credentials, error) {
	return provider.Resolve(call)
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context,
	sdkgo.Call,
	sdkgo.CredentialRefreshDriver[jiraservicemanagement.Credentials],
) (jiraservicemanagement.Credentials, error) {
	provider.forcedRefreshes++
	return jiraservicemanagement.Credentials{AccessToken: sdkgo.NewSecretString("replacement-token")}, nil
}

// testDexContext is a dex.Context whose heartbeat reads the previous attempt's value, as Dex supplies it.
type testDexContext struct {
	context.Context
	step              string
	previousHeartbeat json.RawMessage
	recordedHeartbeat json.RawMessage
	heartbeatCount    int
	rejectsHeartbeat  bool
	attempt           int32
}

func newTestDexContext(step string) *testDexContext {
	return &testDexContext{Context: context.Background(), step: step, attempt: 1}
}

// nextAttempt is the following attempt of the same Step execution, which sees this attempt's last heartbeat.
func (ctx *testDexContext) nextAttempt() *testDexContext {
	return &testDexContext{Context: ctx.Context, step: ctx.step, previousHeartbeat: ctx.recordedHeartbeat, attempt: ctx.attempt + 1}
}

func (*testDexContext) FlowID() string                                  { return "jsm-flow" }
func (*testDexContext) RunID() string                                   { return "run" }
func (*testDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (ctx *testDexContext) StepExecutionID() string                     { return ctx.step }
func (*testDexContext) FromStepExecutionID() string                     { return "" }
func (*testDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*testDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (ctx *testDexContext) Attempt() int32                              { return ctx.attempt }
func (*testDexContext) HasTimerFired() bool                             { return false }
func (*testDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*testDexContext) WaitForMethodFailed() bool                       { return false }
func (*testDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*testDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*testDexContext) RecordEvent(string, any) error                   { return nil }

func (ctx *testDexContext) RecordHeartbeat(value any) error {
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

func (ctx *testDexContext) GetLastHeartbeatValue(target any) (bool, error) {
	if ctx.previousHeartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(ctx.previousHeartbeat, target)
}

var _ dex.Context = (*testDexContext)(nil)
