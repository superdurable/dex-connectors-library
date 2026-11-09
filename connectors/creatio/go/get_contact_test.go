// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const sampleContactID = "90a68db5-cb3a-4f64-bb60-c0a934ee7600"

var testConnection = sdkgo.ConnectionRef{Provider: "creatio", Name: "creatio"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "creatio-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-contact" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (transport roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}
func testResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}
func testClient(t *testing.T, transport roundTripFunc) *Client {
	t.Helper()
	credentials := Credentials{ClientID: "sample-client-id", ClientSecret: sdkgo.NewSecretString("sample-client-secret"), AccessToken: sdkgo.NewSecretString("sample-access-token")}
	client, err := New(Config{BaseURL: "https://app.example.com", TokenEndpoint: "https://identity.example.com/connect/token"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetContactReadsSelectedFields(t *testing.T) {
	client := testClient(t, func(request *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "app.example.com", request.URL.Host)
		require.Equal(t, "/0/odata/Contact", request.URL.Path)
		require.Equal(t, "Id eq "+sampleContactID, request.URL.Query().Get("$filter"))
		require.Equal(t, "Id,Name,Email", request.URL.Query().Get("$select"))
		require.Equal(t, "1", request.URL.Query().Get("$top"))
		require.Equal(t, "Bearer sample-access-token", request.Header.Get("Authorization"))
		return testResponse(200, `{"value":[{"Id":"90a68db5-cb3a-4f64-bb60-c0a934ee7600","Name":"Ada Lovelace","Email":"ada@example.com","Notes":"not persisted"}]}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ContactID: sampleContactID})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchFound, result.Branch)
	require.Equal(t, ContactSummary{ID: sampleContactID, Name: "Ada Lovelace", Email: "ada@example.com"}, result.Value)
}
func TestGetContactClassifiesProviderResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"missing", 404, `{}`, GetContactBranchNotFound, false},
		{"empty collection", 200, `{"value":[]}`, GetContactBranchNotFound, false},
		{"denied", 403, `{}`, GetContactBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"invalid JSON", 200, `{`, GetContactBranchInvalidResponse, false},
		{"wrong contact", 200, `{"value":[{"Id":"7d61f73e-a18c-46a5-88e2-2bfa894657d7","Name":"Wrong"}]}`, GetContactBranchInvalidResponse, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := testClient(t, func(*http.Request) (*http.Response, error) { return testResponse(testCase.status, testCase.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ContactID: sampleContactID})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}
func TestGetContactRejectsInvalidIDAndRetriesTransport(t *testing.T) {
	client := testClient(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("network failed") })
	result, err := sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ContactID: "../other"})
	require.NoError(t, err)
	require.Equal(t, GetContactBranchDefect, result.Branch)
	_, err = sdkgo.RunQuery(queryContext{}, client.GetContact(), testConnection, GetContactInput{ContactID: sampleContactID})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
