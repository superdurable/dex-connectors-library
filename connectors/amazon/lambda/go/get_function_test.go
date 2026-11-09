// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
package lambda

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "amazon-lambda", Name: "amazon-lambda"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "lambda-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-function" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AccessKeyID: sdkgo.NewSecretString("AKIALAMBDACONNECTOR"), SecretAccessKey: sdkgo.NewSecretString("lambda-test-secret"), SessionToken: sdkgo.NewSecretString("")}
	client, err := New(Config{Region: "eu-west-1", Endpoint: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, signingTimeOption{now: func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }})
	require.NoError(t, err)
	return client
}
func TestGetFunctionSignsAndReadsConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/2015-03-31/functions/example-function", r.URL.Path)
		require.Equal(t, "20261030T000000Z", r.Header.Get("X-Amz-Date"))
		require.True(t, strings.Contains(r.Header.Get("Authorization"), "/eu-west-1/lambda/aws4_request"))
		_, err := w.Write([]byte(`{"Configuration":{"FunctionName":"example-function","FunctionArn":"arn:aws:lambda:eu-west-1:123456789012:function:example-function"},"Code":{"Location":"discard"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetFunction(), testConnection, GetFunctionInput{FunctionName: "example-function"})
	require.NoError(t, err)
	require.Equal(t, GetFunctionBranchFound, result.Branch)
	require.Equal(t, FunctionDetails{Name: "example-function", ARN: "arn:aws:lambda:eu-west-1:123456789012:function:example-function"}, result.Value)
}
func TestGetFunctionClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"missing", 404, `{"message":"not found"}`, GetFunctionBranchMissing, false},
		{"forbidden", 403, `{"message":"denied"}`, GetFunctionBranchProviderRejected, false},
		{"rate limit", 429, `{"message":"busy"}`, "", true},
		{"unavailable", 503, `{"message":"unavailable"}`, "", true},
		{"bad response", 200, `{"Configuration":{"FunctionName":"wrong"}}`, GetFunctionBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetFunction(), testConnection, GetFunctionInput{FunctionName: "example-function"})
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
func TestGetFunctionRejectsInvalidNameAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://lambda.eu-west-1.amazonaws.com", 0)
	for _, name := range []string{"", "bad/name"} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetFunction(), testConnection, GetFunctionInput{FunctionName: name})
		require.NoError(t, err)
		require.Equal(t, GetFunctionBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(`{"Configuration":{"FunctionName":"example-function"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 10).GetFunction(), testConnection, GetFunctionInput{FunctionName: "example-function"})
	require.NoError(t, err)
	require.Equal(t, GetFunctionBranchInvalidResponse, result.Branch)
}
