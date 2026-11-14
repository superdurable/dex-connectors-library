// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package volcengine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessKeyID = "AKLTtest-access-key"
const testSecretAccessKey = "test-secret-access-key"

var testConnection = sdkgo.ConnectionRef{Provider: "volcengine", Name: "volcengine"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "volcengine-provider-test" }
func (queryContext) StepExecutionID() string     { return "describe-regions" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessKeyID: sdkgo.NewSecretString(testAccessKeyID), SecretAccessKey: sdkgo.NewSecretString(testSecretAccessKey)}}, clockOption{now: func() time.Time { return time.Date(2025, 3, 29, 18, 9, 37, 0, time.UTC) }})
	require.NoError(t, err)
	return client
}

func TestPublishedVolcengineSignatureVector(t *testing.T) {
	// https://docs.volcengine.com/docs/api-signature-call-guide/signature-method publishes this GET example.
	query := canonicalVolcengineQuery(url.Values{"Action": {"QueryBalanceAcct"}, "Version": {"2022-01-01"}})
	require.Equal(t, "Action=QueryBalanceAcct&Version=2022-01-01", query)
	require.Equal(t, "NextToken=a%20b%2B~", canonicalVolcengineQuery(url.Values{"NextToken": {"a b+~"}}))
	// The documentation's example key is split so push protection does not mistake it for a live key.
	publishedAccessKeyID := "AKLT" + "YWViMTVmZGYzM2E0NDI5Mzk2MDZjNjFmMjc2MjRjMzg"
	authorization := signVolcengineRequest(http.MethodGet, "/", query, "billing.volcengineapi.com", "20250329T180937Z", "cn-beijing", "billing", publishedAccessKeyID, "WkRZeE1EQmxPVGhsWWpWak5HVmtNbUUxTXpZeU9UVXlOMlE1TmpZeVlqTQ==")
	require.Equal(t, "HMAC-SHA256 Credential="+publishedAccessKeyID+"/20250329/cn-beijing/billing/request, SignedHeaders=host;x-date, Signature=1eda9e7e6b1728151a8e8791fdaf67cfbd28bd5c80d0fce2eb208746cf483105", authorization)
}

func TestDescribeRegionsRequestAndPagination(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/", request.URL.Path)
		require.Equal(t, "DescribeRegions", request.URL.Query().Get("Action"))
		require.Equal(t, "2020-04-01", request.URL.Query().Get("Version"))
		require.Equal(t, "20", request.URL.Query().Get("MaxResults"))
		require.Equal(t, "next-page=", request.URL.Query().Get("NextToken"))
		require.Equal(t, "20250329T180937Z", request.Header.Get("X-Date"))
		require.Equal(t, "cn-beijing", request.Header.Get("Region"))
		require.Equal(t, "ecs", request.Header.Get("Service"))
		require.Equal(t, signVolcengineRequest(http.MethodGet, "/", request.URL.RawQuery, request.Host, "20250329T180937Z", "cn-beijing", "ecs", testAccessKeyID, testSecretAccessKey), request.Header.Get("Authorization"))
		require.NotContains(t, request.URL.String(), testSecretAccessKey)
		_, err := writer.Write([]byte(`{"ResponseMetadata":{"RequestId":"request-1","Action":"DescribeRegions","Version":"2020-04-01","Service":"ecs","Region":"cn-beijing"},"Result":{"NextToken":"another-page=","Regions":[{"RegionId":"cn-beijing"},{"RegionId":"cn-shanghai"}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).DescribeRegions(), testConnection, DescribeRegionsInput{MaxResults: 20, NextToken: "next-page="})
	require.NoError(t, err)
	require.Equal(t, DescribeRegionsBranchListed, result.Branch)
	require.Equal(t, []string{"cn-beijing", "cn-shanghai"}, result.Value.RegionIDs)
	require.Equal(t, "another-page=", result.Value.NextToken)
	require.EqualValues(t, 1, calls.Load())
}

func TestDescribeRegionsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty page", 200, `{"ResponseMetadata":{"RequestId":"r"},"Result":{"Regions":[]}}`, DescribeRegionsBranchListed, false},
		{"permission denied", 403, `{"ResponseMetadata":{"Error":{"Code":"PermissionDenied","Message":"private provider text"}}}`, DescribeRegionsBranchProviderRejected, false},
		{"provider rate code", 200, `{"ResponseMetadata":{"Error":{"Code":"FlowLimitExceeded","Message":"private provider text"}}}`, "", true},
		{"rate limited", 429, `private provider text`, "", true},
		{"unavailable", 503, `private provider text`, "", true},
		{"malformed", 200, `{`, DescribeRegionsBranchInvalidResponse, false},
		{"missing result", 200, `{"ResponseMetadata":{}}`, DescribeRegionsBranchInvalidResponse, false},
		{"missing region id", 200, `{"ResponseMetadata":{},"Result":{"Regions":[{}]}}`, DescribeRegionsBranchInvalidResponse, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).DescribeRegions(), testConnection, DescribeRegionsInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testSecretAccessKey)
				require.NotContains(t, err.Error(), "private provider text")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}

func TestInvalidPaginationSendsNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { calls.Add(1); writer.WriteHeader(http.StatusOK) }))
	defer server.Close()
	for _, input := range []DescribeRegionsInput{{MaxResults: -1}, {MaxResults: 21}, {NextToken: strings.Repeat("x", 2049)}, {NextToken: " leading"}} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).DescribeRegions(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, DescribeRegionsBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}

func TestOversizedResponseAndTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"ResponseMetadata":{"RequestId":"r"},"Result":{"Regions":[{"RegionId":"cn-beijing"}]}}`))
		require.NoError(t, err)
	}))
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessKeyID: sdkgo.NewSecretString(testAccessKeyID), SecretAccessKey: sdkgo.NewSecretString(testSecretAccessKey)}}
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 8}, credentials)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.DescribeRegions(), testConnection, DescribeRegionsInput{})
	require.NoError(t, err)
	require.Equal(t, DescribeRegionsBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).DescribeRegions(), testConnection, DescribeRegionsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.NotContains(t, err.Error(), testSecretAccessKey)
}
