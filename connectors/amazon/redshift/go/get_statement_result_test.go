// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
package redshift

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

var testConnection = sdkgo.ConnectionRef{Provider: "amazon-redshift", Name: "amazon-redshift"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "redshift-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-statement-result" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AccessKeyID: sdkgo.NewSecretString("AKIAREDSHIFTCONNECTOR"), SecretAccessKey: sdkgo.NewSecretString("redshift-test-secret"), SessionToken: sdkgo.NewSecretString("")}
	client, err := New(Config{Region: "eu-west-1", Endpoint: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, signingTimeOption{now: func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }})
	require.NoError(t, err)
	return client
}
func exampleInput() GetStatementResultInput {
	return GetStatementResultInput{StatementID: "d9b6c0c9-0747-4bf4-b142-e8883122f766", NextToken: "page-token"}
}
func TestGetStatementResultSignsAndReadsPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/", r.URL.Path)
		require.Equal(t, "application/x-amz-json-1.1", r.Header.Get("Content-Type"))
		require.Equal(t, "RedshiftData.GetStatementResult", r.Header.Get("X-Amz-Target"))
		require.Equal(t, "20261030T000000Z", r.Header.Get("X-Amz-Date"))
		require.True(t, strings.Contains(r.Header.Get("Authorization"), "/eu-west-1/redshift-data/aws4_request"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"Id":"d9b6c0c9-0747-4bf4-b142-e8883122f766","NextToken":"page-token"}`, string(body))
		_, err = w.Write([]byte(`{"Records":[[{"stringValue":"example"}]],"TotalNumRows":1,"NextToken":"more"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetStatementResult(), testConnection, exampleInput())
	require.NoError(t, err)
	require.Equal(t, GetStatementResultBranchFetched, result.Branch)
	require.Equal(t, int64(1), result.Value.TotalNumRows)
	require.Equal(t, "more", result.Value.NextToken)
	require.Len(t, result.Value.Records, 1)
}
func TestGetStatementResultClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"missing", 400, `{"__type":"ResourceNotFoundException"}`, GetStatementResultBranchMissing, false},
		{"forbidden", 403, `{"__type":"AccessDeniedException"}`, GetStatementResultBranchProviderRejected, false},
		{"throttled", 400, `{"__type":"ActiveStatementsExceededException"}`, "", true},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"bad response", 200, `{"Records":"invalid"}`, GetStatementResultBranchInvalidResponse, false},
		{"invalid row", 200, `{"Records":[{"stringValue":"bad"}],"TotalNumRows":1}`, GetStatementResultBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetStatementResult(), testConnection, exampleInput())
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
func TestGetStatementResultRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://redshift-data.eu-west-1.amazonaws.com", 0)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetStatementResult(), testConnection, GetStatementResultInput{StatementID: "invalid"})
	require.NoError(t, err)
	require.Equal(t, GetStatementResultBranchDefect, result.Branch)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(`{"Records":[[{"stringValue":"example"}]],"TotalNumRows":1}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 10).GetStatementResult(), testConnection, exampleInput())
	require.NoError(t, err)
	require.Equal(t, GetStatementResultBranchInvalidResponse, result.Branch)
}
