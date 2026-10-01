// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly_test

import (
	"context"
	"encoding/json"
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
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// sentinelToken stands in for every access token; no Result, Failure, or Receipt may contain it.
	sentinelToken = "SENTINEL-CALENDLY-ACCESS-TOKEN"
	// sentinelSigningKey stands in for the webhook signing key.
	sentinelSigningKey = "SENTINEL-CALENDLY-SIGNING-KEY"

	testUserURI         = "https://api.calendly.com/users/USER0001"
	testOrganizationURI = "https://api.calendly.com/organizations/ORG0001"
	testEventTypeURI    = "https://api.calendly.com/event_types/TYPE0001"
)

var (
	testConnection = sdkgo.ConnectionRef{Provider: "calendly", Name: "scheduling"}
	fixedNow       = time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
)

// fakeCalendly is a TLS stand-in for api.calendly.com and auth.calendly.com that records every request.
type fakeCalendly struct {
	*httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
}

type recordedRequest struct {
	method        string
	path          string
	query         url.Values
	authorization string
	body          string
}

// newFakeCalendly serves routes keyed by "METHOD /path"; an unknown route answers 404.
func newFakeCalendly(t *testing.T, routes map[string]http.HandlerFunc) *fakeCalendly {
	t.Helper()
	fake := &fakeCalendly{}
	fake.Server = httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(response, "unreadable", http.StatusBadRequest)
			return
		}
		fake.mu.Lock()
		fake.requests = append(fake.requests, recordedRequest{
			method: request.Method, path: request.URL.Path, query: request.URL.Query(),
			authorization: request.Header.Get("Authorization"), body: string(body),
		})
		fake.mu.Unlock()
		route, isFound := routes[request.Method+" "+request.URL.Path]
		if !isFound {
			writeJSON(response, http.StatusNotFound, `{"title":"Resource Not Found","message":"The server could not find the requested resource."}`)
			return
		}
		request.Body = io.NopCloser(strings.NewReader(string(body)))
		route(response, request)
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (fake *fakeCalendly) recordedRequests() []recordedRequest {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]recordedRequest(nil), fake.requests...)
}

func (fake *fakeCalendly) requestsTo(method string, path string) []recordedRequest {
	matches := []recordedRequest{}
	for _, request := range fake.recordedRequests() {
		if request.method == method && request.path == path {
			matches = append(matches, request)
		}
	}
	return matches
}

// redirectingClient sends every request, whatever its host, to the fake server.
func (fake *fakeCalendly) redirectingClient() *http.Client {
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

func newTestClient(t *testing.T, httpClient *http.Client, credentials sdkgo.CredentialProvider[calendly.Credentials], config calendly.Config) *calendly.Client {
	t.Helper()
	client, err := calendly.New(config, credentials, calendly.WithHTTPClient(httpClient), calendly.WithClock(func() time.Time { return fixedNow }))
	require.NoError(t, err)
	return client
}

func personalAccessTokenCredentials(signingKey string) sdkgo.StaticCredentialProvider[calendly.Credentials] {
	return sdkgo.StaticCredentialProvider[calendly.Credentials]{testConnection: {
		AuthMethodID: calendly.PersonalAccessTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(sentinelToken),
		WebhookSigningKey: sdkgo.NewSecretString(signingKey),
	}}
}

func writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, _ = io.WriteString(response, body) // A failed write fails the test's assertions instead.
}

func respondJSON(status int, body string) http.HandlerFunc {
	return func(response http.ResponseWriter, _ *http.Request) { writeJSON(response, status, body) }
}

// requireSecretFree proves a Result never carries a credential or a provider message.
func requireSecretFree(t *testing.T, result any) {
	t.Helper()
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), sentinelToken)
	require.NotContains(t, string(encoded), sentinelSigningKey)
	require.NotContains(t, string(encoded), providerSecretMessage)
}

// providerSecretMessage is provider text that must never reach a Failure.
const providerSecretMessage = "PROVIDER-DETAIL-THAT-MUST-NOT-LEAK"

func providerError(title string, message string) string {
	return fmt.Sprintf(`{"title":%q,"message":%q,"details":[{"message":%q}]}`, title, message, providerSecretMessage)
}

// scheduledEventJSON is a Calendly scheduled event with a Zoom password and notes the connector must drop.
func scheduledEventJSON(eventID string, status string, startTime time.Time) string {
	return fmt.Sprintf(`{
  "uri": "https://api.calendly.com/scheduled_events/%s",
  "name": "30 Minute Meeting",
  "status": %q,
  "start_time": %q,
  "end_time": %q,
  "event_type": %q,
  "location": {"type": "zoom", "status": "pushed", "join_url": "https://zoom.us/j/123", "data": {"password": "ZOOM-PASSWORD"}},
  "invitees_counter": {"total": 2, "active": 1, "limit": 1},
  "created_at": "2026-09-01T10:00:00.000000Z",
  "updated_at": "2026-09-02T10:00:00.000000Z",
  "event_memberships": [{"user": %q, "user_email": "host@example.com", "user_name": "Hana Host"}],
  "event_guests": [{"email": "guest@example.com", "created_at": "2026-09-01T10:00:00Z", "updated_at": "2026-09-01T10:00:00Z"}],
  "meeting_notes_plain": "INTERNAL-NOTES",
  "calendar_event": {"kind": "google", "external_id": "abc"}
}`, eventID, status, startTime.Format("2006-01-02T15:04:05.000000Z"), startTime.Add(30*time.Minute).Format("2006-01-02T15:04:05.000000Z"),
		testEventTypeURI, testUserURI)
}

func inviteeJSON(eventID string, inviteeID string, status string) string {
	return fmt.Sprintf(`{
  "uri": "https://api.calendly.com/scheduled_events/%s/invitees/%s",
  "event": "https://api.calendly.com/scheduled_events/%s",
  "email": "ada@example.com", "name": "Ada Lovelace", "first_name": null, "last_name": null,
  "status": %q, "timezone": "Europe/London",
  "questions_and_answers": [{"question": "Topic?", "answer": "Engines", "position": 0}],
  "rescheduled": false, "old_invitee": null, "new_invitee": null,
  "cancel_url": "https://calendly.com/cancellations/%s", "reschedule_url": "https://calendly.com/reschedulings/%s",
  "text_reminder_number": "+15550100", "tracking": {"utm_source": "newsletter"},
  "created_at": "2026-09-01T10:00:00.000000Z", "updated_at": "2026-09-01T10:00:00.000000Z"
}`, eventID, inviteeID, eventID, status, inviteeID, inviteeID)
}

func currentUserRoute() http.HandlerFunc {
	return respondJSON(http.StatusOK, fmt.Sprintf(`{"resource":{"uri":%q,"name":"Hana Host","current_organization":%q}}`, testUserURI, testOrganizationURI))
}

// stepContext is the Dex Step context a Connector Step receives; one step value is one Step execution.
type stepContext struct {
	context.Context
	step string
}

func newStepContext(step string) *stepContext {
	return &stepContext{Context: context.Background(), step: step}
}

func (*stepContext) FlowID() string                                  { return "calendly-test-flow" }
func (*stepContext) RunID() string                                   { return "run" }
func (*stepContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *stepContext) StepExecutionID() string                 { return context.step }
func (*stepContext) FromStepExecutionID() string                     { return "" }
func (*stepContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*stepContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*stepContext) Attempt() int32                                  { return 1 }
func (*stepContext) HasTimerFired() bool                             { return false }
func (*stepContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*stepContext) WaitForMethodFailed() bool                       { return false }
func (*stepContext) RecordHeartbeat(any) error                       { return nil }
func (*stepContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*stepContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*stepContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*stepContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*stepContext)(nil)
