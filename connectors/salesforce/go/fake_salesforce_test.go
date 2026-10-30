// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	salesforceTestToken = "salesforce-token-SENTINEL"
	// providerMessageSentinel appears only in provider error messages, which must never leave the connector.
	providerMessageSentinel = "SENTINEL jane@acme.example.com"
)

var salesforceConnection = sdkgo.ConnectionRef{Provider: "salesforce", Name: "salesforce-crm"}

// recordedSalesforceRequest is one request the fake Salesforce server received.
type recordedSalesforceRequest struct {
	method  string
	path    string
	rawPath string
	query   map[string][]string
	header  http.Header
	body    []byte
}

// fakeSalesforce serves one handler per test and records every request.
type fakeSalesforce struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedSalesforceRequest
}

func newFakeSalesforce(t *testing.T, handler func(response http.ResponseWriter, request *http.Request, body []byte)) *fakeSalesforce {
	t.Helper()
	fake := &fakeSalesforce{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(response, "unreadable request", http.StatusBadRequest)
			return
		}
		fake.mutex.Lock()
		fake.requests = append(fake.requests, recordedSalesforceRequest{
			method: request.Method, path: request.URL.Path, rawPath: request.URL.EscapedPath(),
			query: request.URL.Query(), header: request.Header.Clone(), body: body,
		})
		fake.mutex.Unlock()
		response.Header().Set("Sforce-Limit-Info", "api-usage=18/15000")
		handler(response, request, body)
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (fake *fakeSalesforce) recorded() []recordedSalesforceRequest {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]recordedSalesforceRequest(nil), fake.requests...)
}

func newSalesforceClient(t *testing.T, instanceURL string, config ...salesforce.Config) *salesforce.Client {
	t.Helper()
	clientConfig := salesforce.Config{}
	if len(config) == 1 {
		clientConfig = config[0]
	}
	client, err := salesforce.New(clientConfig, sdkgo.StaticCredentialProvider[salesforce.Credentials]{
		salesforceConnection: {
			AuthMethodID: salesforce.ProductionOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(salesforceTestToken),
			InstanceURL: instanceURL,
		},
	})
	require.NoError(t, err)
	return client
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err := response.Write([]byte(body))
	require.NoError(t, err)
}

// requireRetry asserts that err is the SDK Retry error of failureKind and returns its failure.
func requireRetry(t *testing.T, err error, failureKind sdkgo.FailureKind) sdkgo.Failure {
	t.Helper()
	require.Error(t, err)
	var retryErr *sdkgo.RetryError
	require.ErrorAs(t, err, &retryErr)
	require.Equal(t, failureKind, retryErr.Failure.Kind)
	require.NotContains(t, retryErr.Error(), "SENTINEL")
	return retryErr.Failure
}

type dexContext struct {
	context.Context
	step string
}

func newDexContext(step string) *dexContext {
	return &dexContext{Context: context.Background(), step: step}
}
func (*dexContext) FlowID() string                                  { return "salesforce-flow" }
func (*dexContext) RunID() string                                   { return "run" }
func (*dexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *dexContext) StepExecutionID() string                 { return context.step }
func (*dexContext) FromStepExecutionID() string                     { return "" }
func (*dexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*dexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*dexContext) Attempt() int32                                  { return 1 }
func (*dexContext) HasTimerFired() bool                             { return false }
func (*dexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*dexContext) WaitForMethodFailed() bool                       { return false }
func (*dexContext) RecordHeartbeat(any) error                       { return nil }
func (*dexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*dexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*dexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*dexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*dexContext)(nil)
