// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var calendarConnection = sdkgo.ConnectionRef{Provider: "google", Name: "google-calendar-test"}

func TestNewRejectsInsecureEndpointsAndInvalidLimits(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[calendar.Credentials]{}
	_, err := calendar.New(calendar.Config{Endpoint: "http://calendar.example.com"}, credentials)
	require.ErrorContains(t, err, "HTTPS")
	_, err = calendar.New(calendar.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = calendar.New(calendar.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = calendar.New(calendar.Config{}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	client, err := calendar.New(calendar.Config{}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestUnauthorizedRequestRefreshesOnceAndRetriesWithReplacement(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			require.Equal(t, "Bearer rejected-token", request.Header.Get("Authorization"))
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		require.Equal(t, "Bearer replacement-token", request.Header.Get("Authorization"))
		writeJSON(t, response, http.StatusOK, timedEventJSON("event12345", "Planning"))
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := calendar.New(calendar.Config{Endpoint: provider.URL}, credentials)
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newCalendarDexContext("refresh-once"), client.GetEvent(), calendarConnection, calendar.GetEventInput{CalendarID: "primary", EventID: "event12345"})
	require.NoError(t, err)
	require.Equal(t, calendar.GetEventBranchFound, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, 1, credentials.forcedRefreshes)
}

func TestSecondUnauthorizedResponseIsTerminalWithoutARefreshLoop(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.WriteHeader(http.StatusUnauthorized)
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := calendar.New(calendar.Config{Endpoint: provider.URL}, credentials)
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newCalendarDexContext("no-refresh-loop"), client.GetEvent(), calendarConnection, calendar.GetEventInput{CalendarID: "primary", EventID: "event12345"})
	require.NoError(t, err)
	require.Equal(t, calendar.GetEventBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, 1, credentials.forcedRefreshes)
}

func TestFailuresNeverRepeatProviderMessagesOrTokens(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusForbidden, `{"error":{"code":403,"message":"SENTINEL owner@example.com cannot write","errors":[{"reason":"requiredAccessLevel","message":"SENTINEL detail"},{"reason":"SENTINEL-unknown"}]}}`)
	})
	client := newCalendarClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newCalendarDexContext("safe-failure"), client.GetEvent(), calendarConnection, calendar.GetEventInput{CalendarID: "primary", EventID: "event12345"})
	require.NoError(t, err)
	require.Equal(t, calendar.GetEventBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	require.Equal(t, "provider rejected the request with HTTP 403 (requiredAccessLevel)", result.Failure.Message)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
	require.NotContains(t, string(encoded), "calendar-token")
}

func TestRateLimitsRetryWithProviderDelay(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "429", status: http.StatusTooManyRequests, body: `{}`},
		{name: "403 user rate limit", status: http.StatusForbidden, body: `{"error":{"errors":[{"domain":"usageLimits","reason":"userRateLimitExceeded"}]}}`},
		{name: "403 project rate limit", status: http.StatusForbidden, body: `{"error":{"errors":[{"domain":"usageLimits","reason":"rateLimitExceeded"}]}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				response.Header().Set("Retry-After", "7")
				writeJSON(t, response, test.status, test.body)
			})
			client := newCalendarClient(t, provider.URL)
			_, err := sdkgo.RunQuery(newCalendarDexContext("rate-limit-"+test.name), client.GetEvent(), calendarConnection, calendar.GetEventInput{CalendarID: "primary", EventID: "event12345"})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
			var retryAfter *dex.RetryAfterError
			require.ErrorAs(t, err, &retryAfter)
			require.Equal(t, 7*time.Second, retryAfter.After)
		})
	}
}

func TestDailyQuotaIsTerminal(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusForbidden, `{"error":{"errors":[{"domain":"usageLimits","reason":"quotaExceeded"}]}}`)
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newCalendarDexContext("quota"), client.GetEvent(), calendarConnection, calendar.GetEventInput{CalendarID: "primary", EventID: "event12345"})
	require.NoError(t, err)
	require.Equal(t, calendar.GetEventBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureQuotaExhausted, result.Failure.Kind)
}

func TestOversizedResponseSelectsInvalidResponse(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, timedEventJSON("event12345", strings.Repeat("x", 512)))
	})
	client, err := calendar.New(calendar.Config{Endpoint: provider.URL, MaxResponseBytes: 256}, staticCalendarCredentials())
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newCalendarDexContext("oversized"), client.GetEvent(), calendarConnection, calendar.GetEventInput{CalendarID: "primary", EventID: "event12345"})
	require.NoError(t, err)
	require.Equal(t, calendar.GetEventBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestUnavailableCredentialsSelectDefectWithoutProviderRequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {})
	client, err := calendar.New(calendar.Config{Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[calendar.Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newCalendarDexContext("no-credentials"), client.GetEvent(), calendarConnection, calendar.GetEventInput{CalendarID: "primary", EventID: "event12345"})
	require.NoError(t, err)
	require.Equal(t, calendar.GetEventBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Zero(t, provider.requestCount())
}

type rejectionRefreshingCredentialProvider struct {
	forcedRefreshes int
}

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (calendar.Credentials, error) {
	return calendar.Credentials{AuthMethodID: calendar.GoogleOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

// ResolveWithRefresh returns the stored credential unchanged: its expiry has not passed.
func (provider *rejectionRefreshingCredentialProvider) ResolveWithRefresh(
	_ context.Context,
	call sdkgo.Call,
	_ sdkgo.CredentialRefreshDriver[calendar.Credentials],
) (calendar.Credentials, error) {
	return provider.Resolve(call)
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context,
	sdkgo.Call,
	sdkgo.CredentialRefreshDriver[calendar.Credentials],
) (calendar.Credentials, error) {
	provider.forcedRefreshes++
	return calendar.Credentials{AuthMethodID: calendar.GoogleOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("replacement-token")}, nil
}

type recordedRequest struct {
	method  string
	path    string
	query   url.Values
	body    []byte
	ifMatch string
}

// recordingProvider is an httptest server that records requests and delegates each to a handler.
type recordingProvider struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingProvider(t *testing.T, handler func(http.ResponseWriter, *http.Request, int)) *recordingProvider {
	t.Helper()
	provider := &recordingProvider{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		request.Body = io.NopCloser(bytes.NewReader(body))
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recordedRequest{
			method: request.Method, path: request.URL.EscapedPath(), query: request.URL.Query(), body: body,
			ifMatch: request.Header.Get("If-Match"),
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

func (provider *recordingProvider) methods() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	methods := make([]string, len(provider.requests))
	for index, request := range provider.requests {
		methods[index] = request.method
	}
	return methods
}

func (provider *recordingProvider) request(t *testing.T, index int) recordedRequest {
	t.Helper()
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Less(t, index, len(provider.requests))
	return provider.requests[index]
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Goog-Request-Id", "google-request")
	response.WriteHeader(status)
	_, err := response.Write([]byte(body))
	require.NoError(t, err)
}

func timedEventJSON(eventID string, summary string) string {
	return `{"id":"` + eventID + `","status":"confirmed","summary":"` + summary + `","etag":"\"3181161784712000\"",` +
		`"start":{"dateTime":"2026-02-24T09:00:00-08:00","timeZone":"America/Los_Angeles"},` +
		`"end":{"dateTime":"2026-02-24T10:00:00-08:00","timeZone":"America/Los_Angeles"}}`
}

func decodeRequestBody(t *testing.T, request recordedRequest) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(request.body, &payload))
	return payload
}

func staticCalendarCredentials() sdkgo.StaticCredentialProvider[calendar.Credentials] {
	return sdkgo.StaticCredentialProvider[calendar.Credentials]{
		calendarConnection: {AuthMethodID: calendar.GoogleOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("calendar-token")},
	}
}

func newCalendarClient(t *testing.T, endpoint string) *calendar.Client {
	t.Helper()
	client, err := calendar.New(calendar.Config{Endpoint: endpoint}, staticCalendarCredentials())
	require.NoError(t, err)
	return client
}

type calendarDexContext struct {
	context.Context
	step string
}

func newCalendarDexContext(step string) *calendarDexContext {
	return &calendarDexContext{Context: context.Background(), step: step}
}
func (*calendarDexContext) FlowID() string                                  { return "calendar-flow" }
func (*calendarDexContext) RunID() string                                   { return "run" }
func (*calendarDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *calendarDexContext) StepExecutionID() string                 { return context.step }
func (*calendarDexContext) FromStepExecutionID() string                     { return "" }
func (*calendarDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*calendarDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*calendarDexContext) Attempt() int32                                  { return 1 }
func (*calendarDexContext) HasTimerFired() bool                             { return false }
func (*calendarDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*calendarDexContext) WaitForMethodFailed() bool                       { return false }
func (*calendarDexContext) RecordHeartbeat(any) error                       { return nil }
func (*calendarDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*calendarDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*calendarDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*calendarDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*calendarDexContext)(nil)
