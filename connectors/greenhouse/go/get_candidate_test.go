// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package greenhouse

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "greenhouse", Name: "greenhouse"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "greenhouse-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-candidate" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{ClientID: "sample-client-id", ClientSecret: sdkgo.NewSecretString("sample-client-secret"), AccessToken: sdkgo.NewSecretString("sample-access-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetCandidateReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v3/candidates", request.URL.Path)
		require.Equal(t, "53883394", request.URL.Query().Get("ids"))
		require.Equal(t, "1", request.URL.Query().Get("per_page"))
		require.Equal(t, "id,first_name,last_name", request.URL.Query().Get("fields"))
		require.Equal(t, "Bearer sample-access-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`[{"id":53883394,"first_name":"Sample","last_name":"Candidate","email_addresses":[{"value":"private@example.com"}]}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/v3", 0).GetCandidate(), testConnection, GetCandidateInput{CandidateID: 53883394})
	require.NoError(t, err)
	require.Equal(t, GetCandidateBranchFound, result.Branch)
	require.Equal(t, CandidateProfile{ID: 53883394, FirstName: "Sample", LastName: "Candidate"}, result.Value)
}

func TestGetCandidateClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 200, body: `[]`, branch: GetCandidateBranchNotFound},
		{name: "forbidden", status: 403, branch: GetCandidateBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "wrong candidate", status: 200, body: `[{"id":1,"first_name":"Wrong","last_name":"Candidate"}]`, branch: GetCandidateBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/v3", 0).GetCandidate(), testConnection, GetCandidateInput{CandidateID: 53883394})
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

func TestGetCandidateValidatesInputAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://harvest.greenhouse.io/v3", 0)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetCandidate(), testConnection, GetCandidateInput{})
	require.NoError(t, err)
	require.Equal(t, GetCandidateBranchDefect, result.Branch)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`[{"id":53883394,"first_name":"Sample","last_name":"Candidate"}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/v3", 20).GetCandidate(), testConnection, GetCandidateInput{CandidateID: 53883394})
	require.NoError(t, err)
	require.Equal(t, GetCandidateBranchInvalidResponse, result.Branch)
}

type refreshingTestCredentials struct {
	before       Credentials
	after        Credentials
	resolveError error
}

func (source *refreshingTestCredentials) Resolve(sdkgo.Call) (Credentials, error) {
	return source.before, nil
}
func (source *refreshingTestCredentials) ResolveWithRefresh(_ context.Context, _ sdkgo.Call, _ sdkgo.CredentialRefreshDriver[Credentials]) (Credentials, error) {
	return source.before, source.resolveError
}
func (source *refreshingTestCredentials) ResolveAfterRejection(_ context.Context, _ sdkgo.Call, _ sdkgo.CredentialRefreshDriver[Credentials]) (Credentials, error) {
	return source.after, nil
}

func TestGetCandidateRenewsRejectedAccessTokenOnce(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if requests == 1 {
			require.Equal(t, "Bearer expired-token", request.Header.Get("Authorization"))
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		require.Equal(t, "Bearer fresh-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`[{"id":53883394,"first_name":"Sample","last_name":"Candidate"}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	source := &refreshingTestCredentials{
		before: Credentials{ClientID: "client-id", ClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString("expired-token")},
		after:  Credentials{ClientID: "client-id", ClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString("fresh-token")},
	}
	client, err := New(Config{BaseURL: server.URL + "/v3"}, source)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetCandidate(), testConnection, GetCandidateInput{CandidateID: 53883394})
	require.NoError(t, err)
	require.Equal(t, GetCandidateBranchFound, result.Branch)
	require.Equal(t, 2, requests)
}

func TestGetCandidateRetriesTransientCredentialRefreshFailure(t *testing.T) {
	source := &refreshingTestCredentials{resolveError: errors.New("token endpoint unavailable")}
	client, err := New(Config{}, source)
	require.NoError(t, err)
	_, err = sdkgo.RunQuery(queryContext{}, client.GetCandidate(), testConnection, GetCandidateInput{CandidateID: 53883394})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
