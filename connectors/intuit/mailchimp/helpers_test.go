// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp_test

import (
	"context"
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
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAPIKey       = "0123456789abcdef0123456789abcdef" + "-us6"
	testListID       = "57afe96172"
	testCampaignID   = "42694e9e57"
	testEmailAddress = "Urist.McVankab@example.com"
	// testSubscriberHash is the MD5 hash of urist.mcvankab@example.com.
	testSubscriberHash = "cb63c7306d1853af527b11f0cb244d45"
)

var mailchimpConnection = sdkgo.ConnectionRef{Provider: "mailchimp", Name: "mailchimp-test"}

// recordedRequest is one request a recordingMailchimp received.
type recordedRequest struct {
	method string
	path   string
	query  map[string][]string
	header http.Header
	body   string
}

// recordingMailchimp is a credential-safe httptest server that records requests and delegates replies.
type recordingMailchimp struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingMailchimp(t *testing.T, reply func(http.ResponseWriter, *http.Request, int)) *recordingMailchimp {
	t.Helper()
	provider := &recordingMailchimp{}
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

func (provider *recordingMailchimp) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingMailchimp) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

func newMailchimpClient(t *testing.T, baseURL string) *mailchimp.Client {
	t.Helper()
	if !strings.HasSuffix(baseURL, "/3.0") {
		baseURL += "/3.0"
	}
	client, err := mailchimp.New(mailchimp.Config{}, testCredentialProvider(testAPIKey), mailchimp.WithAPIBaseURL(baseURL))
	require.NoError(t, err)
	return client
}

func testCredentialProvider(apiKey string) sdkgo.StaticCredentialProvider[mailchimp.Credentials] {
	return sdkgo.StaticCredentialProvider[mailchimp.Credentials]{mailchimpConnection: {APIKey: sdkgo.NewSecretString(apiKey)}}
}

// closedLoopbackURL returns an address nothing listens on, so a connection is refused before any request byte is sent.
func closedLoopbackURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return "http://" + address + "/3.0"
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	if response.Header().Get("X-Request-Id") == "" {
		response.Header().Set("X-Request-Id", "a1efb240-f8d8-40fe-a680-c3a5619a42e9")
	}
	response.WriteHeader(status)
	_, err := io.WriteString(response, body)
	require.NoError(t, err)
}

func writeProblem(t *testing.T, response http.ResponseWriter, status int, title string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	writeJSON(t, response, status, `{"type":"https://mailchimp.com/developer/marketing/docs/errors/","title":"`+title+
		`","status":`+jsonNumber(status)+`,"detail":"SENTINEL jane@example.com detail","instance":"3b4dcb40-0b6b-4820-bfaa-41267b3826ea"}`)
}

func writeValue(t *testing.T, response http.ResponseWriter, status int, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	writeJSON(t, response, status, string(encoded))
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

func memberJSON(emailAddress string, status string) map[string]any {
	return map[string]any{
		"id": mailchimp.SubscriberHash(emailAddress), "email_address": emailAddress, "unique_email_id": "882e9bec19",
		"contact_id": "e4b9b6c1a2", "full_name": "Urist McVankab", "web_id": 123456, "email_type": "html", "status": status,
		"unsubscribe_reason": "SENTINEL reason", "merge_fields": map[string]any{"FNAME": "Urist", "LNAME": "McVankab", "AGE": 42},
		"stats":            map[string]any{"avg_open_rate": 0.5, "avg_click_rate": 0.1},
		"timestamp_signup": "2026-01-12T09:00:00+00:00", "timestamp_opt": "", "last_changed": "2026-01-28T08:45:00+00:00",
		"language": "en", "vip": false, "tags_count": 2, "tags": []map[string]any{{"id": 1, "name": "Influencer"}, {"id": 2, "name": "Tech"}},
		"list_id": testListID, "_links": []map[string]any{{"rel": "self", "href": "https://us6.api.mailchimp.com/3.0/lists/" + testListID}},
	}
}

func memberBody(emailAddress string, status string) string {
	encoded, err := json.Marshal(memberJSON(emailAddress, status))
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func campaignJSON(status string, campaignType string, listID string) map[string]any {
	return map[string]any{
		"id": testCampaignID, "web_id": 98765, "type": campaignType, "status": status, "emails_sent": 0,
		"create_time": "2026-01-20T10:00:00+00:00", "send_time": "",
		"settings":   map[string]any{"title": "Spring launch", "subject_line": "Our spring launch is here"},
		"recipients": map[string]any{"list_id": listID, "list_name": "Customers", "recipient_count": 1200},
	}
}

func campaignBody(status string) string {
	encoded, err := json.Marshal(campaignJSON(status, "regular", testListID))
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func jsonNumber(value int) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func requireNoSecretOrProviderText(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
	require.NotContains(t, string(encoded), testAPIKey)
	require.NotContains(t, string(encoded), "jane@example.com")
}

// mailchimpDexContext is a dex.Context whose heartbeat reads the previous attempt's value, as Dex supplies it.
type mailchimpDexContext struct {
	context.Context
	step              string
	previousHeartbeat json.RawMessage
	recordedHeartbeat json.RawMessage
	heartbeatCount    int
	rejectsHeartbeat  bool
	attempt           int32
}

func newMailchimpDexContext(step string) *mailchimpDexContext {
	return &mailchimpDexContext{Context: context.Background(), step: step, attempt: 1}
}

// nextAttempt is the following attempt of the same Step execution, which sees this attempt's last heartbeat.
func (context *mailchimpDexContext) nextAttempt() *mailchimpDexContext {
	return &mailchimpDexContext{
		Context: context.Context, step: context.step, previousHeartbeat: context.recordedHeartbeat, attempt: context.attempt + 1,
	}
}

func (*mailchimpDexContext) FlowID() string                          { return "mailchimp-flow" }
func (*mailchimpDexContext) RunID() string                           { return "run" }
func (*mailchimpDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (context *mailchimpDexContext) StepExecutionID() string         { return context.step }
func (*mailchimpDexContext) FromStepExecutionID() string             { return "" }
func (*mailchimpDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*mailchimpDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (context *mailchimpDexContext) Attempt() int32                  { return context.attempt }
func (*mailchimpDexContext) HasTimerFired() bool                     { return false }
func (*mailchimpDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*mailchimpDexContext) WaitForMethodFailed() bool               { return false }
func (*mailchimpDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*mailchimpDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*mailchimpDexContext) RecordEvent(string, any) error { return nil }

func (context *mailchimpDexContext) RecordHeartbeat(value any) error {
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

func (context *mailchimpDexContext) GetLastHeartbeatValue(target any) (bool, error) {
	if context.previousHeartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(context.previousHeartbeat, target)
}

var _ dex.Context = (*mailchimpDexContext)(nil)
