// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testOrganizationID = "2389290"
	testAccessToken    = "1000.zohoDeskTestAccessToken0123456789"
	testTicketID       = "1892000000042034"
	testDepartmentID   = "1892000000006907"
	testContactID      = "1892000000042032"
	testAgentID        = "1892000000056007"
)

var deskConnection = sdkgo.ConnectionRef{Provider: "zoho", Name: "zoho-desk-test"}

// recordedRequest is one request a recordingDesk received.
type recordedRequest struct {
	method string
	path   string
	query  map[string][]string
	header http.Header
	body   string
}

// recordingDesk is a credential-safe httptest server that records requests and delegates replies.
type recordingDesk struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingDesk(t *testing.T, reply func(http.ResponseWriter, *http.Request, int)) *recordingDesk {
	t.Helper()
	provider := &recordingDesk{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		request.Body = io.NopCloser(bytes.NewReader(body))
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

func (provider *recordingDesk) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingDesk) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

func newDeskClient(t *testing.T, baseURL string, options ...desk.Option) *desk.Client {
	t.Helper()
	options = append([]desk.Option{desk.WithAPIBaseURL(baseURL + "/api/v1")}, options...)
	client, err := desk.New(desk.Config{OrgID: testOrganizationID}, testCredentialProvider(), options...)
	require.NoError(t, err)
	return client
}

func testCredentials(authMethodID string) desk.Credentials {
	return desk.Credentials{
		AuthMethodID: authMethodID, OAuthClientID: "1000.GMB0YULZHJK411248S8I5GZ4CHUEX0",
		OAuthClientSecret: sdkgo.NewSecretString("zoho-client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken),
		RefreshToken: sdkgo.NewSecretString("1000.zohoDeskTestRefreshToken"),
	}
}

func testCredentialProvider() sdkgo.StaticCredentialProvider[desk.Credentials] {
	return sdkgo.StaticCredentialProvider[desk.Credentials]{deskConnection: testCredentials(desk.USDataCenterAuthMethodID)}
}

// closedLoopbackURL returns an address nothing listens on, so a connection is refused before any request byte is sent.
func closedLoopbackURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return "http://" + address
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json;charset=UTF-8")
	response.Header().Set("X-Rate-Limit-Request-Weight-v3", "1")
	response.Header().Set("X-Rate-Limit-Remaining-v3", "49950")
	response.WriteHeader(status)
	_, err := io.WriteString(response, body)
	require.NoError(t, err)
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

// ticketJSON is a Zoho Desk ticket shaped like the documented samples: IDs and counts are strings.
func ticketJSON(id string, status string, statusType string) map[string]any {
	return map[string]any{
		"id": id, "ticketNumber": "101", "subject": "Double charge on order 88213",
		"description": "<div>I was charged twice.</div>", "status": status, "statusType": statusType, "priority": "High",
		"channel": "Email", "classification": nil, "departmentId": testDepartmentID, "contactId": testContactID,
		"assigneeId": testAgentID, "teamId": nil, "email": "jane@acme.example.com", "phone": "SENTINEL phone",
		"isEscalated": false, "isOverDue": "false", "isSpam": false, "threadCount": "2", "commentCount": "1",
		"createdTime": "2026-01-26T14:02:00.000Z", "modifiedTime": "2026-01-28T08:45:00.000Z",
		"dueDate": "2026-01-29T14:02:00.000Z", "closedTime": nil, "cf": map[string]any{"cf_note": "SENTINEL custom"},
		"webUrl": "https://desk.zoho.com/support/zylker/ShowHomePage.do#Cases/dv/d126330fb061247d",
	}
}

func ticketBodyJSON(id string, status string, statusType string) string {
	encoded, err := json.Marshal(ticketJSON(id, status, statusType))
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// requireNoSentinel proves a value carries neither provider message text nor the access token.
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

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (desk.Credentials, error) {
	credentials := testCredentials(desk.USDataCenterAuthMethodID)
	credentials.AccessToken = sdkgo.NewSecretString("1000.rejected-token")
	return credentials, nil
}

// ResolveWithRefresh returns the token Resolve returns, which has not expired before Zoho Desk rejects it.
func (provider *rejectionRefreshingCredentialProvider) ResolveWithRefresh(
	_ context.Context,
	call sdkgo.Call,
	_ sdkgo.CredentialRefreshDriver[desk.Credentials],
) (desk.Credentials, error) {
	return provider.Resolve(call)
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context,
	sdkgo.Call,
	sdkgo.CredentialRefreshDriver[desk.Credentials],
) (desk.Credentials, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.forcedRefreshes++
	credentials := testCredentials(desk.USDataCenterAuthMethodID)
	credentials.AccessToken = sdkgo.NewSecretString("1000.replacement-token")
	return credentials, nil
}

// deskDexContext is a dex.Context whose heartbeat reads the previous attempt's value, as Dex supplies it.
type deskDexContext struct {
	context.Context
	step              string
	previousHeartbeat json.RawMessage
	recordedHeartbeat json.RawMessage
	heartbeatCount    int
	rejectsHeartbeat  bool
	attempt           int32
}

func newDeskDexContext(step string) *deskDexContext {
	return &deskDexContext{Context: context.Background(), step: step, attempt: 1}
}

// nextAttempt is the following attempt of the same Step execution, which sees this attempt's last heartbeat.
func (context *deskDexContext) nextAttempt() *deskDexContext {
	return &deskDexContext{
		Context: context.Context, step: context.step, previousHeartbeat: context.recordedHeartbeat, attempt: context.attempt + 1,
	}
}

func (*deskDexContext) FlowID() string                          { return "zoho-desk-flow" }
func (*deskDexContext) RunID() string                           { return "run" }
func (*deskDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (context *deskDexContext) StepExecutionID() string         { return context.step }
func (*deskDexContext) FromStepExecutionID() string             { return "" }
func (*deskDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*deskDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (context *deskDexContext) Attempt() int32                  { return context.attempt }
func (*deskDexContext) HasTimerFired() bool                     { return false }
func (*deskDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*deskDexContext) WaitForMethodFailed() bool               { return false }
func (*deskDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*deskDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*deskDexContext) RecordEvent(string, any) error { return nil }

func (context *deskDexContext) RecordHeartbeat(value any) error {
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

func (context *deskDexContext) GetLastHeartbeatValue(target any) (bool, error) {
	if context.previousHeartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(context.previousHeartbeat, target)
}

var _ dex.Context = (*deskDexContext)(nil)
