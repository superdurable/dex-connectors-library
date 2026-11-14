// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package upyun

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "upyun", Name: "upyun"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "upyun-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-service-usage" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credential := Credentials{Operator: "upyun", Password: sdkgo.NewSecretString("password")}
	client, err := New(Config{BaseURL: endpoint, Bucket: "bucket", MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credential})
	require.NoError(t, err)
	return client
}

func TestUnifiedAuthorizationMatchesDocumentedAlgorithm(t *testing.T) {
	require.Equal(t, "UPYUN upyun:pEP305Ow6XXraN7SchOSRqhUmBE=", makeAuthorization("upyun", "password", "GET", "/bucket/?usage", "Wed, 29 Oct 2014 02:26:58 GMT"))
}
func TestGetServiceUsageSignsExactRequestURIAndReadsBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/bucket/?usage", request.URL.RequestURI())
		require.Empty(t, request.Header.Get("Content-MD5"))
		date := request.Header.Get("Date")
		_, err := time.Parse(http.TimeFormat, date)
		require.NoError(t, err)
		require.Equal(t, makeAuthorization("upyun", "password", "GET", "/bucket/?usage", date), request.Header.Get("Authorization"))
		require.Empty(t, request.Header.Get("Content-Length"))
		_, err = io.WriteString(writer, "2048")
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetServiceUsage(), testConnection, GetServiceUsageInput{})
	require.NoError(t, err)
	require.Equal(t, GetServiceUsageBranchRead, result.Branch)
	require.Equal(t, ServiceUsage{Bytes: 2048}, result.Value)
}
func TestGetServiceUsageClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"zero", 200, "0", GetServiceUsageBranchRead, false},
		{"forbidden", 403, "denied", GetServiceUsageBranchProviderRejected, false},
		{"missing service", 404, "missing", GetServiceUsageBranchProviderRejected, false},
		{"rate limited", 429, "rate", "", true},
		{"server unavailable", 503, "unavailable", "", true},
		{"empty", 200, "", GetServiceUsageBranchInvalidResponse, false},
		{"negative", 200, "-1", GetServiceUsageBranchInvalidResponse, false},
		{"decimal", 200, "10.5", GetServiceUsageBranchInvalidResponse, false},
		{"overflow", 200, "999999999999999999999999", GetServiceUsageBranchInvalidResponse, false},
		{"json", 200, `{"bytes":10}`, GetServiceUsageBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetServiceUsage(), testConnection, GetServiceUsageInput{})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestGetServiceUsageBoundsResponseAndRetriesTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, "12345")
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 3).GetServiceUsage(), testConnection, GetServiceUsageInput{})
	require.NoError(t, err)
	require.Equal(t, GetServiceUsageBranchInvalidResponse, result.Branch)
	endpoint := server.URL
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, endpoint, 0).GetServiceUsage(), testConnection, GetServiceUsageInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
func TestGetServiceUsageRejectsUnsafeConfigurationAndCredentials(t *testing.T) {
	for _, bucket := range []string{"", "a", "UPPER", "bad/name", "bad?query", "bad..name"} {
		_, err := New(Config{Bucket: bucket}, sdkgo.StaticCredentialProvider[Credentials]{})
		require.Error(t, err, bucket)
	}
	for _, operator := range []string{"", "bad:name", "bad\nname", " leading"} {
		require.False(t, validOperator(operator))
	}
	require.True(t, validOperator("reader_1"))
	client, err := New(Config{Bucket: "bucket"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetServiceUsage(), testConnection, GetServiceUsageInput{})
	require.NoError(t, err)
	require.Equal(t, GetServiceUsageBranchDefect, result.Branch)
	require.False(t, strings.Contains(result.Failure.Message, "password"))
}
