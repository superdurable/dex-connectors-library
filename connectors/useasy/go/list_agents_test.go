// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package useasy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAppID = "1011"
const testSecretKey = "private-test-key"

var testConnection = sdkgo.ConnectionRef{Provider: "useasy", Name: "useasy"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "useasy-provider-test" }
func (queryContext) StepExecutionID() string     { return "agent-list" }
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
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: sdkgo.NewSecretString(testAppID), SecretKey: sdkgo.NewSecretString(testSecretKey)}}
	client, err := New(Config{BaseURL: "https://openapi.useasy.cn", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}
func TestListAgentsRequestSignatureAndPrivacy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, agentsPath, request.URL.Path)
		require.Equal(t, "application/json;charset=utf-8", request.Header.Get("Content-Type"))
		require.Equal(t, testAppID, request.Header.Get("appid"))
		require.Regexp(t, regexp.MustCompile(`^[1-9][0-9]{5}$`), request.Header.Get("nonce"))
		require.NotEmpty(t, request.Header.Get("timestamp"))
		mac := hmac.New(sha256.New, []byte(testSecretKey))
		_, err := mac.Write([]byte(testAppID + request.Header.Get("timestamp") + request.Header.Get("nonce")))
		require.NoError(t, err)
		require.Equal(t, base64.StdEncoding.EncodeToString(mac.Sum(nil)), request.Header.Get("signature"))
		var input ListAgentsInput
		require.NoError(t, json.NewDecoder(request.Body).Decode(&input))
		require.Equal(t, ListAgentsInput{Page: 2, PageSize: 10}, input)
		_, err = writer.Write([]byte(`{"success":true,"code":"200","data":{"list":[{"_id":"663578842498871236","agentNumber":"8010","status":1,"password":"hidden","mobile":"12345678901","email":"private@example.com"}],"page":{"pageSize":10,"pageNumber":2,"totalPage":3,"total":21}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListAgents(), testConnection, ListAgentsInput{Page: 2, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, ListAgentsBranchListed, result.Branch)
	require.Equal(t, ListAgentsOutput{Agents: []AgentSummary{{ID: "663578842498871236", Number: "8010", Enabled: true}}, Page: 2, Total: 21, TotalPages: 3}, result.Value)
	bytes, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(bytes), "hidden")
	require.NotContains(t, string(bytes), "private@example.com")
}
func TestListAgentsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"unauthorized", 401, "private", ListAgentsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, "private", ListAgentsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"provider rejection", 200, `{"success":false,"code":"403","message":"secret"}`, ListAgentsBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"rate limit", 429, "private", "", sdkgo.FailureRateLimit, true},
		{"unavailable", 503, "private", "", sdkgo.FailureAvailability, true},
		{"malformed", 200, "{", ListAgentsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing list", 200, `{"success":true,"code":"200","data":{"page":{"pageSize":20,"pageNumber":1,"totalPage":0,"total":0}}}`, ListAgentsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing status", 200, `{"success":true,"code":"200","data":{"list":[{"_id":"1","agentNumber":"100"}],"page":{"pageSize":20,"pageNumber":1,"totalPage":1,"total":1}}}`, ListAgentsBranchInvalidResponse, sdkgo.FailureProtocol, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListAgents(), testConnection, ListAgentsInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testSecretKey)
			}
		})
	}
}
func TestListAgentsLimitsAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"success":true,"code":"200","data":{"list":[],"page":{"pageSize":20,"pageNumber":1,"totalPage":0,"total":0}}}`))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 8).ListAgents(), testConnection, ListAgentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListAgentsBranchInvalidResponse, result.Branch)
	for _, input := range []ListAgentsInput{{Page: -1}, {PageSize: 101}, {Page: 50001}} {
		result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListAgents(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListAgentsBranchDefect, result.Branch)
	}
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListAgents(), testConnection, ListAgentsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
func TestListAgentsRejectsInsecureBaseURL(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: sdkgo.NewSecretString(testAppID), SecretKey: sdkgo.NewSecretString(testSecretKey)}}
	_, err := New(Config{BaseURL: "http://openapi.useasy.cn"}, credentials)
	require.Error(t, err)
}
