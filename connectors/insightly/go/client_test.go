// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package insightly

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

const testAPIKey = "synthetic-insightly-key"

var testConnection = sdkgo.ConnectionRef{Provider: "insightly", Name: "insightly"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "insightly-provider-test" }
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

func TestListContactsBasicAuthPrivacyAndRetry(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3.1/Contacts", r.URL.Path)
		user, password, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, testAPIKey, user)
		require.Empty(t, password)
		require.Equal(t, "skip=25&top=20", r.URL.RawQuery)
		require.Equal(t, "Dex Connector (https://superdurable.io)", r.Header.Get("User-Agent"))
		if status.Load() != 0 {
			w.WriteHeader(int(status.Load()))
			return
		}
		_, err := w.Write([]byte(`[{"CONTACT_ID":45678,"FIRST_NAME":"Ada","LAST_NAME":"Lovelace","EMAIL_ADDRESS":"ada@example.com","CUSTOMFIELDS":[{"FIELD_VALUE":"not-persisted"}]}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v3.1")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListContacts(), testConnection, ListContactsInput{Top: 20, Skip: 25})
	require.NoError(t, err)
	require.Equal(t, ListContactsBranchListed, result.Branch)
	require.Equal(t, []ContactMetadata{{ID: 45678, FirstName: "Ada", LastName: "Lovelace", EmailAddress: "ada@example.com"}}, result.Value.Contacts)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	for _, code := range []int{429, 503} {
		status.Store(int32(code))
		_, err = sdkgo.RunQuery(queryContext{}, client.ListContacts(), testConnection, ListContactsInput{Top: 20, Skip: 25})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAPIKey)
	}
	status.Store(403)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListContacts(), testConnection, ListContactsInput{Top: 20, Skip: 25})
	require.NoError(t, err)
	require.Equal(t, ListContactsBranchProviderRejected, result.Branch)
}

func TestGetContactClassifiesMissingAndOmitsCustomFields(t *testing.T) {
	var missing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3.1/Contacts/45678", r.URL.Path)
		if missing.Load() {
			w.WriteHeader(404)
			return
		}
		_, err := w.Write([]byte(`{"CONTACT_ID":45678,"FIRST_NAME":"Ada","LAST_NAME":"Lovelace","EMAIL_ADDRESS":"ada@example.com","CUSTOMFIELDS":[{"FIELD_VALUE":"not-persisted"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v3.1")
	input := GetContactInput{ContactID: "45678"}
	result, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetContactBranchFound, result.Branch)
	require.Equal(t, int64(45678), result.Value.ID)
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
	client := newTestClient(t, server.URL+"/v3.1")
	result, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ContactID: "0"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchDefect, result.Branch)
	require.Equal(t, int32(0), calls.Load())
}
