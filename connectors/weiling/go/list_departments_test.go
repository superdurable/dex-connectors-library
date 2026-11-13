// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package weiling

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

const testAppID = "app-test"
const testAppSecret = "secret-test"

var testConnection = sdkgo.ConnectionRef{Provider: "weiling", Name: "weiling"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "weiling-provider-test" }
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
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: sdkgo.NewSecretString(testAppID), AppSecret: sdkgo.NewSecretString(testAppSecret)}}
	client, err := New(Config{BaseURL: "https://openapi.weiling.cn", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}
func TestListDepartmentsTokenCacheAndPrivacy(t *testing.T) {
	tokenCalls := 0
	listCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case authPath:
			tokenCalls++
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, testAppID, request.URL.Query().Get("app_id"))
			require.Equal(t, testAppSecret, request.URL.Query().Get("app_secret"))
			_, err := writer.Write([]byte(`{"code":0,"data":{"access_token":"temporary-token","expires_in":7200}}`))
			require.NoError(t, err)
		case departmentsPath:
			listCalls++
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, "temporary-token", request.URL.Query().Get("access_token"))
			_, err := writer.Write([]byte(`{"code":0,"data":{"data_list":[{"dept_id":"dept-1","name":"Example Department","parent_id":"root","leader_userids":["private-user"]}]}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server, 0)
	for range 2 {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListDepartments(), testConnection, ListDepartmentsInput{})
		require.NoError(t, err)
		require.Equal(t, ListDepartmentsBranchListed, result.Branch)
		require.Equal(t, []DepartmentSummary{{ID: "dept-1", Name: "Example Department", ParentID: "root"}}, result.Value.Departments)
		encoded, err := json.Marshal(result.Value)
		require.NoError(t, err)
		for _, secret := range []string{"temporary-token", testAppSecret, "private-user"} {
			require.NotContains(t, string(encoded), secret)
		}
	}
	require.Equal(t, 1, tokenCalls)
	require.Equal(t, 2, listCalls)
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
		{"auth business rejection", 200, `{"code":403,"msg":"private"}`, 0, "", ListDepartmentsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"auth malformed", 200, `{"code":0,"data":{}}`, 0, "", ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"list forbidden", 200, `{"code":0,"data":{"access_token":"token","expires_in":7200}}`, 403, "private", ListDepartmentsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"list unavailable", 200, `{"code":0,"data":{"access_token":"token","expires_in":7200}}`, 503, "private", "", sdkgo.FailureAvailability, true},
		{"list rate limit", 200, `{"code":0,"data":{"access_token":"token","expires_in":7200}}`, 200, `{"code":1009}`, "", sdkgo.FailureRateLimit, true},
		{"list malformed", 200, `{"code":0,"data":{"access_token":"token","expires_in":7200}}`, 200, `{"code":0}`, ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"list bad item", 200, `{"code":0,"data":{"access_token":"token","expires_in":7200}}`, 200, `{"code":0,"data":{"data_list":[{"name":"Example"}]}}`, ListDepartmentsBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
				if tc.authStatus == 429 {
					var retry *dex.RetryAfterError
					require.ErrorAs(t, err, &retry)
					require.Equal(t, 2*time.Second, retry.After)
				} else {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, err, &retry)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, "private")
			}
		})
	}
}
func TestListDepartmentsTransportAndURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":0,"data":{"access_token":"token","expires_in":7200}}`))
		require.NoError(t, err)
	}))
	client := newTestClient(t, server, 0)
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, client.ListDepartments(), testConnection, ListDepartmentsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: sdkgo.NewSecretString(testAppID), AppSecret: sdkgo.NewSecretString(testAppSecret)}}
	_, err = New(Config{BaseURL: "http://openapi.weiling.cn"}, credentials)
	require.Error(t, err)
	_, err = New(Config{BaseURL: "https://other.example"}, credentials)
	require.Error(t, err)
}
