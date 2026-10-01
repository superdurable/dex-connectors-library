// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testCloudID      = "11223344-a1b2-4b33-8c44-def123456789"
	testAccessToken  = "confluence-access-token"
	testContentPath  = "/ex/confluence/" + testCloudID + "/wiki/api/v2"
	testSearchPath   = "/ex/confluence/" + testCloudID + "/wiki/rest/api"
	testSiteBase     = "https://ops.atlassian.net/wiki"
	testSpaceID      = "98306"
	testPolicyBody   = "# Remote work\n\nStaff may work **remotely** two days a week.\n\n- Ask your manager\n- Log the days"
	testPolicyTitle  = "Remote work policy"
	testPolicyPageID = "557057"
)

var confluenceConnection = sdkgo.ConnectionRef{Provider: "atlassian", Name: "confluence-test"}

type recordedRequest struct {
	method        string
	path          string
	rawQuery      string
	authorization string
	body          string
}

// recordingConfluence is a credential-safe Confluence fake whose handler sees each request and its index.
type recordingConfluence struct {
	*httptest.Server
	t        *testing.T
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingConfluence(t *testing.T, handler func(http.ResponseWriter, *http.Request, int)) *recordingConfluence {
	t.Helper()
	provider := &recordingConfluence{t: t}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		contents, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		request.Body = io.NopCloser(bytes.NewReader(contents))
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recordedRequest{
			method: request.Method, path: request.URL.Path, rawQuery: request.URL.RawQuery,
			authorization: request.Header.Get("Authorization"), body: string(contents),
		})
		provider.mutex.Unlock()
		handler(response, request, index)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingConfluence) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingConfluence) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Greater(provider.t, len(provider.requests), index, "request %d was not sent", index)
	return provider.requests[index]
}

func (provider *recordingConfluence) countRequests(method string, path string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	count := 0
	for _, request := range provider.requests {
		if request.method == method && request.path == path {
			count++
		}
	}
	return count
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Atl-Traceid", "trace-1")
	response.WriteHeader(status)
	_, err := response.Write([]byte(body))
	require.NoError(t, err)
}

// pageJSON renders a v2 page below page 65537 with a storage body.
func pageJSON(t *testing.T, id string, title string, version int, message string, createdAt time.Time, storage string) string {
	t.Helper()
	return pageJSONUnderParent(t, id, title, "65537", version, message, createdAt, storage)
}

func pageJSONUnderParent(t *testing.T, id string, title string, parentID string, version int, message string, createdAt time.Time, storage string) string {
	t.Helper()
	page := map[string]any{
		"id": id, "status": "current", "title": title, "spaceId": testSpaceID, "parentId": parentID, "parentType": "page",
		"authorId": "5b10ac8d82e05b22cc7d4ef5", "createdAt": createdAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		"version": map[string]any{"number": version, "message": message, "createdAt": createdAt.UTC().Format("2006-01-02T15:04:05.000Z"), "authorId": "5b10ac8d82e05b22cc7d4ef5"},
		"body":    map[string]any{"storage": map[string]any{"representation": "storage", "value": storage}},
		"_links":  map[string]any{"webui": "/spaces/OPS/pages/" + id, "base": testSiteBase},
	}
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	return string(encoded)
}

func newConfluenceClient(t *testing.T, endpoint string, options ...confluence.Option) *confluence.Client {
	t.Helper()
	return newConfluenceClientForSite(t, endpoint, testCloudID, options...)
}

func newConfluenceClientForSite(t *testing.T, endpoint string, cloudID string, options ...confluence.Option) *confluence.Client {
	t.Helper()
	client, err := confluence.New(confluence.Config{CloudID: cloudID, Endpoint: endpoint}, staticConfluenceCredentials(), options...)
	require.NoError(t, err)
	return client
}

func staticConfluenceCredentials() sdkgo.StaticCredentialProvider[confluence.Credentials] {
	return sdkgo.StaticCredentialProvider[confluence.Credentials]{confluenceConnection: {
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

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (confluence.Credentials, error) {
	return confluence.Credentials{AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context,
	sdkgo.Call,
	sdkgo.CredentialRefreshDriver[confluence.Credentials],
) (confluence.Credentials, error) {
	provider.forcedRefreshes++
	return confluence.Credentials{AccessToken: sdkgo.NewSecretString("replacement-token")}, nil
}

// testDexContext is a Dex Step context that keeps heartbeat details across attempts of one Step execution.
type testDexContext struct {
	context.Context
	step           string
	firstAttemptAt time.Time
	mutex          sync.Mutex
	heartbeat      []byte
	recordErr      error
}

func newTestDexContext(step string) *testDexContext {
	return &testDexContext{Context: context.Background(), step: step, firstAttemptAt: time.Now()}
}

func (*testDexContext) FlowID() string                          { return "confluence-flow" }
func (*testDexContext) RunID() string                           { return "run" }
func (*testDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (dexContext *testDexContext) StepExecutionID() string      { return dexContext.step }
func (*testDexContext) FromStepExecutionID() string             { return "" }
func (*testDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (dexContext *testDexContext) FirstAttemptAt() time.Time    { return dexContext.firstAttemptAt }
func (*testDexContext) Attempt() int32                          { return 1 }
func (*testDexContext) HasTimerFired() bool                     { return false }
func (*testDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*testDexContext) WaitForMethodFailed() bool               { return false }
func (*testDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*testDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*testDexContext) RecordEvent(string, any) error { return nil }

func (dexContext *testDexContext) RecordHeartbeat(value any) error {
	dexContext.mutex.Lock()
	defer dexContext.mutex.Unlock()
	if dexContext.recordErr != nil {
		return dexContext.recordErr
	}
	if value == nil {
		dexContext.heartbeat = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	dexContext.heartbeat = encoded
	return nil
}

func (dexContext *testDexContext) GetLastHeartbeatValue(valuePtr any) (bool, error) {
	dexContext.mutex.Lock()
	defer dexContext.mutex.Unlock()
	if dexContext.heartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(dexContext.heartbeat, valuePtr)
}

func (dexContext *testDexContext) hasHeartbeat() bool {
	dexContext.mutex.Lock()
	defer dexContext.mutex.Unlock()
	return dexContext.heartbeat != nil
}

var _ dex.Context = (*testDexContext)(nil)
