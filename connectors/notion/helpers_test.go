// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAPIToken     = "ntn_test_token_value"
	testPageID       = "1f3c9a2e-5b7d-4e8a-9c01-23456789abcd"
	testDataSourceID = "2a4d6f80-1b3c-4d5e-8f70-112233445566"
	testDatabaseID   = "3b5e7091-2c4d-4e6f-9081-223344556677"
	sentinel         = "SENTINEL"
)

var notionConnection = sdkgo.ConnectionRef{Provider: "notion", Name: "notion-test"}

type recordedRequest struct {
	method        string
	path          string
	query         string
	authorization string
	version       string
	body          string
}

// recordingNotion is a credential-safe Notion fake whose handler sees each request and its index.
type recordingNotion struct {
	*httptest.Server
	t        *testing.T
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingNotion(t *testing.T, handler func(http.ResponseWriter, *http.Request, int)) *recordingNotion {
	t.Helper()
	provider := &recordingNotion{t: t}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		contents, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		request.Body = io.NopCloser(bytes.NewReader(contents))
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recordedRequest{
			method: request.Method, path: request.URL.Path, query: request.URL.RawQuery,
			authorization: request.Header.Get("Authorization"), version: request.Header.Get("Notion-Version"), body: string(contents),
		})
		provider.mutex.Unlock()
		handler(response, request, index)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingNotion) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingNotion) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Greater(provider.t, len(provider.requests), index, "request %d was not sent", index)
	return provider.requests[index]
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Notion-Request-Id", "request-1")
	response.WriteHeader(status)
	_, err := response.Write([]byte(body))
	require.NoError(t, err)
}

// notionError is a Notion error body whose message carries the sentinel, which must never surface.
func notionError(status int, code string) string {
	return fmt.Sprintf(`{"object":"error","status":%d,"code":%q,"message":"%s provider text","request_id":"err-request-1"}`, status, code, sentinel)
}

// pageJSON is a database row page in a data source with a title, a status, and an email property.
func pageJSON(id string, title string) string {
	return fmt.Sprintf(`{"object":"page","id":%q,"created_time":"2026-09-30T16:00:00.000Z","last_edited_time":"2026-09-30T16:05:00.000Z",`+
		`"in_trash":false,"is_archived":false,"is_locked":false,"url":"https://app.notion.com/p/%s","public_url":null,`+
		`"parent":{"type":"data_source_id","data_source_id":%q,"database_id":%q},`+
		`"properties":{"Name":{"id":"title","type":"title","title":[{"type":"text","text":{"content":%q},"plain_text":%q}]},`+
		`"Status":{"id":"st%%3A","type":"select","select":{"id":"opt1","name":"New","color":"blue"}},`+
		`"Email":{"id":"em1","type":"email","email":"ada@example.com"}},`+
		`"icon":null,"cover":null,"created_by":{"object":"user","id":"user-1"},"last_edited_by":{"object":"user","id":"user-1"}}`,
		id, id, testDataSourceID, testDatabaseID, title, title)
}

func newNotionClient(t *testing.T, endpoint string, options ...notion.Option) *notion.Client {
	t.Helper()
	client, err := notion.New(notion.Config{Endpoint: endpoint}, staticNotionCredentials(), options...)
	require.NoError(t, err)
	return client
}

func staticNotionCredentials() sdkgo.StaticCredentialProvider[notion.Credentials] {
	return sdkgo.StaticCredentialProvider[notion.Credentials]{notionConnection: {APIToken: sdkgo.NewSecretString(testAPIToken)}}
}

func decodeBody(t *testing.T, body string) map[string]any {
	t.Helper()
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &decoded))
	return decoded
}

// requireNoSentinel proves a Result carries neither provider message text nor the API token.
func requireNoSentinel(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), sentinel)
	require.NotContains(t, string(encoded), testAPIToken)
}

func requireRetry(t *testing.T, err error, kind sdkgo.FailureKind) *sdkgo.RetryError {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, kind, retry.Failure.Kind)
	require.NotContains(t, retry.Failure.Message, sentinel)
	return retry
}

// retryDelay returns the provider delay Dex waits before the next attempt, or zero for the Step policy.
func retryDelay(err error) time.Duration {
	var retryAfter *dex.RetryAfterError
	if errors.As(err, &retryAfter) {
		return retryAfter.After
	}
	return 0
}

// notionDexContext is the minimal Dex Step context the SDK needs to derive a Call.
type notionDexContext struct {
	context.Context
	step string
}

func newNotionDexContext(step string) *notionDexContext {
	return &notionDexContext{Context: context.Background(), step: step}
}

func (*notionDexContext) FlowID() string                                  { return "notion-flow" }
func (*notionDexContext) RunID() string                                   { return "run" }
func (*notionDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *notionDexContext) StepExecutionID() string                 { return context.step }
func (*notionDexContext) FromStepExecutionID() string                     { return "" }
func (*notionDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*notionDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*notionDexContext) Attempt() int32                                  { return 1 }
func (*notionDexContext) HasTimerFired() bool                             { return false }
func (*notionDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*notionDexContext) WaitForMethodFailed() bool                       { return false }
func (*notionDexContext) RecordHeartbeat(any) error                       { return nil }
func (*notionDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*notionDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*notionDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*notionDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*notionDexContext)(nil)
