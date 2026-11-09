// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package constantcontact

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const sampleContactID = "1898ae62-4752-11e9-9c8a-fa163e6b01c1"

var testConnection = sdkgo.ConnectionRef{Provider: "constantcontact", Name: "constantcontact"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "constantcontact-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-contact" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func testCredentials() Credentials {
	return Credentials{OAuthClientID: "app-id", OAuthClientSecret: sdkgo.NewSecretString("app-secret"), AccessToken: sdkgo.NewSecretString("sample-access-token"), RefreshToken: sdkgo.NewSecretString("sample-refresh-token")}
}

func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: testCredentials()})
	require.NoError(t, err)
	return client
}

func TestGetContactReadsSelectedFieldsWithoutOtherProfileData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/contacts/"+sampleContactID, request.URL.Path)
		require.Equal(t, "Bearer sample-access-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"contact_id":"1898ae62-4752-11e9-9c8a-fa163e6b01c1","email_address":{"address":"reader@example.com","permission_to_send":"explicit"},"first_name":"Avery","last_name":"Reader","notes":"private profile note"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetContact(), testConnection, GetContactInput{ContactID: sampleContactID})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchFound, result.Branch)
	require.Equal(t, ContactMetadata{ContactID: sampleContactID, EmailAddress: "reader@example.com", FirstName: "Avery", LastName: "Reader", EmailPermission: "explicit"}, result.Value)
}

func TestGetContactClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		status      int
		body        string
		branch      sdkgo.BranchID
		shouldRetry bool
	}{
		{name: "not found", status: 404, branch: GetContactBranchNotFound},
		{name: "forbidden", status: 403, branch: GetContactBranchProviderRejected},
		{name: "rate limited", status: 429, shouldRetry: true},
		{name: "unavailable", status: 503, shouldRetry: true},
		{name: "malformed", status: 200, body: `{"contact_id":1}`, branch: GetContactBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetContact(), testConnection, GetContactInput{ContactID: sampleContactID})
			if testCase.shouldRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestGetContactRejectsInvalidIDBeforeSendingRequest(t *testing.T) {
	client := newTestClient(t, "https://api.cc.email/v3")
	result, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ContactID: "../other"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchDefect, result.Branch)
}

func TestGetContactAcceptsContactWithoutEmailChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"contact_id":"1898ae62-4752-11e9-9c8a-fa163e6b01c1","first_name":"Avery"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetContact(), testConnection, GetContactInput{ContactID: sampleContactID})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchFound, result.Branch)
	require.Equal(t, "", result.Value.EmailAddress)
}

func TestGetContactBoundsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"contact_id":"1898ae62-4752-11e9-9c8a-fa163e6b01c1"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 20}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: testCredentials()})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ContactID: sampleContactID})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchInvalidResponse, result.Branch)
}
