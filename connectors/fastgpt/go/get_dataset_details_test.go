// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fastgpt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "private-test-key"
const testDatasetID = "65abc9bd9d1448617cba5e6c"

var testConnection = sdkgo.ConnectionRef{Provider: "fastgpt", Name: "fastgpt"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "fastgpt-provider-test" }
func (queryContext) StepExecutionID() string     { return "knowledge-base-detail" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	client, err := New(Config{BaseURL: baseURL, MaxResponseBytes: maxResponseBytes}, credentials)
	require.NoError(t, err)
	return client
}
func TestKnowledgeBaseDetailsRequestAndPrivacy(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/core/dataset/detail", request.URL.Path)
		require.Equal(t, testDatasetID, request.URL.Query().Get("id"))
		require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"code":200,"message":"","data":{"_id":"65abc9bd9d1448617cba5e6c","name":"Example Knowledge Base","type":"dataset","status":"active","intro":"private","vectorModel":{"model":"private"},"permission":{"isOwner":true}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetDatasetDetails(), testConnection, GetDatasetDetailsInput{ID: testDatasetID})
	require.NoError(t, err)
	require.Equal(t, GetDatasetDetailsBranchFound, result.Branch)
	require.Equal(t, GetDatasetDetailsOutput{ID: testDatasetID, Name: "Example Knowledge Base", Type: "dataset", Status: "active"}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestKnowledgeBaseDetailsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"unauthorized", 401, `{"message":"private"}`, GetDatasetDetailsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, `private`, GetDatasetDetailsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"not found", 404, `private`, GetDatasetDetailsBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"business rejected", 200, `{"code":403,"message":"private"}`, GetDatasetDetailsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"rate limited", 429, `private`, "", sdkgo.FailureRateLimit, true},
		{"business rate limited", 200, `{"code":429,"message":"private"}`, "", sdkgo.FailureRateLimit, true},
		{"bad gateway", 502, `private`, "", sdkgo.FailureAvailability, true},
		{"business server error", 200, `{"code":503,"message":"private"}`, "", sdkgo.FailureAvailability, true},
		{"malformed", 200, `{`, GetDatasetDetailsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing detail", 200, `{"code":200}`, GetDatasetDetailsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"mismatched ID", 200, `{"code":200,"data":{"_id":"wrong","name":"Other","type":"dataset","status":"active"}}`, GetDatasetDetailsBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetDatasetDetails(), testConnection, GetDatasetDetailsInput{ID: testDatasetID})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testAPIKey)
			}
			if err != nil {
				require.NotContains(t, err.Error(), testAPIKey)
			}
		})
	}
}
func TestKnowledgeBaseDetailsSizeTransportAndInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":200,"data":{"_id":"65abc9bd9d1448617cba5e6c","name":"Example","type":"dataset","status":"active"}}`))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 8).GetDatasetDetails(), testConnection, GetDatasetDetailsInput{ID: testDatasetID})
	require.NoError(t, err)
	require.Equal(t, GetDatasetDetailsBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetDatasetDetails(), testConnection, GetDatasetDetailsInput{ID: "/invalid"})
	require.NoError(t, err)
	require.Equal(t, GetDatasetDetailsBranchDefect, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetDatasetDetails(), testConnection, GetDatasetDetailsInput{ID: testDatasetID})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
