// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package west

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const testUsername = "zhangsan"
const testAPIPassword = "private-test-password"

var testConnection = sdkgo.ConnectionRef{Provider: "west", Name: "west"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "west-provider-test" }
func (queryContext) StepExecutionID() string     { return "domain-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {Username: testUsername, APIPassword: sdkgo.NewSecretString(testAPIPassword)}}
	client, err := New(Config{BaseURL: baseURL, MaxResponseBytes: maxResponseBytes}, credentials)
	require.NoError(t, err)
	return client
}
func TestPublishedSignatureVector(t *testing.T) {
	require.Equal(t, "f17581fb2535b2a7ee4468eb3f96a2a9", signWestToken("zhangsan", "5dh232kfg!*", "1554691950854"))
}
func TestDomainCatalogRequestAndPrivacy(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/v2/domain/", request.URL.Path)
		require.Equal(t, "getdomains", request.URL.Query().Get("act"))
		require.Equal(t, testUsername, request.URL.Query().Get("username"))
		require.Equal(t, "2", request.URL.Query().Get("page"))
		require.Equal(t, "10", request.URL.Query().Get("limit"))
		timestamp := request.URL.Query().Get("time")
		_, err := strconv.ParseInt(timestamp, 10, 64)
		require.NoError(t, err)
		require.Equal(t, signWestToken(testUsername, testAPIPassword, timestamp), request.URL.Query().Get("token"))
		require.Empty(t, request.Header.Get("Authorization"))
		body, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(`{"result":200,"clientid":"private","data":{"items":[{"domain":"例子.cn","expdate":"2030-01-01","dns1":"private"}],"pageno":2,"totalpages":3,"total":21}}`))
		require.NoError(t, err)
		_, err = writer.Write(body)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListDomains(), testConnection, ListDomainsInput{Page: 2, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, ListDomainsBranchListed, result.Branch)
	require.Equal(t, ListDomainsOutput{Domains: []Domain{{Name: "例子.cn", ExpiresAt: "2030-01-01"}}, Page: 2, TotalPages: 3, Total: 21}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestDomainCatalogOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"unauthorized", 401, `{"msg":"private"}`, ListDomainsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, `{"msg":"private"}`, ListDomainsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"provider rejection", 200, `{"result":403,"msg":"private"}`, ListDomainsBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"rate limited", 429, `private`, "", sdkgo.FailureRateLimit, true},
		{"bad gateway", 502, `private`, "", sdkgo.FailureAvailability, true},
		{"malformed", 200, `{`, ListDomainsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing data", 200, `{"result":200}`, ListDomainsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing domain", 200, `{"result":200,"data":{"items":[{"expdate":"2030-01-01"}]}}`, ListDomainsBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListDomains(), testConnection, ListDomainsInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testAPIPassword)
			}
			if err != nil {
				require.NotContains(t, err.Error(), testAPIPassword)
			}
		})
	}
}
func TestDomainCatalogSizeTransportAndInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"result":200,"data":{"items":[],"pageno":1,"totalpages":1,"total":0}}`))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 8).ListDomains(), testConnection, ListDomainsInput{})
	require.NoError(t, err)
	require.Equal(t, ListDomainsBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListDomains(), testConnection, ListDomainsInput{Page: -1})
	require.NoError(t, err)
	require.Equal(t, ListDomainsBranchDefect, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListDomains(), testConnection, ListDomainsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAPIPassword)
}
