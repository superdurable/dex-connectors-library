// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package getresponse

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

const testAPIKey = "test-secret-getresponse-key"

var testConnection = sdkgo.ConnectionRef{Provider: "getresponse", Name: "getresponse"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "getresponse-provider-test" }
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

func TestListContactsTokenPaginationAndRateLimit(t *testing.T) {
	var rateLimited atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/contacts", r.URL.Path)
		require.Equal(t, "2", r.URL.Query().Get("page"))
		require.Equal(t, "3", r.URL.Query().Get("perPage"))
		require.Empty(t, r.URL.Query().Get("api_key"))
		require.Equal(t, "api-key "+testAPIKey, r.Header.Get("X-Auth-Token"))
		if rateLimited.Load() {
			w.Header().Set("X-RateLimit-Reset", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, err := w.Write([]byte(`[{"contactId":"abc123","email":"ada@example.com","name":"Ada Lovelace","customFieldValues":[{"value":"not-persisted"}]}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v3")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListContacts(), testConnection, ListContactsInput{Page: 2, PerPage: 3})
	require.NoError(t, err)
	require.Equal(t, ListContactsBranchListed, result.Branch)
	require.Equal(t, 2, result.Value.Page)
	require.Equal(t, "abc123", result.Value.Contacts[0].ContactID)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	rateLimited.Store(true)
	_, err = sdkgo.RunQuery(queryContext{}, client.ListContacts(), testConnection, ListContactsInput{Page: 2, PerPage: 3})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAPIKey)
}

func TestGetContactExcludesRelationshipsAndClassifiesMissing(t *testing.T) {
	var missing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/contacts/abc123", r.URL.Path)
		require.Equal(t, "api-key "+testAPIKey, r.Header.Get("X-Auth-Token"))
		if missing.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, err := w.Write([]byte(`{"contactId":"abc123","email":"ada@example.com","name":"Ada Lovelace","customFieldValues":[{"value":"not-persisted"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v3")
	result, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ContactID: "abc123"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchFound, result.Branch)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	missing.Store(true)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ContactID: "abc123"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchNotFound, result.Branch)
}

func TestListContactsMalformedAndInvalidInput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, err := w.Write([]byte(`{"contacts":{}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v3")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListContacts(), testConnection, ListContactsInput{})
	require.NoError(t, err)
	require.Equal(t, ListContactsBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListContacts(), testConnection, ListContactsInput{PerPage: 1001})
	require.NoError(t, err)
	require.Equal(t, ListContactsBranchDefect, result.Branch)
	require.Equal(t, int32(1), calls.Load())
}
