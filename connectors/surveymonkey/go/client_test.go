// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package surveymonkey

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

const testAccessToken = "test-secret-surveymonkey-token"

var testConnection = sdkgo.ConnectionRef{Provider: "surveymonkey", Name: "surveymonkey"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "surveymonkey-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString(testAccessToken)}})
	require.NoError(t, err)
	return client
}

func TestListSurveysBearerPaginationAndRetry(t *testing.T) {
	var rateLimited atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/surveys", r.URL.Path)
		require.Equal(t, "2", r.URL.Query().Get("page"))
		require.Equal(t, "3", r.URL.Query().Get("per_page"))
		require.Empty(t, r.URL.Query().Get("access_token"))
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		if rateLimited.Load() {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, err := w.Write([]byte(`{"data":[{"id":"123456789","title":"Customer feedback"}],"page":2,"per_page":3,"total":4}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListSurveys(), testConnection, ListSurveysInput{Page: 2, PerPage: 3})
	require.NoError(t, err)
	require.Equal(t, ListSurveysBranchListed, result.Branch)
	require.Equal(t, "Customer feedback", result.Value.Surveys[0].Title)
	require.Equal(t, 4, result.Value.Total)
	rateLimited.Store(true)
	_, err = sdkgo.RunQuery(queryContext{}, client.ListSurveys(), testConnection, ListSurveysInput{Page: 2, PerPage: 3})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAccessToken)
}

func TestGetResponseExcludesAnswersAndClassifiesMissing(t *testing.T) {
	var missing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/surveys/123456789/responses/987654321", r.URL.Path)
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		if missing.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, err := w.Write([]byte(`{"id":"987654321","survey_id":"123456789","response_status":"completed","date_created":"2026-11-07T10:00:00+00:00","date_modified":"2026-11-07T10:01:00+00:00","ip_address":"private-ip","pages":[{"questions":[{"answers":[{"text":"private-answer"}]}]}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetResponse(), testConnection, GetResponseInput{SurveyID: "123456789", ResponseID: "987654321"})
	require.NoError(t, err)
	require.Equal(t, GetResponseBranchFound, result.Branch)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-answer")
	require.NotContains(t, string(encoded), "private-ip")
	missing.Store(true)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetResponse(), testConnection, GetResponseInput{SurveyID: "123456789", ResponseID: "987654321"})
	require.NoError(t, err)
	require.Equal(t, GetResponseBranchNotFound, result.Branch)
}

func TestListSurveysMalformedAndInvalidInput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, err := w.Write([]byte(`{"data":{},"page":1,"per_page":50,"total":0}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListSurveys(), testConnection, ListSurveysInput{})
	require.NoError(t, err)
	require.Equal(t, ListSurveysBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListSurveys(), testConnection, ListSurveysInput{PerPage: 101})
	require.NoError(t, err)
	require.Equal(t, ListSurveysBranchDefect, result.Branch)
	require.Equal(t, int32(1), calls.Load())
}
