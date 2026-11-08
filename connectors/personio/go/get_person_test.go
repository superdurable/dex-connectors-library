// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personio

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

var testConnection = sdkgo.ConnectionRef{Provider: "personio", Name: "personio"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "personio-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-person" }
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
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetPersonReadsSelectedFields(t *testing.T) {
	client := testClient(t, func(request *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "api.personio.de", request.URL.Host)
		require.Equal(t, "/v2/persons/3003", request.URL.Path)
		require.Equal(t, "Bearer sample-access-token", request.Header.Get("Authorization"))
		return testResponse(200, `{"id":"3003","first_name":"Jane","last_name":"Doe","email":"private@example.com"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, client.GetPerson(), testConnection, GetPersonInput{PersonID: "3003"})
	require.NoError(t, err)
	require.Equal(t, GetPersonBranchFound, result.Branch)
	require.Equal(t, PersonProfile{ID: "3003", FirstName: "Jane", LastName: "Doe"}, result.Value)
}
func TestGetPersonClassifiesProviderResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"missing", 404, `{}`, GetPersonBranchNotFound, false},
		{"denied", 403, `{}`, GetPersonBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"invalid JSON", 200, `{`, GetPersonBranchInvalidResponse, false},
		{"wrong person", 200, `{"id":"other"}`, GetPersonBranchInvalidResponse, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := testClient(t, func(*http.Request) (*http.Response, error) { return testResponse(testCase.status, testCase.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, client.GetPerson(), testConnection, GetPersonInput{PersonID: "3003"})
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
func TestGetPersonRejectsInvalidIDAndRetriesTransport(t *testing.T) {
	client := testClient(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("network failed") })
	result, err := sdkgo.RunQuery(queryContext{}, client.GetPerson(), testConnection, GetPersonInput{PersonID: "../other"})
	require.NoError(t, err)
	require.Equal(t, GetPersonBranchDefect, result.Branch)
	_, err = sdkgo.RunQuery(queryContext{}, client.GetPerson(), testConnection, GetPersonInput{PersonID: "3003"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
