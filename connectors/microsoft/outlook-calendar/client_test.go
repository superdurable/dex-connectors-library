// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar_test

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
	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var calendarConnection = sdkgo.ConnectionRef{Provider: "microsoft", Name: "outlook-calendar-test"}

func TestNewRejectsInvalidConfigurationBeforeAnyCall(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[outlookcalendar.Credentials]{}
	_, err := outlookcalendar.New(outlookcalendar.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = outlookcalendar.New(outlookcalendar.Config{TenantID: "common"}, credentials)
	require.ErrorContains(t, err, "tenantId must be")
	_, err = outlookcalendar.New(outlookcalendar.Config{Mailbox: "Scheduling <scheduling@contoso.com>"}, credentials)
	require.ErrorContains(t, err, "mailbox must be")
	_, err = outlookcalendar.New(outlookcalendar.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = outlookcalendar.New(outlookcalendar.Config{}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	_, err = outlookcalendar.New(outlookcalendar.Config{}, credentials, outlookcalendar.WithLocalProviderURL("https://graph.example.com"))
	require.ErrorContains(t, err, "loopback")
	_, err = outlookcalendar.New(outlookcalendar.Config{}, credentials, outlookcalendar.WithLocalProviderURL("http://127.0.0.1:1/v1.0"))
	require.ErrorContains(t, err, "cannot contain a path")
	client, err := outlookcalendar.New(outlookcalendar.Config{
		TenantID: "contoso.onmicrosoft.com", Mailbox: "scheduling@contoso.com",
	}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestValidateTenantIDAcceptsOneDirectoryOnly(t *testing.T) {
	for _, tenantID := range []string{"72f988bf-86f1-41af-91ab-2d7cd011db47", "contoso.onmicrosoft.com", "contoso.com"} {
		require.NoError(t, outlookcalendar.ValidateTenantID(tenantID), tenantID)
	}
	for _, tenantID := range []string{"", "common", "organizations", "consumers", "fabrikam.com/../evil", "fabrikam.com?x=1", "-fabrikam.com", "fabrikam"} {
		err := outlookcalendar.ValidateTenantID(tenantID)
		require.Error(t, err, tenantID)
		if tenantID != "" {
			require.NotContains(t, err.Error(), tenantID, "the error never repeats the value")
		}
	}
}

func TestEveryRequestAsksForUTCTimesTextBodiesAndCarriesTheCallID(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, timedEventJSON("event-1", "Planning"))
	})
	client := newCalendarClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newCalendarDexContext("headers"), client.GetEvent(), calendarConnection, outlookcalendar.GetEventInput{EventID: "event-1"})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.GetEventBranchFound, result.Branch)
	request := provider.request(t, 0)
	require.Equal(t, "/v1.0/me/events/event-1", request.path)
	require.Equal(t, []string{`outlook.timezone="UTC"`, `outlook.body-content-type="text"`}, request.header.Values("Prefer"))
	require.Equal(t, string(result.Receipt.CallID), request.header.Get("client-request-id"))
	require.Equal(t, "Bearer calendar-token", request.header.Get("Authorization"))
	require.Equal(t, "graph-request", result.Receipt.ProviderRequestID)
}

func TestAppOnlyConnectionAddressesItsConfiguredMailbox(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, timedEventJSON("event-1", "Planning"))
	})
	client, err := outlookcalendar.New(outlookcalendar.Config{TenantID: "contoso.onmicrosoft.com", Mailbox: "scheduling@contoso.com"},
		appOnlyCredentials(), outlookcalendar.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newCalendarDexContext("app-only"), client.GetEvent(), calendarConnection, outlookcalendar.GetEventInput{EventID: "event-1"})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.GetEventBranchFound, result.Branch)
	require.Equal(t, "/v1.0/users/scheduling@contoso.com/events/event-1", provider.request(t, 0).path)
}

func TestAppOnlyConnectionWithoutAMailboxSelectsDefectWithoutARequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {})
	client, err := outlookcalendar.New(outlookcalendar.Config{}, appOnlyCredentials(), outlookcalendar.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newCalendarDexContext("no-mailbox"), client.GetEvent(), calendarConnection, outlookcalendar.GetEventInput{EventID: "event-1"})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.GetEventBranchDefect, result.Branch)
	require.Contains(t, result.Failure.Message, "mailbox")
	require.Zero(t, provider.requestCount())
}

func TestANextLinkToAnotherHostIsNeverFollowed(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"value":[],"@odata.nextLink":"https://graph.example.com/v1.0/me/calendarView?$skip=10"}`)
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newCalendarDexContext("foreign-next-link"), client.ListEvents(), calendarConnection, outlookcalendar.ListEventsInput{
		TimeMin: "2026-02-24T00:00:00Z", TimeMax: "2026-02-25T00:00:00Z",
	})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.ListEventsBranchInvalidResponse, result.Branch, "a nextLink to another host is never followed")
}

func TestUnauthorizedRequestRefreshesOnceAndRetriesWithReplacement(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			require.Equal(t, "Bearer rejected-token", request.Header.Get("Authorization"))
			writeJSON(t, response, http.StatusUnauthorized, `{"error":{"code":"InvalidAuthenticationToken","message":"SENTINEL"}}`)
			return
		}
		require.Equal(t, "Bearer replacement-token", request.Header.Get("Authorization"))
		writeJSON(t, response, http.StatusOK, timedEventJSON("event-1", "Planning"))
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := outlookcalendar.New(outlookcalendar.Config{}, credentials, outlookcalendar.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newCalendarDexContext("refresh-once"), client.GetEvent(), calendarConnection, outlookcalendar.GetEventInput{EventID: "event-1"})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.GetEventBranchFound, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, 1, credentials.forcedRefreshes)
}

func TestSecondUnauthorizedResponseIsTerminalWithoutARefreshLoop(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusUnauthorized, `{"error":{"code":"InvalidAuthenticationToken"}}`)
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := outlookcalendar.New(outlookcalendar.Config{}, credentials, outlookcalendar.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newCalendarDexContext("no-refresh-loop"), client.GetEvent(), calendarConnection, outlookcalendar.GetEventInput{EventID: "event-1"})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.GetEventBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, "provider rejected the request with HTTP 401 (InvalidAuthenticationToken)", result.Failure.Message)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, 1, credentials.forcedRefreshes)
}

func TestFailuresNeverRepeatProviderMessagesOrTokens(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusForbidden, `{"error":{"code":"ErrorAccessDenied","message":"SENTINEL owner@contoso.com cannot write","innerError":{"request-id":"SENTINEL"}}}`)
	})
	client := newCalendarClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newCalendarDexContext("safe-failure"), client.GetEvent(), calendarConnection, outlookcalendar.GetEventInput{EventID: "event-1"})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.GetEventBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	require.Equal(t, "provider rejected the request with HTTP 403 (ErrorAccessDenied)", result.Failure.Message)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
	require.NotContains(t, string(encoded), "calendar-token")
}

func TestAnErrorCodeThatIsNotWordShapedIsDropped(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadRequest, `{"error":{"code":"owner@contoso.com","message":"SENTINEL"}}`)
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newCalendarDexContext("odd-code"), client.GetEvent(), calendarConnection, outlookcalendar.GetEventInput{EventID: "event-1"})
	require.NoError(t, err)
	require.Equal(t, "provider rejected the request with HTTP 400", result.Failure.Message)
}

func TestThrottlingAndUnavailabilityRetryWithProviderDelay(t *testing.T) {
	for _, test := range []struct {
		status int
		kind   sdkgo.FailureKind
	}{
		{status: http.StatusTooManyRequests, kind: sdkgo.FailureRateLimit},
		{status: http.StatusServiceUnavailable, kind: sdkgo.FailureAvailability},
		{status: http.StatusGatewayTimeout, kind: sdkgo.FailureAvailability},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				response.Header().Set("Retry-After", "7")
				writeJSON(t, response, test.status, `{"error":{"code":"ApplicationThrottled"}}`)
			})
			client := newCalendarClient(t, provider.URL)
			_, err := sdkgo.RunQuery(newCalendarDexContext("retry"), client.GetEvent(), calendarConnection, outlookcalendar.GetEventInput{EventID: "event-1"})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.kind, retry.Failure.Kind)
			var retryAfter *dex.RetryAfterError
			require.ErrorAs(t, err, &retryAfter)
			require.Equal(t, 7*time.Second, retryAfter.After)
		})
	}
}

func TestOversizedResponseSelectsInvalidResponse(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, timedEventJSON("event-1", strings.Repeat("x", 512)))
	})
	client, err := outlookcalendar.New(outlookcalendar.Config{MaxResponseBytes: 256}, staticCalendarCredentials(), outlookcalendar.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newCalendarDexContext("oversized"), client.GetEvent(), calendarConnection, outlookcalendar.GetEventInput{EventID: "event-1"})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.GetEventBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestUnavailableCredentialsSelectDefectWithoutProviderRequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {})
	client, err := outlookcalendar.New(outlookcalendar.Config{}, sdkgo.StaticCredentialProvider[outlookcalendar.Credentials]{}, outlookcalendar.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newCalendarDexContext("no-credentials"), client.GetEvent(), calendarConnection, outlookcalendar.GetEventInput{EventID: "event-1"})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.GetEventBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Zero(t, provider.requestCount())
}

type rejectionRefreshingCredentialProvider struct {
	forcedRefreshes int
}

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (outlookcalendar.Credentials, error) {
	return outlookcalendar.Credentials{AuthMethodID: outlookcalendar.MicrosoftOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context,
	sdkgo.Call,
	sdkgo.CredentialRefreshDriver[outlookcalendar.Credentials],
) (outlookcalendar.Credentials, error) {
	provider.forcedRefreshes++
	return outlookcalendar.Credentials{AuthMethodID: outlookcalendar.MicrosoftOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("replacement-token")}, nil
}

type recordedRequest struct {
	method string
	path   string
	query  url.Values
	header http.Header
	body   []byte
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
			method: request.Method, path: request.URL.Path, query: request.URL.Query(), header: request.Header.Clone(), body: body,
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
	response.Header().Set("request-id", "graph-request")
	response.WriteHeader(status)
	_, err := response.Write([]byte(body))
	require.NoError(t, err)
}

// timedEventJSON is a Graph event at 17:00 to 18:00 UTC on 24 February 2026, as Prefer outlook.timezone="UTC" returns it.
func timedEventJSON(eventID string, subject string) string {
	return `{"@odata.etag":"W/\"etag-1\"","id":"` + eventID + `","subject":"` + subject + `","showAs":"busy","type":"singleInstance",` +
		`"start":{"dateTime":"2026-02-24T17:00:00.0000000","timeZone":"UTC"},` +
		`"end":{"dateTime":"2026-02-24T18:00:00.0000000","timeZone":"UTC"},` +
		`"originalStartTimeZone":"Pacific Standard Time","originalEndTimeZone":"Pacific Standard Time"}`
}

func mustReadBody(t *testing.T, request *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	return body
}

func decodeRequestBody(t *testing.T, request recordedRequest) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(request.body, &payload))
	return payload
}

func staticCalendarCredentials() sdkgo.StaticCredentialProvider[outlookcalendar.Credentials] {
	return sdkgo.StaticCredentialProvider[outlookcalendar.Credentials]{
		calendarConnection: {AuthMethodID: outlookcalendar.MicrosoftOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("calendar-token")},
	}
}

func appOnlyCredentials() sdkgo.StaticCredentialProvider[outlookcalendar.Credentials] {
	return sdkgo.StaticCredentialProvider[outlookcalendar.Credentials]{
		calendarConnection: {AuthMethodID: outlookcalendar.AppOnlyAuthMethodID, AccessToken: sdkgo.NewSecretString("calendar-token")},
	}
}

func newCalendarClient(t *testing.T, providerURL string) *outlookcalendar.Client {
	t.Helper()
	client, err := outlookcalendar.New(outlookcalendar.Config{}, staticCalendarCredentials(), outlookcalendar.WithLocalProviderURL(providerURL))
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
