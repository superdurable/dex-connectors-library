// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAccessToken  = "1000.zohoCRMTestAccessToken0123456789"
	testRefreshToken = "1000.zohoCRMTestRefreshToken"
	testContactID    = "4150868000000376008"
	testAccountID    = "4150868000000624046"
	testDealID       = "4150868000003194003"
	testOwnerID      = "4150868000000225013"
)

var crmConnection = sdkgo.ConnectionRef{Provider: "zoho", Name: "zoho-crm-test"}

// recordedRequest is one request a recordingCRM received.
type recordedRequest struct {
	method string
	host   string
	path   string
	query  url.Values
	header http.Header
	body   string
}

// recordingCRM is a credential-safe httptest server that records requests and delegates replies.
type recordingCRM struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingCRM(t *testing.T, reply func(http.ResponseWriter, *http.Request, int)) *recordingCRM {
	t.Helper()
	provider := &recordingCRM{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		request.Body = io.NopCloser(bytes.NewReader(body))
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recordedRequest{
			method: request.Method, host: request.Host, path: request.URL.Path, query: request.URL.Query(),
			header: request.Header.Clone(), body: string(body),
		})
		provider.mutex.Unlock()
		reply(response, request, index)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingCRM) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingCRM) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

// selectQuery returns the COQL statement of a recorded POST /crm/v8/coql request.
func (provider *recordingCRM) selectQuery(t *testing.T, index int) string {
	t.Helper()
	var body struct {
		SelectQuery string `json:"select_query"`
	}
	require.NoError(t, json.Unmarshal([]byte(provider.request(index).body), &body))
	return body.SelectQuery
}

func newCRMClient(t *testing.T, baseURL string, options ...crm.Option) *crm.Client {
	t.Helper()
	options = append([]crm.Option{crm.WithAPIBaseURL(baseURL + "/crm/v8")}, options...)
	client, err := crm.New(crm.Config{}, testCredentialProvider(), options...)
	require.NoError(t, err)
	return client
}

func testCredentials(authMethodID string) crm.Credentials {
	return crm.Credentials{
		AuthMethodID: authMethodID, OAuthClientID: "1000.GMB0YULZHJK411248S8I5GZ4CHUEX0",
		OAuthClientSecret: sdkgo.NewSecretString("zoho-client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken),
		RefreshToken: sdkgo.NewSecretString(testRefreshToken), APIDomain: "https://www.zohoapis.com",
	}
}

func testCredentialProvider() sdkgo.StaticCredentialProvider[crm.Credentials] {
	return sdkgo.StaticCredentialProvider[crm.Credentials]{crmConnection: testCredentials(crm.USDataCenterAuthMethodID)}
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json;charset=UTF-8")
	response.Header().Set("X-API-CREDITS-REMAINING", "48211")
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

// contactJSON is a contact shaped like Zoho CRM's samples, with $ keys and a lookup.
func contactJSON(id string, email string, modifiedTime string) map[string]any {
	return map[string]any{
		"id": id, "Email": email, "First_Name": "Jane", "Last_Name": "Smith", "Modified_Time": modifiedTime,
		"Account_Name": map[string]any{"name": "Acme Corp", "id": testAccountID},
		"Owner":        map[string]any{"name": "Patricia Boyle", "id": testOwnerID, "email": "p.boyle@zylker.example.com"},
		"$approval":    map[string]any{"approve": false}, "$editable": true, "$currency_symbol": "$",
	}
}

func writeSuccessJSON(action string, recordID string, duplicateField any) map[string]any {
	return map[string]any{"data": []any{map[string]any{
		"code": "SUCCESS", "duplicate_field": duplicateField, "action": action, "status": "success",
		"message": "SENTINEL record written",
		"details": map[string]any{
			"Modified_Time": "2026-01-28T13:00:05+05:30", "Created_Time": "2026-01-28T13:00:05+05:30", "id": recordID,
			"Modified_By": map[string]any{"name": "Patricia Boyle", "id": testOwnerID},
		},
	}}}
}

func recordErrorJSON(code string, apiName string, extraDetails map[string]any) map[string]any {
	details := map[string]any{"api_name": apiName, "json_path": "$.data[0]." + apiName}
	for key, value := range extraDetails {
		details[key] = value
	}
	return map[string]any{"data": []any{map[string]any{"code": code, "details": details, "message": "SENTINEL " + code, "status": "error"}}}
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

// routingClient sends requests for the named hosts to the server and refuses every other host.
func routingClient(t *testing.T, server *httptest.Server, hosts ...string) *http.Client {
	t.Helper()
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	return &http.Client{Timeout: 5 * time.Second, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		for _, host := range hosts {
			if request.URL.Host == host {
				routed := request.Clone(request.Context())
				routed.URL.Scheme, routed.URL.Host, routed.Host = target.Scheme, target.Host, host
				return http.DefaultTransport.RoundTrip(routed)
			}
		}
		return nil, errors.New("request to an unexpected host")
	})}
}

// crmDexContext is a dex.Context for running one operation outside a Worker.
type crmDexContext struct {
	context.Context
	step string
}

func newCRMDexContext(step string) *crmDexContext {
	return &crmDexContext{Context: context.Background(), step: step}
}

func (*crmDexContext) FlowID() string                                  { return "zoho-crm-flow" }
func (*crmDexContext) RunID() string                                   { return "run" }
func (*crmDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *crmDexContext) StepExecutionID() string                 { return context.step }
func (*crmDexContext) FromStepExecutionID() string                     { return "" }
func (*crmDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*crmDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*crmDexContext) Attempt() int32                                  { return 1 }
func (*crmDexContext) HasTimerFired() bool                             { return false }
func (*crmDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*crmDexContext) WaitForMethodFailed() bool                       { return false }
func (*crmDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*crmDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*crmDexContext) RecordEvent(string, any) error                   { return nil }
func (*crmDexContext) RecordHeartbeat(any) error                       { return nil }
func (*crmDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }

var _ dex.Context = (*crmDexContext)(nil)
