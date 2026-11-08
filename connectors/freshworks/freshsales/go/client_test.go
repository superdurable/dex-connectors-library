// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshsales

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

const testAPIKey = "test-secret-freshsales-key"

var testConnection = sdkgo.ConnectionRef{Provider: "freshsales", Name: "freshsales"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "freshsales-provider-test" }
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

func TestListContactViewsTokenPaginationAndRateLimit(t *testing.T) {
	var rateLimited atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/crm/sales/api/contacts/filters", r.URL.Path)
		require.Empty(t, r.URL.Query().Get("offset"))
		require.Empty(t, r.URL.Query().Get("limit"))
		require.Empty(t, r.URL.Query().Get("api_key"))
		require.Equal(t, "Token token="+testAPIKey, r.Header.Get("Authorization"))
		if rateLimited.Load() {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, err := w.Write([]byte(`{"filters":[{"id":1,"name":"All Contacts","private":"not-persisted"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/crm/sales/api")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListContactViews(), testConnection, ListContactViewsInput{})
	require.NoError(t, err)
	require.Equal(t, ListContactViewsBranchListed, result.Branch)
	require.Equal(t, []ContactViewMetadata{{ID: 1, Name: "All Contacts"}}, result.Value.Views)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	rateLimited.Store(true)
	_, err = sdkgo.RunQuery(queryContext{}, client.ListContactViews(), testConnection, ListContactViewsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAPIKey)
}

func TestGetContactExcludesRelationshipsAndClassifiesMissing(t *testing.T) {
	var missing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/crm/sales/api/contacts/144", r.URL.Path)
		require.Equal(t, "Token token="+testAPIKey, r.Header.Get("Authorization"))
		if missing.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, err := w.Write([]byte(`{"contact":{"id":144,"email":"ada@example.com","first_name":"Ada","last_name":"Lovelace","custom_field":{"private":"not-persisted"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/crm/sales/api")
	result, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ContactID: "144"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchFound, result.Branch)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	missing.Store(true)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ContactID: "144"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchNotFound, result.Branch)
}

func TestListContactViewsMalformedAndInvalidInput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, err := w.Write([]byte(`{"filters":null}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/crm/sales/api")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListContactViews(), testConnection, ListContactViewsInput{})
	require.NoError(t, err)
	require.Equal(t, ListContactViewsBranchInvalidResponse, result.Branch)
	contact, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ContactID: "0"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchDefect, contact.Branch)
	require.Equal(t, int32(1), calls.Load())
}
