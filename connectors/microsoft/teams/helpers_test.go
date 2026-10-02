// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams_test

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
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testTeamID      = "fbe2bf47-16c8-47cf-b4a5-4b9b187c508b"
	testChannelID   = "19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2"
	testChatID      = "19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2"
	testRootID      = "1616989510408"
	testUserID      = "8ea0e38b-efb3-4757-924a-5f94061cf8c2"
	testAccessToken = "teams-access-token"
	testRequestID   = "4e9e1c6f-6c26-4d0b-8a9f-6e8c2a0f5d11"
)

var (
	teamsConnection     = sdkgo.ConnectionRef{Provider: "microsoft", Name: "teams-test"}
	testChannelPath     = "/v1.0/teams/" + testTeamID + "/channels/" + testChannelID + "/messages"
	testRepliesPath     = testChannelPath + "/" + testRootID + "/replies"
	testChatMessagePath = "/v1.0/chats/" + testChatID + "/messages"
)

type recordedRequest struct {
	method          string
	path            string
	rawQuery        string
	authorization   string
	clientRequestID string
	contentType     string
	body            string
}

// recordingGraph is a credential-safe Microsoft Graph fake whose handler sees each request and its index.
type recordingGraph struct {
	*httptest.Server
	t        *testing.T
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingGraph(t *testing.T, handler func(http.ResponseWriter, *http.Request, int)) *recordingGraph {
	t.Helper()
	provider := &recordingGraph{t: t}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		contents, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		request.Body = io.NopCloser(bytes.NewReader(contents))
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recordedRequest{
			method: request.Method, path: request.URL.Path, rawQuery: request.URL.RawQuery,
			authorization: request.Header.Get("Authorization"), clientRequestID: request.Header.Get("client-request-id"),
			contentType: request.Header.Get("Content-Type"), body: string(contents),
		})
		provider.mutex.Unlock()
		handler(response, request, index)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingGraph) endpoint() string { return provider.URL + "/v1.0" }

func (provider *recordingGraph) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingGraph) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Greater(provider.t, len(provider.requests), index, "request %d was not sent", index)
	return provider.requests[index]
}

func (provider *recordingGraph) countRequests(method string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	count := 0
	for _, request := range provider.requests {
		if request.method == method {
			count++
		}
	}
	return count
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("request-id", testRequestID)
	response.WriteHeader(status)
	_, err := response.Write([]byte(body))
	require.NoError(t, err)
}

// graphErrorJSON is Graph's error envelope with a sentinel in the message text that must never surface.
func graphErrorJSON(code string) string {
	return `{"error":{"code":"` + code + `","message":"SENTINEL provider message text","innerError":{"request-id":"` + testRequestID + `"}}}`
}

// chatMessageJSON renders a Graph chatMessage posted by testUserID.
func chatMessageJSON(t *testing.T, id string, replyToID string, subject string, contentType string, content string, createdAt time.Time) string {
	t.Helper()
	message := map[string]any{
		"id": id, "etag": id, "messageType": "message", "createdDateTime": createdAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		"lastModifiedDateTime": createdAt.UTC().Format("2006-01-02T15:04:05.000Z"), "lastEditedDateTime": nil, "deletedDateTime": nil,
		"subject": nil, "summary": nil, "chatId": nil, "importance": "normal", "locale": "en-us",
		"webUrl": "https://teams.microsoft.com/l/message/" + testChannelID + "/" + id,
		"from": map[string]any{"application": nil, "device": nil, "user": map[string]any{
			"@odata.type": "#microsoft.graph.teamworkUserIdentity", "id": testUserID, "displayName": "Robin Kline", "userIdentityType": "aadUser",
		}},
		"body":            map[string]any{"contentType": contentType, "content": content},
		"channelIdentity": map[string]any{"teamId": testTeamID, "channelId": testChannelID},
		"attachments":     []any{}, "mentions": []any{}, "reactions": []any{},
	}
	if replyToID != "" {
		message["replyToId"] = replyToID
	} else {
		message["replyToId"] = nil
	}
	if subject != "" {
		message["subject"] = subject
	}
	encoded, err := json.Marshal(message)
	require.NoError(t, err)
	return string(encoded)
}

func collectionJSON(nextLink string, messages ...string) string {
	body := `{"@odata.context":"https://graph.microsoft.com/v1.0/$metadata#chatMessages","value":[`
	for index, message := range messages {
		if index > 0 {
			body += ","
		}
		body += message
	}
	body += "]"
	if nextLink != "" {
		body += `,"@odata.nextLink":"` + nextLink + `"`
	}
	return body + "}"
}

func newTeamsClient(t *testing.T, endpoint string, options ...teams.Option) *teams.Client {
	t.Helper()
	client, err := teams.New(teams.Config{Endpoint: endpoint}, staticTeamsCredentials(), options...)
	require.NoError(t, err)
	return client
}

func staticTeamsCredentials() sdkgo.StaticCredentialProvider[teams.Credentials] {
	return sdkgo.StaticCredentialProvider[teams.Credentials]{teamsConnection: {
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
	mutex           sync.Mutex
	forcedRefreshes int
}

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (teams.Credentials, error) {
	return teams.Credentials{AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context,
	sdkgo.Call,
	sdkgo.CredentialRefreshDriver[teams.Credentials],
) (teams.Credentials, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.forcedRefreshes++
	return teams.Credentials{AccessToken: sdkgo.NewSecretString("replacement-token")}, nil
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

func (*testDexContext) FlowID() string                          { return "teams-flow" }
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
