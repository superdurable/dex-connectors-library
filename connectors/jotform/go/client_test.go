// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jotform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "test-secret-jotform-key"

var testConnection = sdkgo.ConnectionRef{Provider: "jotform", Name: "jotform"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "jotform-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	return client
}

func TestListFormsHeaderPaginationAndRateLimit(t *testing.T) {
	var rateLimited atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/user/forms", r.URL.Path)
		require.Equal(t, "10", r.URL.Query().Get("offset"))
		require.Equal(t, "2", r.URL.Query().Get("limit"))
		require.Empty(t, r.URL.Query().Get("apiKey"))
		require.Equal(t, testAPIKey, r.Header.Get("APIKEY"))
		if rateLimited.Load() {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, err := w.Write([]byte(`{"responseCode":200,"content":[{"id":"241234567890123","title":"Contact request","status":"ENABLED"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListForms(), testConnection, ListFormsInput{Offset: 10, Limit: 2})
	require.NoError(t, err)
	require.Equal(t, ListFormsBranchListed, result.Branch)
	require.Equal(t, "Contact request", result.Value.Forms[0].Title)
	require.Equal(t, 10, result.Value.Offset)
	rateLimited.Store(true)
	_, err = sdkgo.RunQuery(queryContext{}, client.ListForms(), testConnection, ListFormsInput{Offset: 10, Limit: 2})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAPIKey)
}

func TestGetSubmissionExcludesAnswersAndClassifiesMissing(t *testing.T) {
	var missing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/submission/241234567890123456", r.URL.Path)
		require.Equal(t, testAPIKey, r.Header.Get("APIKEY"))
		if missing.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, err := w.Write([]byte(`{"responseCode":200,"content":{"id":"241234567890123456","form_id":"241234567890123","status":"ACTIVE","created_at":"2026-11-07 10:00:00","answers":{"1":{"answer":"private-answer"}}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetSubmission(), testConnection, GetSubmissionInput{SubmissionID: "241234567890123456"})
	require.NoError(t, err)
	require.Equal(t, GetSubmissionBranchFound, result.Branch)
	require.Equal(t, "241234567890123", result.Value.FormID)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-answer")
	missing.Store(true)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetSubmission(), testConnection, GetSubmissionInput{SubmissionID: "241234567890123456"})
	require.NoError(t, err)
	require.Equal(t, GetSubmissionBranchNotFound, result.Branch)
}

func TestListFormsRejectsMalformedAndInvalidInputWithoutProviderCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, err := w.Write([]byte(`{"responseCode":200,"content":{}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListForms(), testConnection, ListFormsInput{Offset: -1})
	require.NoError(t, err)
	require.Equal(t, ListFormsBranchDefect, result.Branch)
	require.EqualValues(t, 0, calls.Load())
	result, err = sdkgo.RunQuery(queryContext{}, client.ListForms(), testConnection, ListFormsInput{})
	require.NoError(t, err)
	require.Equal(t, ListFormsBranchInvalidResponse, result.Branch)
	require.EqualValues(t, 1, calls.Load())
}
