// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tingyun

import (
	"context"
	"encoding/json"
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

var testConnection = sdkgo.ConnectionRef{Provider: "tingyun", Name: "tingyun"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "tingyun-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-applications" }
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
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("access-secret")}
	client, err := New(Config{APIHost: "https://wukong2.tingyun.com/", MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestListApplicationsRequestAndSecretFiltered(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://wukong2.tingyun.com/app-api/app/list", request.URL.String())
		require.Equal(t, "token access-secret", request.Header.Get("Authorization"))
		return reply(200, `[{"id":7461,"name":"app_test","appToken":"private-app-token","accountId":186938}]`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListApplications(), testConnection, ListApplicationsInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListApplicationsBranchListed, result.Branch)
	require.Equal(t, ListApplicationsOutput{Applications: []ApplicationMetadata{{ID: 7461, Name: "app_test"}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-app-token")
	require.NotContains(t, string(encoded), "186938")
}
func TestListApplicationsClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `[]`, ListApplicationsBranchListed, false},
		{"unauthorized", 401, `{"error":"access-secret"}`, ListApplicationsBranchProviderRejected, false},
		{"forbidden", 403, `{}`, ListApplicationsBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"object", 200, `{"data":[]}`, ListApplicationsBranchInvalidResponse, false},
		{"missing id", 200, `[{"name":"app"}]`, ListApplicationsBranchInvalidResponse, false},
		{"missing name", 200, `[{"id":1}]`, ListApplicationsBranchInvalidResponse, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListApplications(), testConnection, ListApplicationsInput{})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "access-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "access-secret")
			}
		})
	}
}
func TestListApplicationsHostValidationAndResponseBound(t *testing.T) {
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("access-secret")}
	source := sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}
	for _, host := range []string{"", "http://wukong2.tingyun.com", "https://evil.example", "https://wukong2.tingyun.com:443", "https://wukong2.tingyun.com/path", "https://user:pass@wukong2.tingyun.com", "https://wukong2.tingyun.com?x=1"} {
		_, err := New(Config{APIHost: host}, source)
		require.Error(t, err, host)
		require.NotContains(t, err.Error(), "access-secret")
	}
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).ListApplications(), testConnection, ListApplicationsInput{})
	require.NoError(t, err)
	require.Equal(t, ListApplicationsBranchInvalidResponse, result.Branch)
}
func TestListApplicationsTransportAndRedirectAreSecretSafe(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("access-secret") })
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListApplications(), testConnection, ListApplicationsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "access-secret")
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListApplications(), testConnection, ListApplicationsInput{})
	require.NoError(t, err)
	require.Equal(t, ListApplicationsBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}
