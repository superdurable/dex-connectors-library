// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package udesk

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const testAdminEmail = "admin@example.com"
const testAPIToken = "233df89e-b4a2-42e0-89af-f295b1078686"

var testConnection = sdkgo.ConnectionRef{Provider: "udesk", Name: "udesk"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "udesk-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-ticket-statuses" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AdminEmail: sdkgo.NewSecretString(testAdminEmail), APIToken: sdkgo.NewSecretString(testAPIToken)}})
	require.NoError(t, err)
	return client
}
func TestPublishedSignatureVector(t *testing.T) {
	require.Equal(t, "6892f1b794071c260e1b1eac15df588fc919c9e86eb742affaa742ad6c03cb52", udeskSignature("admin@udesk.cn", testAPIToken, "1494474404", "2d931510-d99f-494a-8c67-87feb05e1594"))
}
func TestStatusRequestAndResponse(t *testing.T) {
	var mu sync.Mutex
	nonces := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/open_api_v1/tickets/status", request.URL.Path)
		query := request.URL.Query()
		require.Equal(t, testAdminEmail, query.Get("email"))
		require.Equal(t, "v2", query.Get("sign_version"))
		require.NotContains(t, request.URL.String(), testAPIToken)
		timestamp := query.Get("timestamp")

		require.Regexp(t, `^[0-9]{10}$`, timestamp)
		unixSeconds, err := strconv.ParseInt(timestamp, 10, 64)
		require.NoError(t, err)
		require.InDelta(t, time.Now().Unix(), unixSeconds, 300)
		require.Regexp(t, `^[0-9a-f]{32}$`, query.Get("nonce"))
		require.Equal(t, udeskSignature(testAdminEmail, testAPIToken, timestamp, query.Get("nonce")), query.Get("sign"))
		mu.Lock()
		require.False(t, nonces[query.Get("nonce")])
		nonces[query.Get("nonce")] = true
		mu.Unlock()
		_, err = writer.Write([]byte(`{"code":1000,"message":"success","status":[{"status_id":1,"name":"开启","en_name":"open","enable":true,"color":"#f30000"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	for range 2 {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListTicketStatuses(), testConnection, ListTicketStatusesInput{})
		require.NoError(t, err)
		require.Equal(t, ListTicketStatusesBranchListed, result.Branch)
		require.Equal(t, "open", result.Value.Statuses[0].EnglishName)
	}
	require.Len(t, nonces, 2)
}

func TestEnabledFilterIncludesExplicitFalse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "false", request.URL.Query().Get("enable"))
		_, err := writer.Write([]byte(`{"code":1000,"status":[]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	isEnabled := false
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListTicketStatuses(), testConnection, ListTicketStatusesInput{EnabledOnly: &isEnabled})
	require.NoError(t, err)
	require.Equal(t, ListTicketStatusesBranchListed, result.Branch)
}
func TestStatusOutcomes(t *testing.T) {
	cases := []struct {
		name       string
		httpStatus int
		body       string
		branch     sdkgo.BranchID
		retry      bool
	}{{"auth", 200, `{"code":2059,"message":"private provider data"}`, ListTicketStatusesBranchProviderRejected, false}, {"provider rejection", 200, `{"code":2001,"message":"private provider data"}`, ListTicketStatusesBranchProviderRejected, false}, {"empty", 200, `{"code":1000,"status":[]}`, ListTicketStatusesBranchListed, false}, {"http rate", 429, `private provider data`, "", true}, {"server unavailable", 503, `private provider data`, "", true}, {"invalid JSON", 200, `{`, ListTicketStatusesBranchInvalidResponse, false}, {"missing status", 200, `{"code":1000}`, ListTicketStatusesBranchInvalidResponse, false}, {"invalid status", 200, `{"code":1000,"status":[{"status_id":1}]}`, ListTicketStatusesBranchInvalidResponse, false}, {"oversized", 200, strings.Repeat("x", 300000), ListTicketStatusesBranchInvalidResponse, false}, {"redirect", 302, `private provider data`, ListTicketStatusesBranchProviderRejected, false}}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				if test.httpStatus == 302 {
					writer.Header().Set("Location", "https://example.com/collect")
				}
				writer.WriteHeader(test.httpStatus)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListTicketStatuses(), testConnection, ListTicketStatusesInput{})
			if test.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAPIToken)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "private provider data")
				require.NotContains(t, result.Failure.Message, testAPIToken)
			}
		})
	}
}
func TestAccountOriginAndCredentials(t *testing.T) {
	for _, baseURL := range []string{"https://udesk.cn", "https://evil.example", "https://demo.udesk.cn/path", "https://foo.bar.udesk.cn", "http://demo.udesk.cn"} {
		_, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{})
		require.Error(t, err, baseURL)
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid credentials must not send a request") }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AdminEmail: sdkgo.NewSecretString("bad email"), APIToken: sdkgo.NewSecretString(testAPIToken)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListTicketStatuses(), testConnection, ListTicketStatusesInput{})
	require.NoError(t, err)
	require.Equal(t, ListTicketStatusesBranchDefect, result.Branch)
}
