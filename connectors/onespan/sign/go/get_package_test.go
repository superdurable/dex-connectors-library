// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onespansign

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "onespan-sign", Name: "onespan-sign"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "onespansign-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-package" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sample-onespan-sign-api-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetPackageReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/packages/9sKhW-h-qS9m6Ho3zRv3n2a-rkI=", request.URL.Path)
		require.Equal(t, "Basic sample-onespan-sign-api-key", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"id":"9sKhW-h-qS9m6Ho3zRv3n2a-rkI=","name":"Example agreement","status":"COMPLETED","recipients":[{"email":"private@example.com"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetPackage(), testConnection, GetPackageInput{PackageID: "9sKhW-h-qS9m6Ho3zRv3n2a-rkI="})
	require.NoError(t, err)
	require.Equal(t, GetPackageBranchFound, result.Branch)
	require.Equal(t, PackageStatus{ID: "9sKhW-h-qS9m6Ho3zRv3n2a-rkI=", Name: "Example agreement", Status: "COMPLETED"}, result.Value)
}

func TestGetPackageClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetPackageBranchNotFound},
		{name: "forbidden", status: 403, branch: GetPackageBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"id":"9sKhW-h-qS9m6Ho3zRv3n2a-rkI=","name":"Example agreement"}`, branch: GetPackageBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetPackage(), testConnection, GetPackageInput{PackageID: "9sKhW-h-qS9m6Ho3zRv3n2a-rkI="})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestGetPackageRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://sandbox.esignlive.com/api", 0)
	for _, packageID := range []string{"", "../other", "bad\npackage", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetPackage(), testConnection, GetPackageInput{PackageID: packageID})
		require.NoError(t, err)
		require.Equal(t, GetPackageBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"id":"9sKhW-h-qS9m6Ho3zRv3n2a-rkI=","name":"Example agreement","status":"COMPLETED"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 20).GetPackage(), testConnection, GetPackageInput{PackageID: "9sKhW-h-qS9m6Ho3zRv3n2a-rkI="})
	require.NoError(t, err)
	require.Equal(t, GetPackageBranchInvalidResponse, result.Branch)
}
