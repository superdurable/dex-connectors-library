// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qichacha

import (
	"context"
	"crypto/md5"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAppKey = "test-qcc-app-key"
const testSecretKey = "test-qcc-secret-key"

var testConnection = sdkgo.ConnectionRef{Provider: "qichacha", Name: "qichacha"}

func TestQichachaSignatureFixedVector(t *testing.T) {
	require.Equal(t, "F82D2EC0DA9D602FB2F633CEBEBDE66E", signQichachaRequest("key", "1700000000", "secret"))
}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "qichacha-provider-test" }
func (queryContext) StepExecutionID() string     { return "enterprise-lookup" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString(testAppKey), SecretKey: sdkgo.NewSecretString(testSecretKey)}}
	client, err := New(Config{BaseURL: baseURL}, credentials)
	require.NoError(t, err)
	return client
}

func TestEnterpriseLookupSignsAndFiltersPersonalData(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/ECIV4/GetBasicDetailsByName", request.URL.Path)
		require.Equal(t, testAppKey, request.URL.Query().Get("key"))
		require.Equal(t, "示例科技有限公司", request.URL.Query().Get("keyword"))
		timestamp := request.Header.Get("Timespan")
		_, err := time.ParseDuration(timestamp + "s")
		require.NoError(t, err)
		expected := fmt.Sprintf("%X", md5.Sum([]byte(testAppKey+timestamp+testSecretKey)))
		require.Equal(t, expected, request.Header.Get("Token"))
		_, err = writer.Write([]byte(`{"Status":"200","Message":"查询成功","Result":{"Name":"示例科技有限公司","Status":"存续","CreditCode":"91110101000000000X","EntType":"0","OperName":"private person","Address":"private address"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetEnterpriseBasicDetails(), testConnection, GetEnterpriseBasicDetailsInput{Keyword: "示例科技有限公司"})
	require.NoError(t, err)
	require.Equal(t, GetEnterpriseBasicDetailsBranchFetched, result.Branch)
	require.Equal(t, GetEnterpriseBasicDetailsOutput{Name: "示例科技有限公司", CreditCode: "91110101000000000X", RegistrationStatus: "存续"}, result.Value)
	require.NotContains(t, fmt.Sprint(result.Value), "private")
	require.EqualValues(t, 1, calls.Load())
}

func TestEnterpriseLookupOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"provider rejection", 200, `{"Status":"201","Message":"private provider detail"}`, GetEnterpriseBasicDetailsBranchProviderRejected, false},
		{"bad HTTP", 403, `{"Status":"201"}`, GetEnterpriseBasicDetailsBranchProviderRejected, false},
		{"rate limit", 429, `private provider data`, "", true},
		{"unavailable", 503, `private provider data`, "", true},
		{"malformed", 200, `{`, GetEnterpriseBasicDetailsBranchInvalidResponse, false},
		{"missing status", 200, `{"Result":{}}`, GetEnterpriseBasicDetailsBranchInvalidResponse, false},
		{"missing fields", 200, `{"Status":"200","Result":{"Name":"X","EntType":"0"}}`, GetEnterpriseBasicDetailsBranchInvalidResponse, false},
		{"hong kong", 200, `{"Status":"200","Result":{"Name":"X","Status":"active","EntType":"2","CreditCode":"HK123"}}`, GetEnterpriseBasicDetailsBranchProviderRejected, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetEnterpriseBasicDetails(), testConnection, GetEnterpriseBasicDetailsInput{Keyword: "Example"})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
			if err != nil {
				require.NotContains(t, err.Error(), testSecretKey)
				require.NotContains(t, err.Error(), "private provider data")
			}
		})
	}
}

func TestInvalidKeywordSendsNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, keyword := range []string{"", " Example", strings.Repeat("x", 1001)} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetEnterpriseBasicDetails(), testConnection, GetEnterpriseBasicDetailsInput{Keyword: keyword})
		require.NoError(t, err)
		require.Equal(t, GetEnterpriseBasicDetailsBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}
