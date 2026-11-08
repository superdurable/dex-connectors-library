// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package nimble

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

const testAPIKey = "synthetic-nimble-token"
const testContactID = "5049fb849b85f669e40000dc"

var testConnection = sdkgo.ConnectionRef{Provider: "nimble", Name: "nimble"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "nimble-provider-test" }
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

func TestListContactIDsBearerPrivacyAndRetry(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/contacts/ids", r.URL.Path)
		require.Equal(t, "page=2&per_page=20", r.URL.RawQuery)
		require.Equal(t, "Bearer "+testAPIKey, r.Header.Get("Authorization"))
		require.Equal(t, "Dex Connector (https://superdurable.io)", r.Header.Get("User-Agent"))
		if status.Load() != 0 {
			w.WriteHeader(int(status.Load()))
			return
		}
		_, err := w.Write([]byte(`{"meta":{"page":2,"pages":3,"per_page":20,"total":43},"resources":["5049fb849b85f669e40000dc"]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/v1")
	input := ListContactIDsInput{Page: 2, PerPage: 20}
	result, err := sdkgo.RunQuery(queryContext{}, client.ListContactIDs(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, ListContactIDsBranchListed, result.Branch)
	require.Equal(t, ContactIDsPage{IDs: []string{testContactID}, Page: 2, Pages: 3, PerPage: 20, Total: 43}, result.Value)
	for _, code := range []int{429, 503} {
		status.Store(int32(code))
		_, err = sdkgo.RunQuery(queryContext{}, client.ListContactIDs(), testConnection, input)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAPIKey)
	}
	status.Store(409)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListContactIDs(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, ListContactIDsBranchProviderRejected, result.Branch)
}

func TestGetContactClassifiesMissingAndOmitsCustomFields(t *testing.T) {
	var missing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/contact/"+testContactID, r.URL.Path)
		if missing.Load() {
			w.WriteHeader(404)
			return
		}
		_, err := w.Write([]byte(`{"resources":[{"id":"5049fb849b85f669e40000dc","fields":{"first name":[{"value":"Ada"}],"last name":[{"value":"Lovelace"}],"email":[{"value":"ada@example.com"}]},"private":"not-persisted"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/v1")
	input := GetContactInput{ContactID: testContactID}
	result, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetContactBranchFound, result.Branch)
	require.Equal(t, ContactMetadata{ID: testContactID, FirstName: "Ada", LastName: "Lovelace", EmailAddress: "ada@example.com"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	missing.Store(true)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetContactBranchNotFound, result.Branch)
}

func TestGetContactRejectsInvalidInputWithoutProviderCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/v1")
	result, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ContactID: "invalid"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchDefect, result.Branch)
	require.Equal(t, int32(0), calls.Load())
}
