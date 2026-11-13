// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package suxin

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

const testAppID = "app_test_123"
const testAPIKey = "sk_test_123"

var testConnection = sdkgo.ConnectionRef{Provider: "suxin", Name: "suxin"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "suxin-provider-test" }
func (queryContext) StepExecutionID() string     { return "organization-list" }
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
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: sdkgo.NewSecretString(testAppID), APIKey: sdkgo.NewSecretString(testAPIKey)}}
	client, err := New(Config{BaseURL: "https://pc.suxin365.com", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}
func TestListOrganizationsRequestAndPrivacy(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		switch request.URL.Path {
		case authPath:
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, "application/json", request.Header.Get("Content-Type"))
			var body map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			require.Equal(t, map[string]string{"appId": testAppID, "apiKey": testAPIKey}, body)
			_, err := writer.Write([]byte(`{"success":true,"data":{"accessToken":"ephemeral-token","userId":"123456","orgs":[]}}`))
			require.NoError(t, err)
		case organizationsPath:
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, "123456", request.URL.Query().Get("userId"))
			require.Equal(t, "Bearer ephemeral-token", request.Header.Get("Authorization"))
			_, err := writer.Write([]byte(`{"success":true,"data":[{"orgId":"1001","name":"Example Organization","entityType":"COMPANY","certifyStatus":"VERIFIED","unifiedCode":"private-code","legalName":"Private Person"}]}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListOrganizations(), testConnection, ListOrganizationsInput{})
	require.NoError(t, err)
	require.Equal(t, ListOrganizationsBranchListed, result.Branch)
	require.Equal(t, 2, calls)
	require.Equal(t, []OrganizationSummary{{ID: "1001", Name: "Example Organization", EntityType: "COMPANY", CertifyStatus: "VERIFIED"}}, result.Value.Organizations)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	for _, secret := range []string{"ephemeral-token", "private-code", "Private Person", testAPIKey} {
		require.NotContains(t, string(encoded), secret)
	}
}
func TestListOrganizationsFailures(t *testing.T) {
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
		{"bad key", 403, "private", 0, "", ListOrganizationsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"auth rate limit", 429, "private", 0, "", "", sdkgo.FailureRateLimit, true},
		{"auth business rejection", 200, `{"success":false,"code":"4010","msg":"private"}`, 0, "", ListOrganizationsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"auth internal error", 200, `{"success":false,"code":"5000"}`, 0, "", "", sdkgo.FailureAvailability, true},
		{"auth malformed", 200, `{"success":true,"data":{}}`, 0, "", ListOrganizationsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"list forbidden", 200, `{"success":true,"data":{"accessToken":"token","userId":"123"}}`, 403, "private", ListOrganizationsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"list unavailable", 200, `{"success":true,"data":{"accessToken":"token","userId":"123"}}`, 503, "private", "", sdkgo.FailureAvailability, true},
		{"list business rejection", 200, `{"success":true,"data":{"accessToken":"token","userId":"123"}}`, 200, `{"success":false,"code":"4003","msg":"private"}`, ListOrganizationsBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"list malformed", 200, `{"success":true,"data":{"accessToken":"token","userId":"123"}}`, 200, `{"success":true}`, ListOrganizationsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"list missing id", 200, `{"success":true,"data":{"accessToken":"token","userId":"123"}}`, 200, `{"success":true,"data":[{"name":"Example","entityType":"COMPANY","certifyStatus":"VERIFIED"}]}`, ListOrganizationsBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListOrganizations(), testConnection, ListOrganizationsInput{})
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
func TestListOrganizationsTransportAndURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"success":true,"data":{"accessToken":"token","userId":"123"}}`))
		require.NoError(t, err)
	}))
	client := newTestClient(t, server, 0)
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, client.ListOrganizations(), testConnection, ListOrganizationsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: sdkgo.NewSecretString(testAppID), APIKey: sdkgo.NewSecretString(testAPIKey)}}
	_, err = New(Config{BaseURL: "http://pc.suxin365.com"}, credentials)
	require.Error(t, err)
	_, err = New(Config{BaseURL: "https://other.example"}, credentials)
	require.Error(t, err)
}
