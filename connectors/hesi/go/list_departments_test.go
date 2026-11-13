// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hesi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAppKey = "test-app-key"
const testAppSecurity = "test-app-security"

var testConnection = sdkgo.ConnectionRef{Provider: "hesi", Name: "hesi"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "hesi-provider-test" }
func (queryContext) StepExecutionID() string     { return "department-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type redirectTransport struct{ serverURL *url.URL }

func (transport redirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.URL.Scheme = transport.serverURL.Scheme
	cloned.URL.Host = transport.serverURL.Host
	return http.DefaultTransport.RoundTrip(cloned)
}
func newTestClient(t *testing.T, server *httptest.Server, maxResponseBytes int64) *Client {
	t.Helper()
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString(testAppKey), AppSecurity: sdkgo.NewSecretString(testAppSecurity)}}
	client, err := New(Config{BaseURL: "https://app.ekuaibao.com", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}
func TestListDepartmentsAuthorizationRequestAndPrivacy(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		switch request.URL.Path {
		case authPath:
			require.Equal(t, http.MethodPost, request.Method)
			var body map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			require.Equal(t, map[string]string{"appKey": testAppKey, "appSecurity": testAppSecurity}, body)
			_, err := writer.Write([]byte(`{"value":{"accessToken":"short-lived-token","corporationId":"corp"}}`))
			require.NoError(t, err)
		case departmentsPath:
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, "short-lived-token", request.URL.Query().Get("accessToken"))
			require.Equal(t, "2", request.URL.Query().Get("start"))
			require.Equal(t, "10", request.URL.Query().Get("count"))
			_, err := writer.Write([]byte(`{"count":4,"items":[{"id":"corp:dept","name":"Example Department","code":"001","active":true,"form":{"costCenter":"secret"},"managerEmail":"private@example.com"}]}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListDepartments(), testConnection, ListDepartmentsInput{Start: 2, Count: 10})
	require.NoError(t, err)
	require.Equal(t, ListDepartmentsBranchListed, result.Branch)
	require.Equal(t, 2, calls)
	require.Equal(t, ListDepartmentsOutput{Departments: []DepartmentSummary{{ID: "corp:dept", Name: "Example Department", Code: "001", Active: true}}, Total: 4, NextStart: 3}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "short-lived-token")
	require.NotContains(t, string(encoded), "private@example.com")
	require.NotContains(t, string(encoded), "secret")
}
func TestListDepartmentsFailures(t *testing.T) {
	cases := []struct {
		name       string
		authStatus int
		authBody   string
		dataStatus int
		dataBody   string
		branch     sdkgo.BranchID
		kind       sdkgo.FailureKind
		retry      bool
	}{
		{"bad key", 403, "private", 0, "", ListDepartmentsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"auth rate limit", 429, "private", 0, "", "", sdkgo.FailureRateLimit, true},
		{"auth malformed", 200, "{}", 0, "", ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"department forbidden", 200, `{"value":{"accessToken":"token"}}`, 403, "private", ListDepartmentsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"department unavailable", 200, `{"value":{"accessToken":"token"}}`, 503, "private", "", sdkgo.FailureAvailability, true},
		{"department malformed", 200, `{"value":{"accessToken":"token"}}`, 200, "{}", ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"department missing active", 200, `{"value":{"accessToken":"token"}}`, 200, `{"count":1,"items":[{"id":"dept","name":"name"}]}`, ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Retry-After", "2")
				if request.URL.Path == authPath {
					writer.WriteHeader(tc.authStatus)
					_, err := writer.Write([]byte(tc.authBody))
					require.NoError(t, err)
				} else {
					writer.WriteHeader(tc.dataStatus)
					_, err := writer.Write([]byte(tc.dataBody))
					require.NoError(t, err)
				}
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListDepartments(), testConnection, ListDepartmentsInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testAppSecurity)
			}
		})
	}
}
func TestListDepartmentsValidationAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"value":{"accessToken":"token"}}`))
		require.NoError(t, err)
	}))
	for _, input := range []ListDepartmentsInput{{Start: -1}, {Count: 101}, {Start: 1000001}} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListDepartments(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListDepartmentsBranchDefect, result.Branch)
	}
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListDepartments(), testConnection, ListDepartmentsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
func TestListDepartmentsRejectsInsecureBaseURL(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString(testAppKey), AppSecurity: sdkgo.NewSecretString(testAppSecurity)}}
	_, err := New(Config{BaseURL: "http://app.ekuaibao.com"}, credentials)
	require.Error(t, err)
	_, err = New(Config{BaseURL: "https://other.example"}, credentials)
	require.Error(t, err)
}
