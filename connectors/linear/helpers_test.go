// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
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
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// testAPIKey is split so that no complete Linear key shape appears in source.
	testAPIKey        = "lin_" + "api_" + "SENTINELKEY0123456789abcdefghijklmnopqrst"
	testAccessToken   = "SENTINEL-LINEAR-ACCESS-TOKEN"
	testSigningSecret = "SENTINEL-LINEAR-SIGNING-SECRET"
	testTeamID        = "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"
	testStateID       = "5a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
	testOtherStateID  = "6b2c3d4e-5f6a-4b7c-9d8e-0f1a2b3c4d5e"
	testUserID        = "7c3d4e5f-6a7b-4c8d-ae9f-1a2b3c4d5e6f"
	testLabelID       = "8d4e5f6a-7b8c-4d9e-bf0a-2b3c4d5e6f7a"
	testIssueID       = "9e5f6a7b-8c9d-4e0f-8a1b-3c4d5e6f7a8b"
)

var linearConnection = sdkgo.ConnectionRef{Provider: "linear", Name: "linear-test"}

// recordedRequest is one GraphQL request a recordingLinear received.
type recordedRequest struct {
	method        string
	path          string
	header        http.Header
	operationName string
	query         string
	variables     map[string]any
}

// recordingLinear is a credential-safe httptest server that records GraphQL requests and delegates replies.
type recordingLinear struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingLinear(t *testing.T, reply func(http.ResponseWriter, recordedRequest, int)) *recordingLinear {
	t.Helper()
	provider := &recordingLinear{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		var document struct {
			OperationName string         `json:"operationName"`
			Query         string         `json:"query"`
			Variables     map[string]any `json:"variables"`
		}
		require.NoError(t, json.Unmarshal(body, &document), "every request is a GraphQL JSON body")
		recorded := recordedRequest{
			method: request.Method, path: request.URL.Path, header: request.Header.Clone(),
			operationName: document.OperationName, query: document.Query, variables: document.Variables,
		}
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recorded)
		provider.mutex.Unlock()
		reply(response, recorded, index)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingLinear) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingLinear) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

func (provider *recordingLinear) operationNames() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	names := make([]string, 0, len(provider.requests))
	for _, request := range provider.requests {
		names = append(names, request.operationName)
	}
	return names
}

func newLinearClient(t *testing.T, apiURL string, credentials linear.CredentialSource, options ...linear.Option) *linear.Client {
	t.Helper()
	client, err := linear.New(linear.Config{}, credentials, append([]linear.Option{linear.WithAPIURL(apiURL + "/graphql")}, options...)...)
	require.NoError(t, err)
	return client
}

func apiKeyCredentials() sdkgo.StaticCredentialProvider[linear.Credentials] {
	return sdkgo.StaticCredentialProvider[linear.Credentials]{linearConnection: {
		AuthMethodID: linear.PersonalAPIKeyAuthMethodID, APIKey: sdkgo.NewSecretString(testAPIKey),
		WebhookSigningSecret: sdkgo.NewSecretString(testSigningSecret),
	}}
}

func newAPIKeyClient(t *testing.T, apiURL string, options ...linear.Option) *linear.Client {
	t.Helper()
	return newLinearClient(t, apiURL, apiKeyCredentials(), options...)
}

// runQuery runs one Query attempt as a Step execution named step would.
func runQuery[IN, OUT any](step string, operation sdkgo.Query[IN, OUT], input IN) (sdkgo.QueryResult[OUT], error) {
	return sdkgo.RunQuery(newLinearDexContext(step), operation, linearConnection, input)
}

// runMutation runs one Mutation attempt; the same step reuses the Call ID, and so the client-supplied UUID.
func runMutation[IN, OUT any](step string, operation sdkgo.Mutation[IN, OUT], input IN) (sdkgo.MutationResult[OUT], error) {
	return sdkgo.RunMutation(newLinearDexContext(step), operation, linearConnection, input)
}

// requireRetry asserts a Retry of kind; a zero delay means the Step's own retry policy applies.
func requireRetry(t *testing.T, err error, kind sdkgo.FailureKind, delay time.Duration) *sdkgo.RetryError {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, kind, retry.Failure.Kind, retry.Failure.Message)
	var retryAfter *dex.RetryAfterError
	if delay > 0 {
		require.ErrorAs(t, err, &retryAfter)
		require.Equal(t, delay, retryAfter.After)
	} else {
		require.False(t, errors.As(err, &retryAfter), "no delay falls back to the Step retry policy")
	}
	require.NotContains(t, retry.Failure.Message, "SENTINEL")
	return retry
}

// closedLoopbackURL returns an address nothing listens on, so a connection is refused before any byte is sent.
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
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("X-Request-Id", "request-0001")
	response.WriteHeader(status)
	_, err := io.WriteString(response, body)
	require.NoError(t, err)
}

func writeData(t *testing.T, response http.ResponseWriter, data any) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"data": data})
	require.NoError(t, err)
	writeJSON(t, response, http.StatusOK, string(encoded))
}

// linearError is Linear's GraphQL error shape; message text is a sentinel the connector must never repeat.
func linearError(code string, errorType string) string {
	return `{"errors":[{"message":"SENTINEL provider message","extensions":{"type":"` + errorType + `","code":"` + code + `","userError":true,"userPresentableMessage":"SENTINEL presentable"}}]}`
}

func issueSummaryJSON(id string, identifier string, title string) map[string]any {
	return map[string]any{
		"id": id, "identifier": identifier, "title": title, "url": "https://linear.app/acme/issue/" + identifier,
		"priority": 2, "dueDate": "2026-02-18", "labelIds": []any{testLabelID},
		"createdAt": "2026-01-28T10:00:00.000Z", "updatedAt": "2026-01-28T10:05:00.000Z", "archivedAt": nil,
		"team":     map[string]any{"id": testTeamID, "key": "ENG", "name": "Engineering"},
		"state":    map[string]any{"id": testStateID, "name": "Todo", "type": "unstarted"},
		"assignee": map[string]any{"id": testUserID},
	}
}

// linearDexContext is a minimal dex.Context for running one operation outside a Worker.
type linearDexContext struct {
	context.Context
	step string
}

func newLinearDexContext(step string) *linearDexContext {
	return &linearDexContext{Context: context.Background(), step: step}
}

func (*linearDexContext) FlowID() string                          { return "linear-flow" }
func (*linearDexContext) RunID() string                           { return "run" }
func (*linearDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (dexContext *linearDexContext) StepExecutionID() string      { return dexContext.step }
func (*linearDexContext) FromStepExecutionID() string             { return "" }
func (*linearDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*linearDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (*linearDexContext) Attempt() int32                          { return 1 }
func (*linearDexContext) HasTimerFired() bool                     { return false }
func (*linearDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*linearDexContext) WaitForMethodFailed() bool               { return false }
func (*linearDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*linearDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*linearDexContext) RecordEvent(string, any) error           { return nil }
func (*linearDexContext) RecordHeartbeat(any) error               { return nil }
func (*linearDexContext) GetLastHeartbeatValue(any) (bool, error) { return false, nil }

var _ dex.Context = (*linearDexContext)(nil)
