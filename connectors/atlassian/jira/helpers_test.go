// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testCloudID     = "11223344-a1b2-4b33-8c44-def123456789"
	testAccessToken = "jira-access-token"
	testSitePrefix  = "/ex/jira/" + testCloudID + "/rest/api/3"
)

var jiraConnection = sdkgo.ConnectionRef{Provider: "atlassian", Name: "jira-test"}

type recordedRequest struct {
	method        string
	path          string
	rawQuery      string
	authorization string
	body          string
}

// recordingJira is a credential-safe Jira fake whose handler sees each request and its index.
type recordingJira struct {
	*httptest.Server
	t        *testing.T
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingJira(t *testing.T, handler func(http.ResponseWriter, *http.Request, int)) *recordingJira {
	t.Helper()
	provider := &recordingJira{t: t}
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

func (provider *recordingJira) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingJira) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Greater(provider.t, len(provider.requests), index, "request %d was not sent", index)
	return provider.requests[index]
}

func (provider *recordingJira) requestBody(index int) map[string]any {
	var body map[string]any
	require.NoError(provider.t, json.Unmarshal([]byte(provider.request(index).body), &body))
	return body
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Arequestid", "request-1")
	response.WriteHeader(status)
	_, err := response.Write([]byte(body))
	require.NoError(t, err)
}

func issueJSON(id string, key string, summary string, statusID string, statusName string) string {
	return fmt.Sprintf(`{"id":%q,"key":%q,"self":"https://api.atlassian.com/ex/jira/%s/rest/api/3/issue/%s","fields":{`+
		`"summary":%q,"status":{"id":%q,"name":%q,"statusCategory":{"key":"new"}},`+
		`"issuetype":{"id":"10001","name":"Task","subtask":false},"project":{"id":"10000","key":"OPS","name":"Operations"},`+
		`"assignee":{"accountId":"5b10ac8d82e05b22cc7d4ef5","displayName":"Ada","emailAddress":"ada@example.com"},`+
		`"reporter":null,"priority":{"id":"3","name":"Medium"},"labels":["incident"],`+
		`"created":"2026-09-30T09:15:00.000-0700","updated":"2026-09-30T10:00:00.000-0700"}}`,
		id, key, testCloudID, id, summary, statusID, statusName)
}

func newJiraClient(t *testing.T, endpoint string, options ...jira.Option) *jira.Client {
	t.Helper()
	return newJiraClientForSite(t, endpoint, testCloudID, options...)
}

func newJiraClientForSite(t *testing.T, endpoint string, cloudID string, options ...jira.Option) *jira.Client {
	t.Helper()
	client, err := jira.New(jira.Config{CloudID: cloudID, Endpoint: endpoint}, staticJiraCredentials(), options...)
	require.NoError(t, err)
	return client
}

func staticJiraCredentials() sdkgo.StaticCredentialProvider[jira.Credentials] {
	return sdkgo.StaticCredentialProvider[jira.Credentials]{jiraConnection: {
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

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (jira.Credentials, error) {
	return jira.Credentials{AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context,
	sdkgo.Call,
	sdkgo.CredentialRefreshDriver[jira.Credentials],
) (jira.Credentials, error) {
	provider.forcedRefreshes++
	return jira.Credentials{AccessToken: sdkgo.NewSecretString("replacement-token")}, nil
}

// jiraDexContext is the minimal Dex Step context the SDK needs to derive a Call.
type jiraDexContext struct {
	context.Context
	step string
}

func newJiraDexContext(step string) *jiraDexContext {
	return &jiraDexContext{Context: context.Background(), step: step}
}

func (*jiraDexContext) FlowID() string                                  { return "jira-flow" }
func (*jiraDexContext) RunID() string                                   { return "run" }
func (*jiraDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *jiraDexContext) StepExecutionID() string                 { return context.step }
func (*jiraDexContext) FromStepExecutionID() string                     { return "" }
func (*jiraDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*jiraDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*jiraDexContext) Attempt() int32                                  { return 1 }
func (*jiraDexContext) HasTimerFired() bool                             { return false }
func (*jiraDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*jiraDexContext) WaitForMethodFailed() bool                       { return false }
func (*jiraDexContext) RecordHeartbeat(any) error                       { return nil }
func (*jiraDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*jiraDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*jiraDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*jiraDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*jiraDexContext)(nil)

func containsText(values []string, text string) bool {
	for _, value := range values {
		if strings.Contains(value, text) {
			return true
		}
	}
	return false
}
