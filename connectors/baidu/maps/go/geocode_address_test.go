// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package baidumaps

import (
	"context"
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

const testAccessKey = "test-baidu-server-ak-123"

var testConnection = sdkgo.ConnectionRef{Provider: "baidu-maps", Name: "baidu-maps"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "baidu-maps-provider-test" }
func (queryContext) StepExecutionID() string     { return "geocode" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {AccessKey: sdkgo.NewSecretString(testAccessKey)},
	})
	require.NoError(t, err)
	return client
}

func TestGeocodeAddressRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/geocoding/v3/", request.URL.Path)
		require.Equal(t, testAccessKey, request.URL.Query().Get("ak"))
		require.Equal(t, "北京市海淀区上地十街10号", request.URL.Query().Get("address"))
		require.Equal(t, "北京市", request.URL.Query().Get("city"))
		require.Equal(t, "json", request.URL.Query().Get("output"))
		_, err := writer.Write([]byte(`{"status":0,"result":{"location":{"lng":116.30783584945,"lat":40.056583335558},"precise":1,"confidence":80,"comprehension":100,"level":"门址"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GeocodeAddress(), testConnection,
		GeocodeAddressInput{Address: "北京市海淀区上地十街10号", City: "北京市"})
	require.NoError(t, err)
	require.Equal(t, GeocodeAddressBranchGeocoded, result.Branch)
	require.Equal(t, 116.30783584945, result.Value.Longitude)
	require.Equal(t, 80, result.Value.Confidence)
	require.True(t, result.Value.Precise)
	require.EqualValues(t, 1, calls.Load())
}

func TestGeocodeAddressOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		isRetry bool
	}{
		{"invalid AK", 200, `{"status":5}`, GeocodeAddressBranchProviderRejected, false},
		{"quota exhausted", 200, `{"status":4}`, GeocodeAddressBranchProviderRejected, false},
		{"permission denied", 200, `{"status":240}`, GeocodeAddressBranchProviderRejected, false},
		{"provider server error", 200, `{"status":1}`, "", true},
		{"http rate limit", 429, `private provider data`, "", true},
		{"server unavailable", 503, `private provider data`, "", true},
		{"malformed response", 200, `{`, GeocodeAddressBranchInvalidResponse, false},
		{"missing result", 200, `{"status":0}`, GeocodeAddressBranchInvalidResponse, false},
		{"invalid latitude", 200, `{"status":0,"result":{"location":{"lng":116,"lat":91},"precise":1,"confidence":80,"comprehension":100}}`, GeocodeAddressBranchInvalidResponse, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(test.status)
				_, err := writer.Write([]byte(test.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GeocodeAddress(), testConnection,
				GeocodeAddressInput{Address: "北京市海淀区上地十街10号"})
			if test.isRetry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAccessKey)
				require.NotContains(t, err.Error(), "private provider data")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
		})
	}
}

func TestInvalidInputSendsNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GeocodeAddress(), testConnection,
		GeocodeAddressInput{Address: strings.Repeat("x", 129)})
	require.NoError(t, err)
	require.Equal(t, GeocodeAddressBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
