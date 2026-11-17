// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sell

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type sellWriteContext struct {
	context.Context
	attempt   int32
	mutex     sync.Mutex
	heartbeat []byte
}

func newSellWriteContext() *sellWriteContext {
	return &sellWriteContext{Context: context.Background(), attempt: 1}
}
func (*sellWriteContext) FlowID() string                                  { return "sell-provider-test" }
func (*sellWriteContext) RunID() string                                   { return "run" }
func (*sellWriteContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (*sellWriteContext) StepExecutionID() string                         { return "write" }
func (*sellWriteContext) FromStepExecutionID() string                     { return "" }
func (*sellWriteContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*sellWriteContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (ctx *sellWriteContext) Attempt() int32                              { return ctx.attempt }
func (*sellWriteContext) HasTimerFired() bool                             { return false }
func (*sellWriteContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*sellWriteContext) WaitForMethodFailed() bool                       { return false }
func (*sellWriteContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*sellWriteContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*sellWriteContext) RecordEvent(string, any) error                   { return nil }
func (ctx *sellWriteContext) RecordHeartbeat(value any) error {
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	encoded, err := json.Marshal(value)
	ctx.heartbeat = encoded
	return err
}
func (ctx *sellWriteContext) GetLastHeartbeatValue(target any) (bool, error) {
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	if len(ctx.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(ctx.heartbeat, target)
}

var _ dex.Context = (*sellWriteContext)(nil)

func newSellWriteClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint + "/v2"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString(testAccessToken)}})
	require.NoError(t, err)
	return client
}

func TestCreateLeadSendsOneEnvelopeAndKeepsProviderFieldsOutOfResult(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v2/leads", request.URL.Path)
		require.Equal(t, "Bearer "+testAccessToken, request.Header.Get("Authorization"))
		var body map[string]map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, "Lovelace", body["data"]["last_name"])
		require.NotContains(t, body["data"], "owner_id")
		writer.WriteHeader(http.StatusCreated)
		_, err := writer.Write([]byte(`{"data":{"id":144,"first_name":"Ada","last_name":"Lovelace","email":"ada@example.com","custom_fields":{"secret":"never-store"}}}`))
		require.NoError(t, err)
	}))
	defer provider.Close()
	result, err := sdkgo.RunMutation(newSellWriteContext(), newSellWriteClient(t, provider.URL).CreateLead(), testConnection, CreateLeadInput{FirstName: "Ada", LastName: "Lovelace", EmailAddress: "ada@example.com"})
	require.NoError(t, err)
	require.Equal(t, CreateLeadBranchCreated, result.Branch)
	require.Equal(t, int64(144), result.Value.ID)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "never-store")
}

func TestUpdateLeadRetriesServerFailureAndMapsNotFound(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusBadGateway} {
		provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			require.Equal(t, http.MethodPut, request.Method)
			require.Equal(t, "/v2/leads/144", request.URL.Path)
			writer.WriteHeader(status)
		}))
		value := "New"
		result, err := sdkgo.RunMutation(newSellWriteContext(), newSellWriteClient(t, provider.URL).UpdateLead(), testConnection, UpdateLeadInput{LeadID: "144", Status: &value})
		if status == http.StatusNotFound {
			require.NoError(t, err)
			require.Equal(t, UpdateLeadBranchNotFound, result.Branch)
		} else {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		}
		provider.Close()
	}
}

func TestDeleteLeadAcceptsEmptyNoContentAndRetriesRateLimit(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusTooManyRequests} {
		provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			require.Equal(t, http.MethodDelete, request.Method)
			require.Equal(t, "/v2/leads/144", request.URL.Path)
			writer.WriteHeader(status)
		}))
		result, err := sdkgo.RunMutation(newSellWriteContext(), newSellWriteClient(t, provider.URL).DeleteLead(), testConnection, DeleteLeadInput{LeadID: "144"})
		if status == http.StatusNoContent {
			require.NoError(t, err)
			require.Equal(t, DeleteLeadBranchDeleted, result.Branch)
		} else {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		}
		provider.Close()
	}
}
