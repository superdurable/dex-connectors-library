// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerio

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

var testConnection = sdkgo.ConnectionRef{Provider: "customerio", Name: "customerio"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "customerio-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-customers-by-email" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{PrivateKey: sdkgo.NewSecretString("sample-customerio-private-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetCustomersByEmailReadsMatchingProfiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/customers", request.URL.Path)
		require.Equal(t, "person@example.com", request.URL.Query().Get("email"))
		require.Equal(t, "Bearer sample-customerio-private-key", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"results":[{"cio_id":"a3000001","email":"person@example.com","id":2}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetCustomersByEmail(), testConnection, GetCustomersByEmailInput{Email: "person@example.com"})
	require.NoError(t, err)
	require.Equal(t, GetCustomersByEmailBranchFound, result.Branch)
	require.Equal(t, CustomerLookupPage{Email: "person@example.com", CustomerIDs: []string{"a3000001"}}, result.Value)
}

func TestGetCustomersByEmailClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "forbidden", status: 403, branch: GetCustomersByEmailBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"results":[{"cio_id":""}]}`, branch: GetCustomersByEmailBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetCustomersByEmail(), testConnection, GetCustomersByEmailInput{Email: "person@example.com"})
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

func TestGetCustomersByEmailRejectsInvalidEmailAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://customerio.com", 0)
	for _, email := range []string{"", " bad ", "bad\nname", "not-an-email", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetCustomersByEmail(), testConnection, GetCustomersByEmailInput{Email: email})
		require.NoError(t, err)
		require.Equal(t, GetCustomersByEmailBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"results":[]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 10).GetCustomersByEmail(), testConnection, GetCustomersByEmailInput{Email: "person@example.com"})
	require.NoError(t, err)
	require.Equal(t, GetCustomersByEmailBranchInvalidResponse, result.Branch)
}
