// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ucloud

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testPublicKey = "test-public-key"
const testPrivateKey = "test-private-key"

var testConnection = sdkgo.ConnectionRef{Provider: "ucloud", Name: "ucloud"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "ucloud-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-region" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PublicKey: sdkgo.NewSecretString(testPublicKey), PrivateKey: sdkgo.NewSecretString(testPrivateKey)}})
	require.NoError(t, err)
	return client
}
func TestGetRegionSignature(t *testing.T) {
	require.Equal(t, "dec400a82b05219740991537eb83756ed96bcd9d", getRegionSignature(testPublicKey, testPrivateKey))
	require.Equal(t, "4201919d267504385deb93af19e0197870fed36b", signUCloudParameters(map[string]string{
		"Action": "DescribeUHostInstance", "Region": "cn-bj2", "Limit": "10", "PublicKey": "someone@example.com1296235120854146120",
	}, "46f09bb9fab4f12dfc160dae12273d5332b5debe"))
}
func TestGetRegionRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var body map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, map[string]string{"Action": "GetRegion", "PublicKey": testPublicKey, "Signature": "dec400a82b05219740991537eb83756ed96bcd9d"}, body)
		require.NotContains(t, body, testPrivateKey)
		_, err := writer.Write([]byte(`{"Action":"GetRegionResponse","RetCode":0,"Regions":[{"RegionId":1,"RegionName":"Beijing","IsDefault":true,"BitMaps":"1","Region":"cn-bj","Zone":"cn-bj-01"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetRegion(), testConnection, GetRegionInput{})
	require.NoError(t, err)
	require.Equal(t, GetRegionBranchFound, result.Branch)
	require.Len(t, result.Value.Regions, 1)
	require.Equal(t, "cn-bj-01", result.Value.Regions[0].Zone)
}
func TestGetRegionOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"Action":"GetRegionResponse","RetCode":0}`, GetRegionBranchFound, false},
		{"auth error", 200, `{"Action":"GetRegionResponse","RetCode":171,"Message":"private provider data"}`, GetRegionBranchProviderRejected, false},
		{"rate code", 200, `{"Action":"GetRegionResponse","RetCode":153}`, "", true},
		{"gateway code", 200, `{"Action":"GetRegionResponse","RetCode":130}`, "", true},
		{"other code", 200, `{"Action":"GetRegionResponse","RetCode":160}`, GetRegionBranchProviderRejected, false},
		{"http rate", 429, `private provider data`, "", true},
		{"unavailable", 503, `private provider data`, "", true},
		{"invalid json", 200, `{`, GetRegionBranchInvalidResponse, false},
		{"missing retcode", 200, `{"Action":"GetRegionResponse"}`, GetRegionBranchInvalidResponse, false},
		{"invalid region", 200, `{"Action":"GetRegionResponse","RetCode":0,"Regions":[{"Region":"cn-bj"}]}`, GetRegionBranchInvalidResponse, false},
		{"oversized", 200, strings.Repeat("x", 300000), GetRegionBranchInvalidResponse, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetRegion(), testConnection, GetRegionInput{})
			if test.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testPrivateKey)
				require.NotContains(t, err.Error(), "private provider data")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "private provider data")
			}
		})
	}
}
func TestInvalidCredentialsSendNoRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid credentials must not send a request") }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PublicKey: sdkgo.NewSecretString("bad key"), PrivateKey: sdkgo.NewSecretString(testPrivateKey)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetRegion(), testConnection, GetRegionInput{})
	require.NoError(t, err)
	require.Equal(t, GetRegionBranchDefect, result.Branch)
}
