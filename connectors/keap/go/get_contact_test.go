// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package keap

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "keap", Name: "keap"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "keap-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-contact" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credential := Credentials{PersonalAccessToken: sdkgo.NewSecretString("sample-keap-personal-access-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credential})
	require.NoError(t, err)
	return client
}
func TestGetContactReadsSelectedProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/rest/v2/contacts/123", request.URL.Path)
		require.Equal(t, "id,given_name,family_name", request.URL.Query().Get("fields"))
		require.Equal(t, "Bearer sample-keap-personal-access-token", request.Header.Get("Authorization"))
		_, err := io.WriteString(writer, `{"id":"123","given_name":"Example","family_name":"Contact"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetContact(), testConnection, GetContactInput{ContactID: "123"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchFound, result.Branch)
	require.Equal(t, ContactProfile{ID: "123", GivenName: "Example", FamilyName: "Contact"}, result.Value)
}
func TestGetContactClassifiesProviderOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "not found", status: 404, branch: GetContactBranchNotFound},
		{name: "forbidden", status: 403, branch: GetContactBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"id":"other"}`, branch: GetContactBranchInvalidResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetContact(), testConnection, GetContactInput{ContactID: "123"})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestGetContactRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.infusionsoft.com/crm", 0)
	for _, input := range []GetContactInput{{}, {ContactID: "bad/id"}, {ContactID: " bad"}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, GetContactBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"id":"123","given_name":"Example"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).GetContact(), testConnection, GetContactInput{ContactID: "123"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchInvalidResponse, result.Branch)
}
