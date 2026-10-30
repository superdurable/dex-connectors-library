// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform_test

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
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// sentinelToken stands in for the personal access token; no Result, Failure, or Receipt may contain it.
	sentinelToken = "SENTINEL-TYPEFORM-ACCESS-TOKEN"
	// sentinelSecret stands in for the webhook secret.
	sentinelSecret = "SENTINEL-TYPEFORM-WEBHOOK-SECRET"
	// providerSecretMessage is provider text that must never reach a Failure.
	providerSecretMessage = "PROVIDER-DETAIL-THAT-MUST-NOT-LEAK"

	testFormID = "lT4Z3j"
)

var (
	testConnection = sdkgo.ConnectionRef{Provider: "typeform", Name: "forms"}
	fixedNow       = time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
)

// fakeTypeform is a TLS stand-in for every Typeform API host that records each request.
type fakeTypeform struct {
	*httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
}

type recordedRequest struct {
	host          string
	method        string
	path          string
	query         url.Values
	authorization string
	body          string
}

// newFakeTypeform serves routes keyed by "METHOD /path"; an unknown route answers 404.
func newFakeTypeform(t *testing.T, routes map[string]http.HandlerFunc) *fakeTypeform {
	t.Helper()
	fake := &fakeTypeform{}
	fake.Server = httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(response, "unreadable", http.StatusBadRequest)
			return
		}
		fake.mu.Lock()
		fake.requests = append(fake.requests, recordedRequest{
			host: request.Header.Get("X-Original-Host"), method: request.Method, path: request.URL.EscapedPath(), query: request.URL.Query(),
			authorization: request.Header.Get("Authorization"), body: string(body),
		})
		fake.mu.Unlock()
		route, isFound := routes[request.Method+" "+request.URL.EscapedPath()]
		if !isFound {
			writeJSON(response, http.StatusNotFound, providerError("NOT_EXISTING_ID"))
			return
		}
		request.Body = io.NopCloser(strings.NewReader(string(body)))
		route(response, request)
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (fake *fakeTypeform) recordedRequests() []recordedRequest {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]recordedRequest(nil), fake.requests...)
}

func (fake *fakeTypeform) requestsTo(method string, path string) []recordedRequest {
	matches := []recordedRequest{}
	for _, request := range fake.recordedRequests() {
		if request.method == method && request.path == path {
			matches = append(matches, request)
		}
	}
	return matches
}

// redirectingClient sends every request, whatever its host, to the fake server and records the host.
func (fake *fakeTypeform) redirectingClient() *http.Client {
	target, err := url.Parse(fake.URL)
	if err != nil {
		panic(err)
	}
	base := fake.Client().Transport
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		redirected := request.Clone(request.Context())
		redirected.Header.Set("X-Original-Host", request.URL.Host)
		redirected.URL.Scheme, redirected.URL.Host, redirected.Host = target.Scheme, target.Host, target.Host
		return base.RoundTrip(redirected)
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func newTestClient(t *testing.T, httpClient *http.Client, credentials sdkgo.CredentialProvider[typeform.Credentials], config typeform.Config) *typeform.Client {
	t.Helper()
	client, err := typeform.New(config, credentials, typeform.WithHTTPClient(httpClient), typeform.WithClock(func() time.Time { return fixedNow }))
	require.NoError(t, err)
	return client
}

func personalAccessTokenCredentials(secret string) sdkgo.StaticCredentialProvider[typeform.Credentials] {
	return sdkgo.StaticCredentialProvider[typeform.Credentials]{testConnection: {
		AuthMethodID: typeform.PersonalAccessTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(sentinelToken),
		WebhookSecret: sdkgo.NewSecretString(secret),
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

// providerError is a Typeform error object whose description must never reach a Failure.
func providerError(code string) string {
	return fmt.Sprintf(`{"code":%q,"description":%q,"details":[{"code":"x","description":%q}]}`, code, providerSecretMessage, providerSecretMessage)
}

// requireSecretFree proves a Result never carries a credential or a provider message.
func requireSecretFree(t *testing.T, result any) {
	t.Helper()
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), sentinelToken)
	require.NotContains(t, string(encoded), sentinelSecret)
	require.NotContains(t, string(encoded), providerSecretMessage)
}

// stepContext is the Dex Step context a Connector Step receives; one step value is one Step execution.
type stepContext struct {
	context.Context
	step string
}

func newStepContext(step string) *stepContext {
	return &stepContext{Context: context.Background(), step: step}
}

func (*stepContext) FlowID() string                                  { return "typeform-test-flow" }
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

// formResponseJSON is one response with every documented answer type, including card details to drop.
func formResponseJSON(token string) string {
	return fmt.Sprintf(`{
  "token": %q,
  "landed_at": "2026-09-30T11:50:00Z",
  "submitted_at": "2026-09-30T11:58:59Z",
  "calculated": {"score": 9},
  "variables": [{"key": "score", "type": "number", "number": 4}, {"key": "name", "type": "text", "text": "typeform"}],
  "hidden": {"user_id": "abc123456", "campaign": 7, "empty": null},
  "answers": [
    {"type": "text", "text": "Ada", "field": {"id": "JwWggjAKtOkA", "type": "short_text", "ref": "first_name"}},
    {"type": "email", "email": "ada@example.com", "field": {"id": "SMEUb7VJz92Q", "type": "email", "ref": "email"}},
    {"type": "date", "date": "2005-10-15", "field": {"id": "KoJxDM3c6x8h", "type": "date"}},
    {"type": "boolean", "boolean": false, "field": {"id": "RUqkXSeXBXSd", "type": "yes_no", "ref": "consent"}},
    {"type": "number", "number": 4.5, "field": {"id": "WOTdC00F8A3h", "type": "rating", "ref": "rating"}},
    {"type": "choice", "choice": {"id": "4WIlUvKOl0UB", "label": "London", "ref": "london"}, "field": {"id": "k6TP9oLGgHjl", "type": "multiple_choice", "ref": "city"}},
    {"type": "choices", "choices": {"ids": ["eXnU3oA141Cg", "aTZmZGYV6liX"], "labels": ["London", "Sydney"], "refs": ["l", "s"], "other": "Paris"}, "field": {"id": "PNe8ZKBK8C2Q", "type": "picture_choice", "ref": "cities"}},
    {"type": "choices", "choices": [{"id": "m3hE6bENFNpI", "label": "I accept", "ref": "accept"}], "field": {"id": "OU3E4w2zbpno", "type": "checkbox", "ref": "terms"}},
    {"type": "url", "url": "https://calendly.com/scheduled_events/EVENT/invitees/INVITEE", "field": {"id": "M5tXK5kG7IeA", "type": "calendly", "ref": "booking"}},
    {"type": "file_url", "file_url": "https://api.typeform.com/responses/files/abc/cv.pdf", "field": {"id": "F9M3CArY00dS", "type": "file_upload", "ref": "cv"}},
    {"type": "phone_number", "phone_number": "+14155550100", "field": {"id": "PH0N3fieldA1", "type": "phone_number", "ref": "phone"}},
    {"type": "payment", "payment": {"amount": "1", "last4": "4242", "name": "CARDHOLDER-NAME", "success": true}, "field": {"id": "T6E2XAwU83AS", "type": "payment", "ref": "payment"}},
    {"type": "signature", "signature": {"url": "https://api.typeform.com/responses/files/sig.png", "type": "drawn"}, "field": {"id": "9tz6uYjjiSWr", "type": "signature", "ref": "signature"}},
    {"type": "multi_format", "multi_format": {"video_id": "vid-1", "transcript": "Hello"}, "field": {"id": "wwVznbEmbUUq", "type": "multi_format", "ref": "video"}},
    {"type": "matrix_row", "matrix_row": {"x": 1}, "field": {"id": "UNKNOWNfield", "type": "matrix", "ref": "grid"}}
  ],
  "ending": {"id": "dN5FLyFpCMFo", "ref": "thanks"}
}`, token)
}

func pointerTo[T any](value T) *T { return &value }

// expectedAnswers are the typed answers formResponseJSON decodes to, without definition titles.
func expectedAnswers() []typeform.FormAnswer {
	return []typeform.FormAnswer{
		{FieldID: "JwWggjAKtOkA", FieldRef: "first_name", FieldType: "short_text", Type: typeform.AnswerTypeText, Text: "Ada"},
		{FieldID: "SMEUb7VJz92Q", FieldRef: "email", FieldType: "email", Type: typeform.AnswerTypeEmail, Email: "ada@example.com"},
		{FieldID: "KoJxDM3c6x8h", FieldType: "date", Type: typeform.AnswerTypeDate, Date: "2005-10-15"},
		{FieldID: "RUqkXSeXBXSd", FieldRef: "consent", FieldType: "yes_no", Type: typeform.AnswerTypeBoolean, Boolean: pointerTo(false)},
		{FieldID: "WOTdC00F8A3h", FieldRef: "rating", FieldType: "rating", Type: typeform.AnswerTypeNumber, Number: pointerTo(4.5)},
		{FieldID: "k6TP9oLGgHjl", FieldRef: "city", FieldType: "multiple_choice", Type: typeform.AnswerTypeChoice,
			Choice: &typeform.FormAnswerChoice{ID: "4WIlUvKOl0UB", Label: "London", Ref: "london"}},
		{FieldID: "PNe8ZKBK8C2Q", FieldRef: "cities", FieldType: "picture_choice", Type: typeform.AnswerTypeChoices,
			Choices: &typeform.FormAnswerChoices{IDs: []string{"eXnU3oA141Cg", "aTZmZGYV6liX"}, Labels: []string{"London", "Sydney"}, Refs: []string{"l", "s"}, Other: "Paris"}},
		{FieldID: "OU3E4w2zbpno", FieldRef: "terms", FieldType: "checkbox", Type: typeform.AnswerTypeChoices,
			Choices: &typeform.FormAnswerChoices{IDs: []string{"m3hE6bENFNpI"}, Labels: []string{"I accept"}, Refs: []string{"accept"}}},
		{FieldID: "M5tXK5kG7IeA", FieldRef: "booking", FieldType: "calendly", Type: typeform.AnswerTypeURL, URL: "https://calendly.com/scheduled_events/EVENT/invitees/INVITEE"},
		{FieldID: "F9M3CArY00dS", FieldRef: "cv", FieldType: "file_upload", Type: typeform.AnswerTypeFileURL, FileURL: "https://api.typeform.com/responses/files/abc/cv.pdf"},
		{FieldID: "PH0N3fieldA1", FieldRef: "phone", FieldType: "phone_number", Type: typeform.AnswerTypePhoneNumber, PhoneNumber: "+14155550100"},
		{FieldID: "T6E2XAwU83AS", FieldRef: "payment", FieldType: "payment", Type: typeform.AnswerTypePayment,
			Payment: &typeform.FormAnswerPayment{Amount: "1", IsSuccessful: true}},
		{FieldID: "9tz6uYjjiSWr", FieldRef: "signature", FieldType: "signature", Type: typeform.AnswerTypeSignature,
			Signature: &typeform.FormAnswerSignature{URL: "https://api.typeform.com/responses/files/sig.png", Method: "drawn"}},
		{FieldID: "wwVznbEmbUUq", FieldRef: "video", FieldType: "multi_format", Type: typeform.AnswerTypeMultiFormat,
			MultiFormat: &typeform.FormAnswerMultiFormat{VideoID: "vid-1", Transcript: "Hello"}},
		{FieldID: "UNKNOWNfield", FieldRef: "grid", FieldType: "matrix", Type: "matrix_row"},
	}
}

// expectedResponse is formResponseJSON decoded, without definition titles.
func expectedResponse(token string) typeform.FormResponse {
	return typeform.FormResponse{
		Token: token, LandedAt: time.Date(2026, time.September, 30, 11, 50, 0, 0, time.UTC),
		SubmittedAt: time.Date(2026, time.September, 30, 11, 58, 59, 0, time.UTC), Answers: expectedAnswers(),
		HiddenFields: map[string]string{"user_id": "abc123456", "campaign": "7", "empty": ""},
		Variables: []typeform.FormVariable{
			{Key: "score", Type: "number", Number: pointerTo(4.0)}, {Key: "name", Type: "text", Text: "typeform"},
		},
		Score: pointerTo(9.0), EndingRef: "thanks",
	}
}
