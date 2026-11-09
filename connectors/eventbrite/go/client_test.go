// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventbrite

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

const testPrivateToken = "test-secret-eventbrite-token"

var testConnection = sdkgo.ConnectionRef{Provider: "eventbrite", Name: "eventbrite"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "eventbrite-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PrivateToken: sdkgo.NewSecretString(testPrivateToken)}})
	require.NoError(t, err)
	return client
}

func TestListOrganizationsUsesBearerAndPages(t *testing.T) {
	var rateLimited atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/users/me/organizations/", r.URL.Path)
		require.Equal(t, "2", r.URL.Query().Get("page"))
		require.Equal(t, "Bearer "+testPrivateToken, r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.String(), testPrivateToken)
		if rateLimited.Load() {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, err := w.Write([]byte(`{"organizations":[{"id":"12345","name":"Example organization"}],"pagination":{"page_number":2,"page_count":3}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v3")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListOrganizations(), testConnection, ListOrganizationsInput{Page: 2})
	require.NoError(t, err)
	require.Equal(t, ListOrganizationsBranchListed, result.Branch)
	require.Equal(t, "12345", result.Value.Organizations[0].ID)
	require.Equal(t, 3, result.Value.PageCount)
	rateLimited.Store(true)
	_, err = sdkgo.RunQuery(queryContext{}, client.ListOrganizations(), testConnection, ListOrganizationsInput{Page: 2})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testPrivateToken)
}

func TestListEventsAndGetAttendeeExcludePrivateFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer "+testPrivateToken, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/v3/organizations/12345/events/":
			require.Equal(t, "1", r.URL.Query().Get("page"))
			_, err := w.Write([]byte(`{"events":[{"id":"67890","name":{"text":"Conference","html":"secret-html"},"status":"live","description":"secret-description"}],"pagination":{"page_number":1,"page_count":1}}`))
			require.NoError(t, err)
		case "/v3/events/67890/attendees/13579/":
			_, err := w.Write([]byte(`{"id":"13579","event_id":"67890","status":"attending","created":"2026-11-07T10:00:00Z","profile":{"email":"secret@example.com"},"barcodes":[{"barcode":"secret-barcode"}]}`))
			require.NoError(t, err)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v3")
	events, err := sdkgo.RunQuery(queryContext{}, client.ListEvents(), testConnection, ListEventsInput{OrganizationID: "12345"})
	require.NoError(t, err)
	require.Equal(t, ListEventsBranchListed, events.Branch)
	require.Equal(t, "Conference", events.Value.Events[0].Name)
	attendee, err := sdkgo.RunQuery(queryContext{}, client.GetAttendee(), testConnection, GetAttendeeInput{EventID: "67890", AttendeeID: "13579"})
	require.NoError(t, err)
	require.Equal(t, GetAttendeeBranchFound, attendee.Branch)
	encoded, err := json.Marshal(attendee.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret")
}

func TestGetAttendeeClassifiesNotFoundAndInvalidInput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNotFound) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetAttendee(), testConnection, GetAttendeeInput{EventID: "../", AttendeeID: "13579"})
	require.NoError(t, err)
	require.Equal(t, GetAttendeeBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
	result, err = sdkgo.RunQuery(queryContext{}, client.GetAttendee(), testConnection, GetAttendeeInput{EventID: "67890", AttendeeID: "13579"})
	require.NoError(t, err)
	require.Equal(t, GetAttendeeBranchNotFound, result.Branch)
	require.EqualValues(t, 1, calls.Load())
}

func TestListEventsInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`{"events":{}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListEvents(), testConnection, ListEventsInput{OrganizationID: "12345"})
	require.NoError(t, err)
	require.Equal(t, ListEventsBranchInvalidResponse, result.Branch)
}
