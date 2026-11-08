// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contacts

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

const testAccessToken = "synthetic-contacts-token"

var testConnection = sdkgo.ConnectionRef{Provider: "google-contacts", Name: "google-contacts"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "contacts-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	credentials := Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token")}
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestReadContactsProjectsOnlySelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		require.Equal(t, "names,emailAddresses", r.URL.Query().Get("personFields"))
		switch r.URL.Path {
		case "/v1/people/me/connections":
			require.Equal(t, "50", r.URL.Query().Get("pageSize"))
			require.Equal(t, "cursor", r.URL.Query().Get("pageToken"))
			_, err := w.Write([]byte(`{"connections":[{"resourceName":"people/c123","names":[{"displayName":"Example Contact"}],"emailAddresses":[{"value":"example@example.com"}],"phoneNumbers":[{"value":"secret-phone"}]}],"nextPageToken":"next"}`))
			require.NoError(t, err)
		case "/v1/people/c123":
			_, err := w.Write([]byte(`{"resourceName":"people/c123","names":[{"displayName":"Example Contact"}],"emailAddresses":[{"value":"example@example.com"}],"phoneNumbers":[{"value":"secret-phone"}]}`))
			require.NoError(t, err)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	page, err := sdkgo.RunQuery(queryContext{}, client.ListContacts(), testConnection, ListContactsInput{PageSize: 50, PageToken: "cursor"})
	require.NoError(t, err)
	require.Equal(t, ListContactsBranchListed, page.Branch)
	require.Equal(t, "next", page.Value.NextPageToken)
	require.Equal(t, Contact{ResourceName: "people/c123", DisplayName: "Example Contact", EmailAddress: "example@example.com"}, page.Value.Contacts[0])
	contact, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ResourceName: "people/c123"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchFound, contact.Branch)
	encoded, err := json.Marshal(contact.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret-phone")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestReadContactsRejectsInvalidInputAndRetriesTransientResponses(t *testing.T) {
	var status atomic.Int32
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ResourceName: "people/../other"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchDefect, result.Branch)
	page, err := sdkgo.RunQuery(queryContext{}, client.ListContacts(), testConnection, ListContactsInput{PageSize: 1001})
	require.NoError(t, err)
	require.Equal(t, ListContactsBranchDefect, page.Branch)
	require.Zero(t, calls.Load())
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunQuery(queryContext{}, client.ListContacts(), testConnection, ListContactsInput{})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
	status.Store(http.StatusNotFound)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ResourceName: "people/c123"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchNotFound, result.Branch)
}
