// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qiniu

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testAccessKey = "test-access-key"
const testSecretKey = "test-secret-key"

var testNow = time.Date(2026, 11, 14, 8, 30, 5, 0, time.FixedZone("CST", 8*60*60))

var testConnection = sdkgo.ConnectionRef{Provider: "qiniu", Name: "qiniu"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "qiniu-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-buckets" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessKey: sdkgo.NewSecretString(testAccessKey), SecretKey: sdkgo.NewSecretString(testSecretKey)}}, WithClock(func() time.Time { return testNow }))
	require.NoError(t, err)
	return client
}

func TestListBucketsRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/buckets", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "20261114T003005Z", request.Header.Get("X-Qiniu-Date"))
		mac := hmac.New(sha1.New, []byte(testSecretKey))
		_, err := mac.Write([]byte("GET /buckets\nHost: " + request.Host + "\nX-Qiniu-Date: 20261114T003005Z\n\n"))
		require.NoError(t, err)
		require.Equal(t, "Qiniu "+testAccessKey+":"+base64.URLEncoding.EncodeToString(mac.Sum(nil)), request.Header.Get("Authorization"))
		_, err = writer.Write([]byte(`["demo-images","archive-data"]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListBuckets(), testConnection, ListBucketsInput{})
	require.NoError(t, err)
	require.Equal(t, ListBucketsBranchListed, result.Branch)
	require.Equal(t, []string{"demo-images", "archive-data"}, result.Value.Buckets)
}

func TestListBucketsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `[]`, ListBucketsBranchListed, false},
		{"unauthorized", 401, `private provider data`, ListBucketsBranchProviderRejected, false},
		{"rate limited", 429, `private provider data`, "", true},
		{"unavailable", 503, `private provider data`, "", true},
		{"invalid JSON", 200, `{`, ListBucketsBranchInvalidResponse, false},
		{"invalid shape", 200, `{"buckets":[]}`, ListBucketsBranchInvalidResponse, false},
		{"null", 200, `null`, ListBucketsBranchInvalidResponse, false},
		{"oversized", 200, `[` + strings.Repeat(`"long-bucket",`, 30000) + `"last"]`, ListBucketsBranchInvalidResponse, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListBuckets(), testConnection, ListBucketsInput{})
			if test.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testSecretKey)
				require.NotContains(t, err.Error(), "private provider data")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
		})
	}
}

func TestInvalidCredentialsSendNoRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("invalid credentials must not send a request")
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {AccessKey: sdkgo.NewSecretString("bad:key"), SecretKey: sdkgo.NewSecretString(testSecretKey)},
	})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListBuckets(), testConnection, ListBucketsInput{})
	require.NoError(t, err)
	require.Equal(t, ListBucketsBranchDefect, result.Branch)
	require.NotContains(t, result.Failure.Message, "bad:key")
}
