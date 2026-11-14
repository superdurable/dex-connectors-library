// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package amap

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

const testAPIKey = "test-amap-key-123"

var testConnection = sdkgo.ConnectionRef{Provider: "amap", Name: "amap"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "amap-provider-test" }
func (queryContext) StepExecutionID() string     { return "geocode" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	})
	require.NoError(t, err)
	return client
}

func TestGeocodeAddressRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v3/geocode/geo", request.URL.Path)
		require.Equal(t, testAPIKey, request.URL.Query().Get("key"))
		require.Equal(t, "北京市朝阳区阜通东大街6号", request.URL.Query().Get("address"))
		require.Equal(t, "北京", request.URL.Query().Get("city"))
		require.Equal(t, "JSON", request.URL.Query().Get("output"))
		_, err := writer.Write([]byte(`{"status":"1","info":"OK","infocode":"10000","count":"1","geocodes":[{"formatted_address":"北京市朝阳区阜通东大街6号","province":"北京市","city":[],"district":"朝阳区","adcode":"110105","location":"116.481488,39.990464","level":"门牌号"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GeocodeAddress(), testConnection,
		GeocodeAddressInput{Address: "北京市朝阳区阜通东大街6号", City: "北京"})
	require.NoError(t, err)
	require.Equal(t, GeocodeAddressBranchGeocoded, result.Branch)
	require.Len(t, result.Value.Locations, 1)
	require.Equal(t, 116.481488, result.Value.Locations[0].Longitude)
	require.Empty(t, result.Value.Locations[0].City)
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
		{"empty match", 200, `{"status":"1","infocode":"10000","count":"0","geocodes":[]}`, GeocodeAddressBranchGeocoded, false},
		{"invalid key", 200, `{"status":"0","infocode":"10001","info":"INVALID_USER_KEY"}`, GeocodeAddressBranchProviderRejected, false},
		{"invalid input", 200, `{"status":"0","infocode":"20000","info":"INVALID_PARAMS"}`, GeocodeAddressBranchProviderRejected, false},
		{"amap rate limit", 200, `{"status":"0","infocode":"10004","info":"ACCESS_TOO_FREQUENT"}`, "", true},
		{"http rate limit", 429, `private provider data`, "", true},
		{"server failure", 503, `private provider data`, "", true},
		{"malformed response", 200, `{`, GeocodeAddressBranchInvalidResponse, false},
		{"count mismatch", 200, `{"status":"1","infocode":"10000","count":"1","geocodes":[]}`, GeocodeAddressBranchInvalidResponse, false},
		{"invalid coordinates", 200, `{"status":"1","infocode":"10000","count":"1","geocodes":[{"location":"NaN,39"}]}`, GeocodeAddressBranchInvalidResponse, false},
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
				GeocodeAddressInput{Address: "北京市朝阳区阜通东大街6号"})
			if test.isRetry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAPIKey)
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
		GeocodeAddressInput{Address: strings.Repeat("x", 513)})
	require.NoError(t, err)
	require.Equal(t, GeocodeAddressBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
