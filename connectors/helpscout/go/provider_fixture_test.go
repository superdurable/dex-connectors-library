// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// sentinelToken stands in for every access token; no Result, Failure, or Receipt may contain it.
	sentinelToken = "SENTINEL-HELPSCOUT-ACCESS-TOKEN"
	// sentinelWebhookSecret stands in for the webhook secret.
	sentinelWebhookSecret = "SENTINEL-HELPSCOUT-WEBHOOK-SECRET"
	// providerSecretMessage is provider text that must never reach a Failure.
	providerSecretMessage = "PROVIDER-DETAIL-THAT-MUST-NOT-LEAK"
)

var (
	testConnection = sdkgo.ConnectionRef{Provider: "helpscout", Name: "support-inbox"}
	fixedNow       = time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
)

// fakeHelpScout is a TLS stand-in for api.helpscout.net that records every request.
type fakeHelpScout struct {
	*httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
}

type recordedRequest struct {
	method        string
	path          string
	query         url.Values
	authorization string
	contentType   string
	body          string
}

// newFakeHelpScout serves routes keyed by "METHOD /path"; an unknown route answers 404.
func newFakeHelpScout(t *testing.T, routes map[string]http.HandlerFunc) *fakeHelpScout {
	t.Helper()
	fake := &fakeHelpScout{}
	fake.Server = httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(response, "unreadable", http.StatusBadRequest)
			return
		}
		fake.mu.Lock()
		fake.requests = append(fake.requests, recordedRequest{
			method: request.Method, path: request.URL.Path, query: request.URL.Query(),
			authorization: request.Header.Get("Authorization"), contentType: request.Header.Get("Content-Type"), body: string(body),
		})
		fake.mu.Unlock()
		route, isFound := routes[request.Method+" "+request.URL.Path]
		if !isFound {
			writeJSON(response, http.StatusNotFound, helpScoutErrorBody("Not Found", nil))
			return
		}
		request.Body = io.NopCloser(strings.NewReader(string(body)))
		route(response, request)
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (fake *fakeHelpScout) recordedRequests() []recordedRequest {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]recordedRequest(nil), fake.requests...)
}

func (fake *fakeHelpScout) requestsTo(method string, path string) []recordedRequest {
	matches := []recordedRequest{}
	for _, request := range fake.recordedRequests() {
		if request.method == method && request.path == path {
			matches = append(matches, request)
		}
	}
	return matches
}

// redirectingClient sends every request, whatever its host, to the fake server.
func (fake *fakeHelpScout) redirectingClient() *http.Client {
	target, err := url.Parse(fake.URL)
	if err != nil {
		panic(err)
	}
	base := fake.Client().Transport
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		redirected := request.Clone(request.Context())
		redirected.URL.Scheme, redirected.URL.Host, redirected.Host = target.Scheme, target.Host, target.Host
		return base.RoundTrip(redirected)
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func newTestClient(t *testing.T, httpClient *http.Client, credentials helpscout.CredentialSource, config helpscout.Config) *helpscout.Client {
	t.Helper()
	client, err := helpscout.New(config, credentials, helpscout.WithHTTPClient(httpClient), helpscout.WithClock(func() time.Time { return fixedNow }))
	require.NoError(t, err)
	return client
}

func newFakeBackedClient(t *testing.T, fake *fakeHelpScout) *helpscout.Client {
	t.Helper()
	return newTestClient(t, fake.redirectingClient(), staticCredentials(sentinelWebhookSecret), helpscout.Config{})
}

func staticCredentials(webhookSecret string) sdkgo.StaticCredentialProvider[helpscout.Credentials] {
	return sdkgo.StaticCredentialProvider[helpscout.Credentials]{testConnection: {
		AppID: "app-id", AppSecret: sdkgo.NewSecretString("app-secret"),
		AccessToken: sdkgo.NewSecretString(sentinelToken), WebhookSecret: sdkgo.NewSecretString(webhookSecret),
	}}
}

func writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/hal+json")
	response.WriteHeader(status)
	_, _ = io.WriteString(response, body) // A failed write fails the test's assertions instead.
}

func respondJSON(status int, body string) http.HandlerFunc {
	return func(response http.ResponseWriter, _ *http.Request) { writeJSON(response, status, body) }
}

func respondStatus(status int, header map[string]string) http.HandlerFunc {
	return func(response http.ResponseWriter, _ *http.Request) {
		for name, value := range header {
			response.Header().Set(name, value)
		}
		response.WriteHeader(status)
	}
}

// helpScoutErrorBody is Help Scout's unified error object; every message and rejected value is secret text.
func helpScoutErrorBody(message string, fieldErrors map[string]string) string {
	errorsJSON := []string{}
	for path, code := range fieldErrors {
		errorsJSON = append(errorsJSON, fmt.Sprintf(`{"path":%q,"message":%q,"rejectedValue":%q,"source":"JSON","_links":{"about":{"href":"http://developer.helpscout.net/mailbox-api/overview/errors#%s"}}}`,
			path, providerSecretMessage, providerSecretMessage, code))
	}
	return fmt.Sprintf(`{"logRef":"64bb72b4-0a92-11e8-ba89-0ed5f89f718b","message":%q,"_embedded":{"errors":[%s]},"_links":{"about":{"href":"http://developer.helpscout.net/mailbox-api/overview/errors"}}}`,
		message+" "+providerSecretMessage, strings.Join(errorsJSON, ","))
}

// conversationJSON is a v2 Conversation object as Help Scout documents it, with an assignee and two tags.
func conversationJSON(conversationID int64, status string, mailboxID int64, tags ...string) string {
	tagObjects := make([]string, 0, len(tags))
	for index, tag := range tags {
		tagObjects = append(tagObjects, fmt.Sprintf(`{"id":%d,"color":"#929499","tag":%q,"styles":{"default":{"background":{"fill":"#ACE3FF"}}}}`, 9150+index, tag))
	}
	return fmt.Sprintf(`{
  "_links": {"self": {"href": "https://api.helpscout.net/v2/conversations/%d"}, "web": {"href": "https://secure.helpscout.net/conversation/%d/12"}},
  "id": %d, "number": 12, "threads": 2, "type": "email", "folderId": 11, "status": %q, "state": "published",
  "subject": "Double charge on order 88213", "preview": "I was charged twice", "mailboxId": %d,
  "assignee": {"id": 99, "type": "user", "first": "Mr", "last": "Robot", "email": "agent@acme.example.com"},
  "createdBy": {"id": 238604, "type": "customer", "email": "jane@acme.example.com"},
  "createdAt": "2026-09-29T22:46:22Z", "closedBy": 0, "userUpdatedAt": "2026-09-30T08:00:00Z",
  "customerWaitingSince": {"time": "2026-09-30T07:59:00Z", "friendly": "4 hours ago"},
  "source": {"type": "email", "via": "customer"},
  "tags": [%s],
  "cc": ["cc@acme.example.com"], "bcc": ["bcc@acme.example.com"],
  "primaryCustomer": {"id": 238604, "type": "customer", "first": "Jane", "last": "Smith", "email": "jane@acme.example.com"},
  "customFields": [{"id": 8, "name": "Account Type", "value": "8518", "text": "Free"}],
  "_embedded": {"threads": []}
}`, conversationID, conversationID, conversationID, status, mailboxID, strings.Join(tagObjects, ","))
}

func threadJSON(threadID int64, threadType string, body string, createdAt string) string {
	return fmt.Sprintf(`{"id": %d, "type": %q, "status": "active", "state": "published", "body": %q,
  "source": {"type": "email", "via": "customer"},
  "customer": {"id": 238604, "first": "Jane", "last": "Smith", "email": "jane@acme.example.com"},
  "createdBy": {"id": 238604, "type": "customer", "first": "Jane", "last": "Smith", "email": "jane@acme.example.com"},
  "to": ["support@acme.example.com"], "createdAt": %q}`, threadID, threadType, body, createdAt)
}

func threadPageJSON(threads []string, hasNext bool) string {
	next := ""
	if hasNext {
		next = `,"next":{"href":"https://api.helpscout.net/v2/conversations/1/threads?page=2"}`
	}
	return fmt.Sprintf(`{"_embedded":{"threads":[%s]},"_links":{"self":{"href":"x"}%s},"page":{"number":1,"size":25,"totalElements":%d,"totalPages":1}}`,
		strings.Join(threads, ","), next, len(threads))
}

// requireRetry asserts that an operation returned Retry with kind and returns its failure.
func requireRetry(t *testing.T, err error, kind sdkgo.FailureKind) *sdkgo.RetryError {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, kind, retry.Failure.Kind, retry.Failure.Message)
	return retry
}

// retryDelay is the provider delay a Retry carries, or zero for the Step's retry policy.
func retryDelay(err error) time.Duration {
	var retryAfter *dex.RetryAfterError
	if errors.As(err, &retryAfter) {
		return retryAfter.After
	}
	return 0
}

// requireSecretFree proves a Result never carries a credential or a provider message.
func requireSecretFree(t *testing.T, result any) {
	t.Helper()
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), sentinelToken)
	require.NotContains(t, string(encoded), sentinelWebhookSecret)
	require.NotContains(t, string(encoded), providerSecretMessage)
}

// stepContext is one Step execution's Dex context; it keeps the last heartbeat across attempts.
type stepContext struct {
	context.Context
	step             string
	mu               sync.Mutex
	heartbeat        []byte
	heartbeatRecords int
}

func newStepContext(step string) *stepContext {
	return &stepContext{Context: context.Background(), step: step}
}

func (*stepContext) FlowID() string                          { return "helpscout-test-flow" }
func (*stepContext) RunID() string                           { return "run" }
func (*stepContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (context *stepContext) StepExecutionID() string         { return context.step }
func (*stepContext) FromStepExecutionID() string             { return "" }
func (*stepContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*stepContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (*stepContext) Attempt() int32                          { return 1 }
func (*stepContext) HasTimerFired() bool                     { return false }
func (*stepContext) HasTimerFiredByIndex(int) bool           { return false }
func (*stepContext) WaitForMethodFailed() bool               { return false }
func (*stepContext) SetStepExecutionLocal(string, any) error { return nil }
func (*stepContext) RecordEvent(string, any) error           { return nil }
func (*stepContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}

// RecordHeartbeat keeps the value as JSON; nil clears it, as a nil Dex heartbeat clears persisted details.
func (context *stepContext) RecordHeartbeat(value any) error {
	context.mu.Lock()
	defer context.mu.Unlock()
	context.heartbeatRecords++
	if value == nil {
		context.heartbeat = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	context.heartbeat = encoded
	return err
}

func (context *stepContext) GetLastHeartbeatValue(destination any) (bool, error) {
	context.mu.Lock()
	defer context.mu.Unlock()
	if context.heartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, destination)
}

func (context *stepContext) hasHeartbeat() bool {
	context.mu.Lock()
	defer context.mu.Unlock()
	return context.heartbeat != nil
}

var _ dex.Context = (*stepContext)(nil)
