// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drip

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

const testAPIKey = "synthetic-drip-key"

var testConnection = sdkgo.ConnectionRef{Provider: "drip", Name: "drip"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "drip-provider-test" }
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

func TestListAccountsBasicAuthPrivacyAndRetry(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/accounts", r.URL.Path)
		username, password, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, testAPIKey, username)
		require.Empty(t, password)
		require.Equal(t, "Dex Connector (https://superdurable.io)", r.Header.Get("User-Agent"))
		if status.Load() != 0 {
			w.WriteHeader(int(status.Load()))
			return
		}
		_, err := w.Write([]byte(`{"accounts":[{"id":"9999999","name":"Acme","primary_email":"private@example.com"}],"links":{"accounts.campaigns":"secret"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v2")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListAccounts(), testConnection, ListAccountsInput{})
	require.NoError(t, err)
	require.Equal(t, ListAccountsBranchListed, result.Branch)
	require.Equal(t, []AccountMetadata{{ID: "9999999", Name: "Acme"}}, result.Value.Accounts)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private@example.com")
	for _, code := range []int{429, 503} {
		status.Store(int32(code))
		_, err = sdkgo.RunQuery(queryContext{}, client.ListAccounts(), testConnection, ListAccountsInput{})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAPIKey)
	}
	status.Store(403)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListAccounts(), testConnection, ListAccountsInput{})
	require.NoError(t, err)
	require.Equal(t, ListAccountsBranchProviderRejected, result.Branch)
}

func TestGetSubscriberEncodesEmailAndClassifiesMissing(t *testing.T) {
	var missing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/9999999/subscribers/ada@example.com", r.URL.Path)
		if missing.Load() {
			w.WriteHeader(404)
			return
		}
		_, err := w.Write([]byte(`{"subscribers":[{"id":"subscriber-1","email":"ada@example.com","first_name":"Ada","last_name":"Lovelace","status":"active","custom_fields":{"private":"not-persisted"}}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v2")
	input := GetSubscriberInput{AccountID: "9999999", SubscriberIDOrEmail: "ada@example.com"}
	result, err := sdkgo.RunQuery(queryContext{}, client.GetSubscriber(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetSubscriberBranchFound, result.Branch)
	require.Equal(t, "subscriber-1", result.Value.ID)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	missing.Store(true)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetSubscriber(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetSubscriberBranchNotFound, result.Branch)
}

func TestGetSubscriberRejectsInvalidInputWithoutProviderCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v2")
	result, err := sdkgo.RunQuery(queryContext{}, client.GetSubscriber(), testConnection, GetSubscriberInput{AccountID: "0", SubscriberIDOrEmail: "a@example.com"})
	require.NoError(t, err)
	require.Equal(t, GetSubscriberBranchDefect, result.Branch)
	require.Equal(t, int32(0), calls.Load())
}
