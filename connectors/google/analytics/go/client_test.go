// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package analytics4

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

const testAccessToken = "synthetic-analytics-token"

var testConnection = sdkgo.ConnectionRef{Provider: "google-analytics-4", Name: "google-analytics-4"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "analytics-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	credentials := Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token")}
	client, err := New(Config{DataBaseURL: baseURL, AdminBaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestRunReportProjectsBoundedRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1beta/properties/123456789:runReport", r.URL.Path)
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		var body struct {
			DateRanges []map[string]string `json:"dateRanges"`
			Dimensions []map[string]string `json:"dimensions"`
			Metrics    []map[string]string `json:"metrics"`
			Limit      string              `json:"limit"`
			Offset     string              `json:"offset"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "2026-11-01", body.DateRanges[0]["startDate"])
		require.Equal(t, "country", body.Dimensions[0]["name"])
		require.Equal(t, "activeUsers", body.Metrics[0]["name"])
		require.Equal(t, "20", body.Limit)
		require.Equal(t, "10", body.Offset)
		_, err := w.Write([]byte(`{"rows":[{"dimensionValues":[{"value":"United States"}],"metricValues":[{"value":"4820"}]}],"rowCount":"101","private":"discard"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	input := RunReportInput{PropertyID: "123456789", StartDate: "2026-11-01", EndDate: "2026-11-07", Dimensions: []string{"country"}, Metrics: []string{"activeUsers"}, Limit: 20, Offset: 10}
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).RunReport(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, RunReportBranchReported, result.Branch)
	require.Equal(t, int64(101), result.Value.RowCount)
	require.Equal(t, []string{"United States"}, result.Value.Rows[0].Dimensions)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestGetPropertyAndListAudiences(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/v1beta/properties/123456789":
			_, err := w.Write([]byte(`{"name":"properties/123456789","displayName":"Example Analytics","timeZone":"America/Los_Angeles","currencyCode":"USD","private":"discard"}`))
			require.NoError(t, err)
		case "/v1alpha/properties/123456789/audiences":
			require.Equal(t, "pageSize=20&pageToken=cursor", r.URL.RawQuery)
			_, err := w.Write([]byte(`{"audiences":[{"name":"properties/123456789/audiences/42","displayName":"Returning users","description":"Recently active users","private":"discard"}],"nextPageToken":"next"}`))
			require.NoError(t, err)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	property, err := sdkgo.RunQuery(queryContext{}, client.GetProperty(), testConnection, GetPropertyInput{PropertyID: "123456789"})
	require.NoError(t, err)
	require.Equal(t, GetPropertyBranchFound, property.Branch)
	require.Equal(t, "Example Analytics", property.Value.DisplayName)
	page, err := sdkgo.RunQuery(queryContext{}, client.ListAudiences(), testConnection, ListAudiencesInput{PropertyID: "123456789", PageSize: 20, PageToken: "cursor"})
	require.NoError(t, err)
	require.Equal(t, ListAudiencesBranchListed, page.Branch)
	require.Equal(t, "next", page.Value.NextPageToken)
	require.Equal(t, "properties/123456789/audiences/42", page.Value.Audiences[0].Name)
}

func TestPropertyNotFoundAndProviderRetries(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(status.Load()))
		_, err := w.Write([]byte(`{}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	status.Store(http.StatusNotFound)
	missing, err := sdkgo.RunQuery(queryContext{}, client.GetProperty(), testConnection, GetPropertyInput{PropertyID: "123456789"})
	require.NoError(t, err)
	require.Equal(t, GetPropertyBranchNotFound, missing.Branch)
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunQuery(queryContext{}, client.RunReport(), testConnection, RunReportInput{PropertyID: "123456789", StartDate: "2026-11-01", EndDate: "2026-11-07", Metrics: []string{"activeUsers"}})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
}

func TestInvalidInputsDoNotContactGoogle(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	report, err := sdkgo.RunQuery(queryContext{}, client.RunReport(), testConnection, RunReportInput{PropertyID: "../123", Metrics: []string{"activeUsers"}})
	require.NoError(t, err)
	require.Equal(t, RunReportBranchDefect, report.Branch)
	audiences, err := sdkgo.RunQuery(queryContext{}, client.ListAudiences(), testConnection, ListAudiencesInput{PropertyID: "123456789", PageToken: "bad\nheader"})
	require.NoError(t, err)
	require.Equal(t, ListAudiencesBranchDefect, audiences.Branch)
	require.Zero(t, calls.Load())
}
