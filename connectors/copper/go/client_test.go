// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "synthetic-copper-key"
const testUserEmail = "user@example.com"

var testConnection = sdkgo.ConnectionRef{Provider: "copper", Name: "copper"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "copper-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey), UserEmail: sdkgo.NewSecretString(testUserEmail)}})
	require.NoError(t, err)
	return client
}

func TestListLeadStatusesAuthPrivacyAndRetry(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/developer_api/v1/lead_statuses", r.URL.Path)
		require.Equal(t, testAPIKey, r.Header.Get("X-PW-AccessToken"))
		require.Equal(t, testUserEmail, r.Header.Get("X-PW-UserEmail"))
		require.Equal(t, "developer_api", r.Header.Get("X-PW-Application"))
		require.Equal(t, "Dex Connector (https://superdurable.io)", r.Header.Get("User-Agent"))
		if status.Load() != 0 {
			w.WriteHeader(int(status.Load()))
			return
		}
		_, err := w.Write([]byte(`[{"id":208231,"name":"New","private":"not-persisted"}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/developer_api/v1")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListLeadStatuses(), testConnection, ListLeadStatusesInput{})
	require.NoError(t, err)
	require.Equal(t, ListLeadStatusesBranchListed, result.Branch)
	require.Equal(t, []LeadStatusMetadata{{ID: 208231, Name: "New"}}, result.Value.Statuses)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	for _, code := range []int{429, 503} {
		status.Store(int32(code))
		_, err = sdkgo.RunQuery(queryContext{}, client.ListLeadStatuses(), testConnection, ListLeadStatusesInput{})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAPIKey)
	}
	status.Store(403)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListLeadStatuses(), testConnection, ListLeadStatusesInput{})
	require.NoError(t, err)
	require.Equal(t, ListLeadStatusesBranchProviderRejected, result.Branch)
}

func TestGetLeadClassifiesMissingAndOmitsCustomFields(t *testing.T) {
	var missing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/developer_api/v1/leads/8894157", r.URL.Path)
		if missing.Load() {
			w.WriteHeader(404)
			return
		}
		_, err := w.Write([]byte(`{"id":8894157,"name":"Sample Lead","status":"New","company_name":"Example Company","custom_fields":{"private":"not-persisted"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/developer_api/v1")
	input := GetLeadInput{LeadID: "8894157"}
	result, err := sdkgo.RunQuery(queryContext{}, client.GetLead(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetLeadBranchFound, result.Branch)
	require.Equal(t, int64(8894157), result.Value.ID)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	missing.Store(true)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetLead(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetLeadBranchNotFound, result.Branch)
}

func TestGetLeadRejectsInvalidInputWithoutProviderCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer server.Close()
	client := newTestClient(t, server.URL+"/developer_api/v1")
	result, err := sdkgo.RunQuery(queryContext{}, client.GetLead(), testConnection, GetLeadInput{LeadID: "0"})
	require.NoError(t, err)
	require.Equal(t, GetLeadBranchDefect, result.Branch)
	require.Equal(t, int32(0), calls.Load())
}
