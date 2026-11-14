// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package worktile

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

var testConnection = sdkgo.ConnectionRef{Provider: "worktile", Name: "worktile"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "worktile-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-projects" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{ClientID: sdkgo.NewSecretString("client-id-secret"), ClientSecret: sdkgo.NewSecretString("client-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestListProjectsRequestsTokenAndOnePage(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, tenantTokenEndpoint, request.URL.String())
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"client_id":"client-id-secret","client_secret":"client-secret"}`, string(body))
			return reply(200, `{"tenant_access_token":"short-lived-token","expires_in":7200}`), nil
		}
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, projectsEndpoint, request.URL.Scheme+"://"+request.URL.Host+request.URL.Path)
		require.Equal(t, "short-lived-token", request.URL.Query().Get("access_token"))
		require.Equal(t, "0", request.URL.Query().Get("page_index"))
		require.Equal(t, "10", request.URL.Query().Get("page_size"))
		return reply(200, `{"page_index":0,"page_size":10,"has_more":true,"items":[{"_id":"project-1","name":"Example Project","identifier":"ABC","visibility":1,"members":["private"]}]}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListProjects(), testConnection, ListProjectsInput{PageIndex: 0, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, ListProjectsBranchFound, result.Branch)
	require.Equal(t, ListProjectsOutput{PageIndex: 0, PageSize: 10, HasMore: true, Projects: []Project{{ID: "project-1", Name: "Example Project", Identifier: "ABC", Visibility: 1}}}, result.Value)
}
func TestListProjectsClassifiesTokenAndPageResponsesWithoutSecrets(t *testing.T) {
	scenarios := []struct {
		name          string
		tokenStatus   int
		tokenBody     string
		projectStatus int
		projectBody   string
		branch        sdkgo.BranchID
		retry         bool
		calls         int
	}{
		{"empty", 200, `{"tenant_access_token":"short-lived-token","expires_in":7200}`, 200, `{"page_index":0,"page_size":10,"has_more":false,"items":[]}`, ListProjectsBranchFound, false, 2},
		{"bad client", 401, `{"message":"client-secret"}`, 0, "", ListProjectsBranchProviderRejected, false, 1},
		{"token rate limit", 429, `{}`, 0, "", "", true, 1},
		{"bad token shape", 200, `{"tenant_access_token":"short-lived-token"}`, 0, "", ListProjectsBranchInvalidResponse, false, 1},
		{"project unauthorized", 200, `{"tenant_access_token":"short-lived-token","expires_in":7200}`, 401, `{"message":"short-lived-token"}`, ListProjectsBranchProviderRejected, false, 2},
		{"project malformed", 200, `{"tenant_access_token":"short-lived-token","expires_in":7200}`, 200, `{"items":[]}`, ListProjectsBranchInvalidResponse, false, 2},
		{"project rate limit", 200, `{"tenant_access_token":"short-lived-token","expires_in":7200}`, 429, `{}`, "", true, 2},
		{"project unavailable", 200, `{"tenant_access_token":"short-lived-token","expires_in":7200}`, 503, `{}`, "", true, 2},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return reply(scenario.tokenStatus, scenario.tokenBody), nil
				}
				return reply(scenario.projectStatus, scenario.projectBody), nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListProjects(), testConnection, ListProjectsInput{PageIndex: 0, PageSize: 10})
			require.Equal(t, scenario.calls, calls)
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "client-secret")
				require.NotContains(t, retry.Error(), "short-lived-token")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "client-secret")
				require.NotContains(t, result.Failure.Message, "short-lived-token")
			}
		})
	}
}
func TestListProjectsBoundsResponsesAndInvalidInput(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, strings.Repeat("x", 100)), nil })
	client := newTestClient(t, transport, 16)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListProjects(), testConnection, ListProjectsInput{PageIndex: 0, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, ListProjectsBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListProjects(), testConnection, ListProjectsInput{PageIndex: -1, PageSize: 101})
	require.NoError(t, err)
	require.Equal(t, ListProjectsBranchDefect, result.Branch)
	require.Equal(t, 1, calls)
}
func TestListProjectsTransportAndRedirectAreSecretSafe(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("client-secret short-lived-token") })
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListProjects(), testConnection, ListProjectsInput{PageIndex: 0, PageSize: 10})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "client-secret")
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return reply(200, `{"tenant_access_token":"short-lived-token","expires_in":7200}`), nil
		}
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListProjects(), testConnection, ListProjectsInput{PageIndex: 0, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, ListProjectsBranchProviderRejected, result.Branch)
	require.Equal(t, 2, calls)
}

func TestListProjectsTokenRedirectIsNotFollowed(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListProjects(), testConnection, ListProjectsInput{PageIndex: 0, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, ListProjectsBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}

func TestListProjectsProjectTransportErrorOmitsTokenURL(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return reply(200, `{"tenant_access_token":"short-lived-token","expires_in":7200}`), nil
		}
		return nil, errors.New(request.URL.String())
	})
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListProjects(), testConnection, ListProjectsInput{PageIndex: 0, PageSize: 10})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "short-lived-token")
	require.NotContains(t, retry.Error(), "access_token")
	require.Equal(t, 2, calls)
}
