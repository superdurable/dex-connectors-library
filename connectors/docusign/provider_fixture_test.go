// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign_test

import (
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
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// sentinelAccessToken stands in for every access token; no Result, Failure, or Receipt may contain it.
	sentinelAccessToken = "SENTINEL-DOCUSIGN-ACCESS-TOKEN"
	// sentinelClientSecret stands in for the integration's secret key.
	sentinelClientSecret = "SENTINEL-DOCUSIGN-CLIENT-SECRET"
	// sentinelRefreshToken stands in for the refresh token.
	sentinelRefreshToken = "SENTINEL-DOCUSIGN-REFRESH-TOKEN"
	// sentinelHMACKey stands in for the Connect HMAC key.
	sentinelHMACKey = "SENTINEL-DOCUSIGN-CONNECT-HMAC-KEY"
	// sentinelMessage is DocuSign error text that must never reach a Failure.
	sentinelMessage = "SENTINEL provider message text"

	testAccountID      = "a4ec37d6-1111-2222-3333-143885c220e1"
	otherAccountID     = "a4ec37d6-4444-5555-6666-143885c333aa"
	testEnvelopeID     = "93be49ab-1111-2222-3333-f752070d71ec"
	otherEnvelopeID    = "93be49ab-4444-5555-6666-f752070d71ec"
	testTemplateID     = "8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a"
	productionBaseHost = "na3.docusign.net"
	accountPath        = "/restapi/v2.1/accounts/" + testAccountID
	envelopePath       = accountPath + "/envelopes/" + testEnvelopeID
)

var (
	testConnection = sdkgo.ConnectionRef{Provider: "docusign", Name: "esignature"}
	fixedNow       = time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
)

// fakeDocuSign stands in for DocuSign's hosts over TLS and records each request's addressed host.
type fakeDocuSign struct {
	*httptest.Server
	mu       sync.Mutex
	routes   map[string]http.HandlerFunc
	requests []recordedRequest
}

type recordedRequest struct {
	method        string
	host          string
	path          string
	query         url.Values
	authorization string
	accept        string
	body          string
}

// newFakeDocuSign serves "METHOD /path" routes; default userinfo names the na3 test account, unknown routes 404.
func newFakeDocuSign(t *testing.T, routes map[string]http.HandlerFunc) *fakeDocuSign {
	t.Helper()
	fake := &fakeDocuSign{routes: map[string]http.HandlerFunc{
		"GET /oauth/userinfo": respondJSON(http.StatusOK, userInfoJSON(testAccountID, "https://"+productionBaseHost)),
	}}
	for key, route := range routes {
		fake.routes[key] = route
	}
	fake.Server = httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(response, "unreadable", http.StatusBadRequest)
			return
		}
		fake.mu.Lock()
		fake.requests = append(fake.requests, recordedRequest{
			method: request.Method, host: request.Header.Get(originalHostHeader), path: request.URL.Path, query: request.URL.Query(),
			authorization: request.Header.Get("Authorization"), accept: request.Header.Get("Accept"), body: string(body),
		})
		route, isFound := fake.routes[request.Method+" "+request.URL.Path]
		fake.mu.Unlock()
		if !isFound {
			writeJSON(response, http.StatusNotFound, `{"errorCode":"RESOURCE_NOT_FOUND","message":"`+sentinelMessage+`"}`)
			return
		}
		request.Body = io.NopCloser(strings.NewReader(string(body)))
		route(response, request)
	}))
	t.Cleanup(fake.Close)
	return fake
}

// originalHostHeader carries the host the connector addressed through the redirecting client.
const originalHostHeader = "X-Test-Original-Host"

func (fake *fakeDocuSign) recordedRequests() []recordedRequest {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]recordedRequest(nil), fake.requests...)
}

func (fake *fakeDocuSign) requestsTo(method string, path string) []recordedRequest {
	matches := []recordedRequest{}
	for _, request := range fake.recordedRequests() {
		if request.method == method && request.path == path {
			matches = append(matches, request)
		}
	}
	return matches
}

// redirectingClient sends every request, whatever its host, to the fake server.
func (fake *fakeDocuSign) redirectingClient() *http.Client {
	target, err := url.Parse(fake.URL)
	if err != nil {
		panic(err)
	}
	base := fake.Client().Transport
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		redirected := request.Clone(request.Context())
		redirected.Header.Set(originalHostHeader, request.URL.Host)
		redirected.URL.Scheme, redirected.URL.Host, redirected.Host = target.Scheme, target.Host, target.Host
		return base.RoundTrip(redirected)
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func respondJSON(status int, body string) http.HandlerFunc {
	return func(response http.ResponseWriter, _ *http.Request) { writeJSON(response, status, body) }
}

func writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, _ = io.WriteString(response, body) // A failed write leaves the client to classify a broken response.
}

// docusignError is an eSignature error body whose message the connector must never repeat.
func docusignError(errorCode string) string {
	return `{"errorCode":"` + errorCode + `","message":"` + sentinelMessage + `"}`
}

// userInfoJSON lists a non-default account on eu.docusign.net and the default account at baseURI.
func userInfoJSON(defaultAccountID string, baseURI string) string {
	return `{"sub":"4799e5e9-0000-0000-0000-cf4713bbcacc","name":"Dana Sender","email":"dana@example.com","accounts":[` +
		`{"account_id":"` + otherAccountID + `","is_default":false,"account_name":"Example Europe","base_uri":"https://eu.docusign.net"},` +
		`{"account_id":"` + defaultAccountID + `","is_default":true,"account_name":"Example","base_uri":"` + baseURI + `"}]}`
}

// envelopeJSON is an envelope resource carrying the marker custom field and fields the connector drops.
func envelopeJSON(envelopeID string, status string, marker string) string {
	return `{"envelopeId":"` + envelopeID + `","status":"` + status + `","emailSubject":"Meridian MSA",` +
		`"createdDateTime":"2026-10-04T10:00:00.1230000Z","sentDateTime":"2026-10-04T10:00:01.0000000Z",` +
		`"statusChangedDateTime":"2026-10-04T11:00:00.0000000Z","expireDateTime":"2026-12-03T10:00:01.0000000Z",` +
		`"documentsUri":"/envelopes/` + envelopeID + `/documents","customFields":{"textCustomFields":[` +
		`{"fieldId":"1","name":"dexIdempotencyKey","show":"false","required":"false","value":"` + marker + `"},` +
		`{"fieldId":"2","name":"dexSigningRequestId","show":"false","required":"false","value":"opp-123"}]}}`
}

// newTestClient builds a client whose requests reach fake whatever host they address.
func newTestClient(t *testing.T, fake *fakeDocuSign, credentials docusign.CredentialSource, config docusign.Config, options ...docusign.Option) *docusign.Client {
	t.Helper()
	options = append([]docusign.Option{docusign.WithHTTPClient(fake.redirectingClient()), docusign.WithClock(func() time.Time { return fixedNow })}, options...)
	client, err := docusign.New(config, credentials, options...)
	require.NoError(t, err)
	return client
}

func productionCredentials() docusign.Credentials {
	return docusign.Credentials{
		AuthMethodID: docusign.ProductionOAuthAuthMethodID, OAuthClientID: "integration-key",
		OAuthClientSecret: sdkgo.NewSecretString(sentinelClientSecret), AccessToken: sdkgo.NewSecretString(sentinelAccessToken),
		RefreshToken: sdkgo.NewSecretString(sentinelRefreshToken), ConnectHMACKey: sdkgo.NewSecretString(sentinelHMACKey),
	}
}

func staticCredentials(credentials docusign.Credentials) sdkgo.StaticCredentialProvider[docusign.Credentials] {
	return sdkgo.StaticCredentialProvider[docusign.Credentials]{testConnection: credentials}
}

// requireNoSecrets fails when a Result, Failure, or Receipt repeats a credential or provider message.
func requireNoSecrets(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	for _, secret := range []string{sentinelAccessToken, sentinelClientSecret, sentinelRefreshToken, sentinelHMACKey, sentinelMessage} {
		require.NotContains(t, string(encoded), secret)
	}
}

// sdkgoJSON is value as it would be persisted in Dex.
func sdkgoJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}

// testDexContext is a deterministic Dex context whose heartbeat value persists like a regular attempt's.
type testDexContext struct {
	context.Context
	step           string
	attempt        int32
	mu             sync.Mutex
	heartbeatValue json.RawMessage
	heartbeats     int
}

func newTestDexContext(step string) *testDexContext {
	return &testDexContext{Context: context.Background(), step: step, attempt: 1}
}

// newRetriedTestDexContext is a later attempt of step whose earlier attempt left no heartbeat.
func newRetriedTestDexContext(step string, attempt int32) *testDexContext {
	return &testDexContext{Context: context.Background(), step: step, attempt: attempt}
}

func (*testDexContext) FlowID() string                          { return "docusign-flow" }
func (*testDexContext) RunID() string                           { return "run" }
func (*testDexContext) FlowStartedAt() time.Time                { return fixedNow }
func (ctx *testDexContext) StepExecutionID() string             { return ctx.step }
func (*testDexContext) FromStepExecutionID() string             { return "" }
func (*testDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*testDexContext) FirstAttemptAt() time.Time               { return fixedNow }
func (ctx *testDexContext) Attempt() int32                      { return ctx.attempt }
func (*testDexContext) HasTimerFired() bool                     { return false }
func (*testDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*testDexContext) WaitForMethodFailed() bool               { return false }
func (*testDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*testDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*testDexContext) RecordEvent(string, any) error { return nil }

// RecordHeartbeat stores the value as Dex would hand it to the next regular attempt; nil clears it.
func (ctx *testDexContext) RecordHeartbeat(value any) error {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.heartbeats++
	if value == nil {
		ctx.heartbeatValue = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	ctx.heartbeatValue = encoded
	return nil
}

func (ctx *testDexContext) GetLastHeartbeatValue(valuePointer any) (bool, error) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if ctx.heartbeatValue == nil {
		return false, nil
	}
	return true, json.Unmarshal(ctx.heartbeatValue, valuePointer)
}

func (ctx *testDexContext) lastHeartbeat() json.RawMessage {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	return ctx.heartbeatValue
}

var _ dex.Context = (*testDexContext)(nil)
