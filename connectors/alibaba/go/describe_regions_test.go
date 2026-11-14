// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package alibaba

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testAccessKeyID = "test-access-key-id"
const testAccessKeySecret = "test-access-key-secret"

var testConnection = sdkgo.ConnectionRef{Provider: "alibaba", Name: "alibaba"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "alibaba-provider-test" }
func (queryContext) StepExecutionID() string     { return "describe-regions" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessKeyID: sdkgo.NewSecretString(testAccessKeyID), AccessKeySecret: sdkgo.NewSecretString(testAccessKeySecret)}})
	require.NoError(t, err)
	return client
}
func TestOfficialACS3SignatureVector(t *testing.T) {
	// Published RunInstances vector: https://help.aliyun.com/en/sdk/product-overview/v3-request-structure-and-signature
	signature := signACS3Request("YourAccessKeySecret", "POST", "ecs.cn-shanghai.aliyuncs.com", "RunInstances", "ImageId=win2019_1809_x64_dtc_zh-cn_40G_alibase_20230811.vhd&RegionId=cn-shanghai", "2023-10-26T10:22:32Z", "3156853299f313e23d1673dc12e1703d")
	require.Equal(t, "06563a9e1b43f5dfe96b81484da74bceab24a1d853912eee15083a6f0f3283c0", signature)
}
func TestSignedRequestAndRegionsResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "DescribeRegions", request.Header.Get("x-acs-action"))
		require.Equal(t, "2014-05-26", request.Header.Get("x-acs-version"))
		require.Equal(t, emptyPayloadSHA256, request.Header.Get("x-acs-content-sha256"))
		requestDate := request.Header.Get("x-acs-date")
		parsedDate, err := time.Parse("2006-01-02T15:04:05Z", requestDate)
		require.NoError(t, err)
		require.WithinDuration(t, time.Now(), parsedDate, 5*time.Second)
		nonce := request.Header.Get("x-acs-signature-nonce")
		require.Len(t, nonce, 32)
		signature := signACS3Request(testAccessKeySecret, "GET", request.Host, "DescribeRegions", "", requestDate, nonce)
		require.Equal(t, "ACS3-HMAC-SHA256 Credential="+testAccessKeyID+",SignedHeaders="+signedHeaderNames+",Signature="+signature, request.Header.Get("Authorization"))
		_, err = writer.Write([]byte(`{"RequestId":"safe-id","Regions":{"Region":[{"RegionId":"cn-hangzhou","LocalName":"华东1（杭州）","RegionEndpoint":"ecs.cn-hangzhou.aliyuncs.com","Status":"available"},{"RegionId":"cn-beijing","LocalName":"华北2（北京）","RegionEndpoint":"ecs.cn-beijing.aliyuncs.com","Status":"soldOut"}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).DescribeRegions(), testConnection, DescribeRegionsInput{})
	require.NoError(t, err)
	require.Equal(t, DescribeRegionsBranchFetched, result.Branch)
	require.Equal(t, []ECSRegion{{RegionID: "cn-hangzhou", LocalName: "华东1（杭州）", Endpoint: "ecs.cn-hangzhou.aliyuncs.com", Status: "available"}, {RegionID: "cn-beijing", LocalName: "华北2（北京）", Endpoint: "ecs.cn-beijing.aliyuncs.com", Status: "soldOut"}}, result.Value.Regions)
	require.EqualValues(t, 1, calls.Load())
}
func TestProviderOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"unauthorized", 403, `{"Code":"Forbidden","Message":"secret details"}`, DescribeRegionsBranchProviderRejected, false},
		{"provider rejection on HTTP 200", 200, `{"Code":"InvalidAccessKeyId.NotFound","Message":"secret details"}`, DescribeRegionsBranchProviderRejected, false},
		{"throttling code on HTTP 400", 400, `{"Code":"Throttling.System","Message":"private provider data"}`, "", true},
		{"rate limited", 429, `private provider data`, "", true}, {"unavailable", 503, `private provider data`, "", true},
		{"malformed", 200, `{`, DescribeRegionsBranchInvalidResponse, false}, {"missing list", 200, `{"Regions":{}}`, DescribeRegionsBranchInvalidResponse, false},
		{"invalid region", 200, `{"Regions":{"Region":[{"RegionId":"cn-hangzhou"}]}}`, DescribeRegionsBranchInvalidResponse, false},
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
				require.NotContains(t, err.Error(), testAccessKeySecret)
				require.NotContains(t, err.Error(), "private provider data")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestOversizedResponseAndBadCredential(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, err := writer.Write([]byte(`{"Regions":{"Region":[]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	smallClient, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 4}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessKeyID: sdkgo.NewSecretString(testAccessKeyID), AccessKeySecret: sdkgo.NewSecretString(testAccessKeySecret)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, smallClient.DescribeRegions(), testConnection, DescribeRegionsInput{})
	require.NoError(t, err)
	require.Equal(t, DescribeRegionsBranchInvalidResponse, result.Branch)
	badClient, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessKeyID: sdkgo.NewSecretString("bad key"), AccessKeySecret: sdkgo.NewSecretString(testAccessKeySecret)}})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, badClient.DescribeRegions(), testConnection, DescribeRegionsInput{})
	require.NoError(t, err)
	require.Equal(t, DescribeRegionsBranchDefect, result.Branch)
	require.EqualValues(t, 1, calls.Load())
	require.False(t, isValidCredential(strings.Repeat("x", 513)))
}
