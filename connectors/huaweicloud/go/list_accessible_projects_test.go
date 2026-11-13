// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package huaweicloud

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

var testConnection = sdkgo.ConnectionRef{Provider: "huaweicloud", Name: "huaweicloud"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "huaweicloud-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-projects" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func iamReply(status int, token, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"X-Subject-Token": []string{token}}, Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper) *Client {
	t.Helper()
	credentials := Credentials{AccountName: sdkgo.NewSecretString("example-account"), IAMUsername: sdkgo.NewSecretString("example-user"), IAMPassword: sdkgo.NewSecretString("test-password")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestTokenAndProjectRequest(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "iam.myhuaweicloud.com", request.URL.Host)
		require.Equal(t, "application/json;charset=utf8", request.Header.Get("Content-Type"))
		if calls == 1 {
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, "/v3/auth/tokens", request.URL.Path)
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.Contains(t, string(body), `"scope":{"domain":{"name":"example-account"}}`)
			require.Contains(t, string(body), `"password":"test-password"`)
			return iamReply(201, "test-token", `{"token":{}}`), nil
		}
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v3/auth/projects", request.URL.Path)
		require.Equal(t, "test-token", request.Header.Get("X-Auth-Token"))
		return iamReply(200, "", `{"projects":[{"id":"project-1","name":"cn-north-4","enabled":true,"domain_id":"domain-1"}]}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport).ListAccessibleProjects(), testConnection, ListAccessibleProjectsInput{})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, ListAccessibleProjectsBranchFound, result.Branch)
	require.Equal(t, "project-1", result.Value.Projects[0].ID)
}
func TestIAMFailuresAreSecretSafe(t *testing.T) {
	scenarios := []struct {
		name          string
		first, second int
		token, body   string
		wantBranch    sdkgo.BranchID
	}{
		{"auth rejected", 401, 0, "", `{"error":{"message":"test-password"}}`, ListAccessibleProjectsBranchProviderRejected},
		{"token missing", 201, 0, "", `{}`, ListAccessibleProjectsBranchInvalidResponse},
		{"projects rejected", 201, 403, "test-token", `{"error":{"message":"test-token"}}`, ListAccessibleProjectsBranchProviderRejected},
		{"projects malformed", 201, 200, "test-token", `{"projects":[{"id":"x"}]}`, ListAccessibleProjectsBranchInvalidResponse},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return iamReply(scenario.first, scenario.token, scenario.body), nil
				}
				return iamReply(scenario.second, "", scenario.body), nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport).ListAccessibleProjects(), testConnection, ListAccessibleProjectsInput{})
			require.NoError(t, err)
			require.Equal(t, scenario.wantBranch, result.Branch)
			require.NotContains(t, result.Failure.Message, "test-password")
			require.NotContains(t, result.Failure.Message, "test-token")
		})
	}
}

func TestIAMRetriesAndRedirectSafety(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		status int
		token  string
		count  int
	}{
		{"token rate limited", 429, "", 1},
		{"projects unavailable", 503, "test-token", 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 && scenario.count == 2 {
					return iamReply(201, scenario.token, `{}`), nil
				}
				return iamReply(scenario.status, "", `{}`), nil
			})
			_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport).ListAccessibleProjects(), testConnection, ListAccessibleProjectsInput{})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, scenario.count, calls)
		})
	}
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := iamReply(http.StatusFound, "", "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport).ListAccessibleProjects(), testConnection, ListAccessibleProjectsInput{})
	require.NoError(t, err)
	require.Equal(t, ListAccessibleProjectsBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}

func TestIAMEmptyProjectsAndResponseLimit(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			return iamReply(201, "test-token", `{}`), nil
		}
		return iamReply(200, "", `{"projects":[]}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport).ListAccessibleProjects(), testConnection, ListAccessibleProjectsInput{})
	require.NoError(t, err)
	require.Equal(t, ListAccessibleProjectsBranchFound, result.Branch)
	require.Empty(t, result.Value.Projects)
	credentials := Credentials{AccountName: sdkgo.NewSecretString("example-account"), IAMUsername: sdkgo.NewSecretString("example-user"), IAMPassword: sdkgo.NewSecretString("test-password")}
	client, err := New(Config{MaxResponseBytes: 16}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return iamReply(201, "test-token", strings.Repeat("x", 100)), nil
	})}))
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListAccessibleProjects(), testConnection, ListAccessibleProjectsInput{})
	require.NoError(t, err)
	require.Equal(t, ListAccessibleProjectsBranchInvalidResponse, result.Branch)
}

func TestIAMTransportFailureDoesNotLeakCredentials(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-password test-token") })
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport).ListAccessibleProjects(), testConnection, ListAccessibleProjectsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-password")
	require.NotContains(t, retry.Error(), "test-token")
}
