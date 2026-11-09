// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaignmonitor

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

const testAPIKey = "synthetic-campaignmonitor-key"

var testConnection = sdkgo.ConnectionRef{Provider: "campaignmonitor", Name: "campaignmonitor"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "campaignmonitor-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	return client
}

func TestListClientsBasicAuthenticationAndPrivacy(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v3.3/clients.json", r.URL.Path)
		username, password, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, testAPIKey, username)
		require.Empty(t, password)
		require.Equal(t, "application/json", r.Header.Get("Accept"))
		if status.Load() != 0 {
			w.WriteHeader(int(status.Load()))
			return
		}
		_, err := w.Write([]byte(`[{"ClientID":"client-1","Name":"Acme","ApiKey":"never-persist"}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/v3.3")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListClients(), testConnection, ListClientsInput{})
	require.NoError(t, err)
	require.Equal(t, ListClientsBranchListed, result.Branch)
	require.Equal(t, []ClientMetadata{{ID: "client-1", Name: "Acme"}}, result.Value.Clients)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "never-persist")
	for _, code := range []int{429, 503} {
		status.Store(int32(code))
		_, err = sdkgo.RunQuery(queryContext{}, client.ListClients(), testConnection, ListClientsInput{})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAPIKey)
	}
	status.Store(403)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListClients(), testConnection, ListClientsInput{})
	require.NoError(t, err)
	require.Equal(t, ListClientsBranchProviderRejected, result.Branch)
}

func TestListClientsRejectsMalformedRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`[{"ClientID":"client-1","Name":""}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/v3.3")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListClients(), testConnection, ListClientsInput{})
	require.NoError(t, err)
	require.Equal(t, ListClientsBranchInvalidResponse, result.Branch)
}
