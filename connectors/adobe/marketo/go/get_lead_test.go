// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "adobe", Name: "adobe-marketo"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "marketo-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-lead" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (transport roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}
func testClient(t *testing.T, transport roundTripFunc) *Client {
	t.Helper()
	credentials := Credentials{ClientID: "sample-client-id", ClientSecret: sdkgo.NewSecretString("sample-client-secret"), AccessToken: sdkgo.NewSecretString("sample-access-token")}
	client, err := New(Config{RESTEndpoint: "https://123-ABC-456.mktorest.com/rest", TokenEndpoint: "https://123-ABC-456.mktorest.com/identity/oauth/token"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetLeadReadsOnlySelectedProfileFields(t *testing.T) {
	client := testClient(t, func(request *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "123-ABC-456.mktorest.com", request.URL.Host)
		require.Equal(t, "/rest/v1/lead/318581.json", request.URL.Path)
		require.Equal(t, "id,firstName,lastName", request.URL.Query().Get("fields"))
		require.Equal(t, "Bearer sample-access-token", request.Header.Get("Authorization"))
		return response(200, `{"success":true,"result":[{"id":318581,"firstName":"Sample","lastName":"Lead","email":"secret@example.com"}]}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, client.GetLead(), testConnection, GetLeadInput{LeadID: 318581})
	require.NoError(t, err)
	require.Equal(t, GetLeadBranchFound, result.Branch)
	require.Equal(t, LeadProfile{ID: 318581, FirstName: "Sample", LastName: "Lead"}, result.Value)
}
func TestGetLeadClassifiesProviderResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"missing", 200, `{"success":true,"result":[]}`, GetLeadBranchNotFound, false},
		{"not found code", 200, `{"success":false,"errors":[{"code":"702"}]}`, GetLeadBranchNotFound, false},
		{"denied", 200, `{"success":false,"errors":[{"code":"603"}]}`, GetLeadBranchProviderRejected, false},
		{"rate limit code", 200, `{"success":false,"errors":[{"code":"606"}]}`, "", true},
		{"temporary code", 200, `{"success":false,"errors":[{"code":"608"}]}`, "", true},
		{"expired token code", 200, `{"success":false,"errors":[{"code":"602","message":"Access token expired"}]}`, "", true},
		{"invalid token code", 200, `{"success":false,"errors":[{"code":"601","message":"Access token invalid"}]}`, GetLeadBranchProviderRejected, false},
		{"HTTP rate limit", 429, `{}`, "", true},
		{"HTTP unavailable", 503, `{}`, "", true},
		{"invalid JSON", 200, `{`, GetLeadBranchInvalidResponse, false},
		{"wrong lead", 200, `{"success":true,"result":[{"id":123}]}`, GetLeadBranchInvalidResponse, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := testClient(t, func(*http.Request) (*http.Response, error) { return response(testCase.status, testCase.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, client.GetLead(), testConnection, GetLeadInput{LeadID: 318581})
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
func TestGetLeadRejectsInvalidInputAndTransportFailure(t *testing.T) {
	client := testClient(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("network failed") })
	result, err := sdkgo.RunQuery(queryContext{}, client.GetLead(), testConnection, GetLeadInput{})
	require.NoError(t, err)
	require.Equal(t, GetLeadBranchDefect, result.Branch)
	_, err = sdkgo.RunQuery(queryContext{}, client.GetLead(), testConnection, GetLeadInput{LeadID: 318581})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
func TestNewRejectsCrossSubscriptionTokenEndpoint(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {ClientID: "id", ClientSecret: sdkgo.NewSecretString("secret")}}
	_, err := New(Config{RESTEndpoint: "https://123-ABC-456.mktorest.com/rest", TokenEndpoint: "https://456-ABC-123.mktorest.com/identity/oauth/token"}, credentials)
	require.ErrorContains(t, err, "same subscription host")
}
