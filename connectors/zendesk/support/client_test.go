// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"context"
	"encoding/base64"
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
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testSubdomain = "acme"
	testEmail     = "agent@acme.example.com"
	testAPIToken  = "zendeskTestToken0123456789abcdef"
)

var zendeskConnection = sdkgo.ConnectionRef{Provider: "zendesk", Name: "zendesk-support-test"}

func TestNewValidatesSubdomainOptionsAndLimits(t *testing.T) {
	credentials := testCredentialProvider()
	for _, subdomain := range []string{"", "Acme", "acme.zendesk.com", "https://acme.zendesk.com", "-acme", "acme-", "ac_me", strings.Repeat("a", 64)} {
		_, err := support.New(support.Config{Subdomain: subdomain}, credentials)
		require.Error(t, err, subdomain)
	}
	_, err := support.New(support.Config{Subdomain: testSubdomain, MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = support.New(support.Config{Subdomain: testSubdomain}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = support.New(support.Config{Subdomain: testSubdomain}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	for _, baseURL := range []string{"http://zendesk.example.com/api/v2", "https://user:secret@acme.zendesk.com/api/v2", "https://acme.zendesk.com/api/v2?x=1"} {
		_, err = support.New(support.Config{Subdomain: testSubdomain}, credentials, support.WithAPIBaseURL(baseURL))
		require.Error(t, err, baseURL)
		require.NotContains(t, err.Error(), "secret")
	}
	client, err := support.New(support.Config{Subdomain: "acme-support2"}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestRequestsUseTheAPITokenAsBasicAuthenticationWithJSONHeaders(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(35436, "open", nil))
	})
	client := newZendeskClient(t, provider.URL)
	_, err := sdkgo.RunQuery(newZendeskDexContext("basic-auth"), client.GetTicket(), zendeskConnection, support.GetTicketInput{TicketID: 35436})
	require.NoError(t, err)
	request := provider.request(0)
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(testEmail+"/token:"+testAPIToken))
	require.Equal(t, expected, request.header.Get("Authorization"))
	require.Equal(t, "application/json", request.header.Get("Accept"))
	require.Equal(t, "/api/v2/tickets/35436", request.path)
}

func TestTicketAgentURLAlwaysUsesTheSubdomain(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(35436, "open", nil))
			return
		}
		writeJSON(t, response, http.StatusOK, `{"comments":[],"meta":{"has_more":false}}`)
	})
	result, err := sdkgo.RunQuery(newZendeskDexContext("agent-url"), newZendeskClient(t, provider.URL).GetTicket(), zendeskConnection, support.GetTicketInput{TicketID: 35436})
	require.NoError(t, err)
	require.Equal(t, "https://acme.zendesk.com/agent/tickets/35436", result.Value.Ticket.AgentURL)
}

func TestFailureStatusesSelectBranchesWithOnlyZendeskErrorTokens(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		kind    sdkgo.FailureKind
		message string
	}{
		{
			name: "record invalid", status: http.StatusUnprocessableEntity,
			body:   `{"error":"RecordInvalid","description":"SENTINEL Record validation errors","details":{"status":[{"type":"invalid","description":"SENTINEL Status: closed is not valid"}],"Bad Key":[{"type":"x"}]}}`,
			branch: support.GetTicketBranchProviderRejected, kind: sdkgo.FailureValidation,
			message: "Zendesk rejected the request (HTTP 422) [RecordInvalid; details: status=invalid]",
		},
		{
			name: "authentication text", status: http.StatusUnauthorized, body: `{"error":"Couldn't authenticate you"}`,
			branch: support.GetTicketBranchProviderRejected, kind: sdkgo.FailureAuthentication, message: "Zendesk rejected the request (HTTP 401)",
		},
		{
			name: "forbidden", status: http.StatusForbidden, body: `{"error":"Forbidden","description":"SENTINEL You do not have access to this page"}`,
			branch: support.GetTicketBranchProviderRejected, kind: sdkgo.FailureAuthorization, message: "Zendesk rejected the request (HTTP 403) [Forbidden]",
		},
		{
			name: "not found", status: http.StatusNotFound, body: `{"error":"RecordNotFound","description":"SENTINEL Not found"}`,
			branch: support.GetTicketBranchNotFound, kind: sdkgo.FailureNotFound, message: "Zendesk found no such resource (HTTP 404) [RecordNotFound]",
		},
		{
			name: "plain text", status: http.StatusBadRequest, body: `SENTINEL Please use HTTPS`,
			branch: support.GetTicketBranchProviderRejected, kind: sdkgo.FailureValidation, message: "Zendesk rejected the request (HTTP 400)",
		},
		{
			name: "reflected token", status: http.StatusBadRequest, body: `{"error":"` + testAPIToken + `","details":{"` + "token_" + `":[{"type":"` + testAPIToken + `"}]}}`,
			branch: support.GetTicketBranchProviderRejected, kind: sdkgo.FailureValidation, message: "Zendesk rejected the request (HTTP 400) [details: token_]",
		},
		{
			name: "redirect", status: http.StatusFound, body: ``,
			branch: support.GetTicketBranchProviderRejected, kind: sdkgo.FailureProtocol,
			message: "Zendesk redirected the request (HTTP 302); check that the subdomain is the zendesk.com account subdomain",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.status == http.StatusFound {
					response.Header().Set("Location", "https://attacker.example.com/steal")
				}
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newZendeskDexContext("failure-"+test.name), newZendeskClient(t, provider.URL).GetTicket(), zendeskConnection, support.GetTicketInput{TicketID: 35436})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, test.message, result.Failure.Message)
			require.Equal(t, 1, provider.requestCount(), "redirects are never followed")
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "SENTINEL")
			require.NotContains(t, string(encoded), testAPIToken)
		})
	}
}

func TestRetryableStatusesHonorRetryAfter(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
		kind       sdkgo.FailureKind
		delay      time.Duration
	}{
		{name: "rate limit", status: http.StatusTooManyRequests, retryAfter: "13", kind: sdkgo.FailureRateLimit, delay: 13 * time.Second},
		{name: "database timeout", status: http.StatusServiceUnavailable, retryAfter: "4", kind: sdkgo.FailureAvailability, delay: 4 * time.Second},
		{name: "server error", status: http.StatusInternalServerError, kind: sdkgo.FailureAvailability},
		{name: "request timeout", status: http.StatusRequestTimeout, kind: sdkgo.FailureAvailability},
		{name: "conflict", status: http.StatusConflict, kind: sdkgo.FailureConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.retryAfter != "" {
					response.Header().Set("Retry-After", test.retryAfter)
				}
				writeJSON(t, response, test.status, `{"error":"SENTINEL busy"}`)
			})
			_, err := sdkgo.RunQuery(newZendeskDexContext("retry-"+test.name), newZendeskClient(t, provider.URL).GetTicket(), zendeskConnection, support.GetTicketInput{TicketID: 35436})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.kind, retry.Failure.Kind)
			require.NotContains(t, retry.Failure.Message, "SENTINEL")
			var retryAfter *dex.RetryAfterError
			if test.delay == 0 {
				require.False(t, errors.As(err, &retryAfter), "no Retry-After uses the Step retry policy")
				return
			}
			require.ErrorAs(t, err, &retryAfter)
			require.Equal(t, test.delay, retryAfter.After)
		})
	}
}

func TestTransportFailureRetries(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	_, err = sdkgo.RunQuery(newZendeskDexContext("transport"), newZendeskClient(t, "http://"+address+"/api/v2").GetTicket(), zendeskConnection, support.GetTicketInput{TicketID: 35436})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
}

func TestOversizedAndCredentialReflectingResponsesSelectInvalidResponse(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		kind sdkgo.FailureKind
	}{
		{name: "oversized", body: `{"ticket":{"id":1,"description":"` + strings.Repeat("x", 4096) + `"}}`, kind: sdkgo.FailureResponseTooLarge},
		{name: "reflects credential", body: `{"ticket":{"id":35436,"subject":"` + testAPIToken + `"}}`, kind: sdkgo.FailureProtocol},
		{name: "malformed", body: `{"ticket":{"id":"not-a-number"}}`, kind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, test.body)
			})
			client, err := support.New(support.Config{Subdomain: testSubdomain, MaxResponseBytes: 1024}, testCredentialProvider(), support.WithAPIBaseURL(provider.URL+"/api/v2"))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newZendeskDexContext("invalid-"+test.name), client.GetTicket(), zendeskConnection, support.GetTicketInput{TicketID: 35436})
			require.NoError(t, err)
			require.Equal(t, support.GetTicketBranchInvalidResponse, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), testAPIToken)
		})
	}
}

func TestUnusableCredentialsSelectDefectBeforeAnyRequest(t *testing.T) {
	for name, credentials := range map[string]support.Credentials{
		"missing token":    {Email: testEmail},
		"display address":  {Email: "Agent <agent@acme.example.com>", APIToken: sdkgo.NewSecretString(testAPIToken)},
		"token with space": {Email: testEmail, APIToken: sdkgo.NewSecretString("two words")},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingZendesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
			client, err := support.New(support.Config{Subdomain: testSubdomain},
				sdkgo.StaticCredentialProvider[support.Credentials]{zendeskConnection: credentials}, support.WithAPIBaseURL(provider.URL+"/api/v2"))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newZendeskDexContext("credentials-"+name), client.GetTicket(), zendeskConnection, support.GetTicketInput{TicketID: 35436})
			require.NoError(t, err)
			require.Equal(t, support.GetTicketBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			require.Equal(t, "connection credentials are unavailable", result.Failure.Message)
			require.Zero(t, provider.requestCount())
		})
	}
}

// recordedRequest is one request a recordingZendesk received.
type recordedRequest struct {
	method string
	path   string
	query  map[string][]string
	header http.Header
	body   string
}

// recordingZendesk is a credential-safe httptest server that records requests and delegates replies.
type recordingZendesk struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingZendesk(t *testing.T, reply func(http.ResponseWriter, *http.Request, int)) *recordingZendesk {
	t.Helper()
	provider := &recordingZendesk{}
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

func (provider *recordingZendesk) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingZendesk) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

func newZendeskClient(t *testing.T, baseURL string) *support.Client {
	t.Helper()
	if !strings.HasSuffix(baseURL, "/api/v2") {
		baseURL += "/api/v2"
	}
	client, err := support.New(support.Config{Subdomain: testSubdomain}, testCredentialProvider(), support.WithAPIBaseURL(baseURL))
	require.NoError(t, err)
	return client
}

func testCredentialProvider() sdkgo.StaticCredentialProvider[support.Credentials] {
	return sdkgo.StaticCredentialProvider[support.Credentials]{zendeskConnection: {Email: testEmail, APIToken: sdkgo.NewSecretString(testAPIToken)}}
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Zendesk-Request-Id", "request-0001")
	response.WriteHeader(status)
	_, err := io.WriteString(response, body)
	require.NoError(t, err)
}

func ticketJSON(id int64, status string, tags []string) map[string]any {
	if tags == nil {
		tags = []string{"billing"}
	}
	return map[string]any{
		"id": id, "url": "https://acme.zendesk.com/api/v2/tickets/1.json", "subject": "Double charge on order 88213",
		"description": "I was charged twice.", "status": status, "priority": "high", "type": "problem",
		"requester_id": 20978392, "submitter_id": 20978392, "assignee_id": 235323, "group_id": 98738,
		"organization_id": 509974, "brand_id": 1234, "tags": tags, "external_id": "ERP-88213",
		"via": map[string]any{"channel": "email"}, "created_at": "2026-01-26T14:02:00Z", "updated_at": "2026-01-28T08:45:00Z",
		"due_at": nil, "custom_status_id": 123, "satisfaction_rating": map[string]any{"comment": "SENTINEL rating text"},
	}
}

func ticketEnvelopeJSON(id int64, status string, tags []string) string {
	encoded, err := json.Marshal(map[string]any{"ticket": ticketJSON(id, status, tags)})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

type zendeskDexContext struct {
	context.Context
	step string
}

func newZendeskDexContext(step string) *zendeskDexContext {
	return &zendeskDexContext{Context: context.Background(), step: step}
}
func (*zendeskDexContext) FlowID() string                                  { return "zendesk-flow" }
func (*zendeskDexContext) RunID() string                                   { return "run" }
func (*zendeskDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *zendeskDexContext) StepExecutionID() string                 { return context.step }
func (*zendeskDexContext) FromStepExecutionID() string                     { return "" }
func (*zendeskDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*zendeskDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*zendeskDexContext) Attempt() int32                                  { return 1 }
func (*zendeskDexContext) HasTimerFired() bool                             { return false }
func (*zendeskDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*zendeskDexContext) WaitForMethodFailed() bool                       { return false }
func (*zendeskDexContext) RecordHeartbeat(any) error                       { return nil }
func (*zendeskDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*zendeskDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*zendeskDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*zendeskDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*zendeskDexContext)(nil)
