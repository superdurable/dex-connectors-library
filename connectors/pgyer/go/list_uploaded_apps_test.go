// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pgyer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "pgyer", Name: "pgyer"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "pgyer-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-uploaded-apps" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, apiKey string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(apiKey)}})
	require.NoError(t, err)
	return client
}
func TestListUploadedAppsRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/apiv2/app/listMy", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		form, err := url.ParseQuery(string(body))
		require.NoError(t, err)
		require.Equal(t, "secretKey", form.Get("_api_key"))
		require.Equal(t, "2", form.Get("page"))
		_, err = w.Write([]byte(`{"code":0,"message":"","data":[{"appKey":"x1y2z3","buildKey":"a1b2c3d4e5f6","buildName":"Example App","buildType":2,"buildVersion":"1.1","buildCreated":"2025-01-01 10:00:00","buildIdentifier":"com.example.app"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, "secretKey").ListUploadedApps(), testConnection, ListUploadedAppsInput{Page: 2})
	require.NoError(t, err)
	require.Equal(t, ListUploadedAppsBranchListed, result.Branch)
	require.Equal(t, UploadedAppPage{Apps: []UploadedApp{{AppKey: "x1y2z3", BuildKey: "a1b2c3d4e5f6", Name: "Example App", BuildType: 2, Version: "1.1", CreatedAt: "2025-01-01 10:00:00"}}}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestListUploadedAppsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"code":0,"data":[]}`, ListUploadedAppsBranchListed, false},
		{"invalid key", 200, `{"code":1002,"message":"bad"}`, ListUploadedAppsBranchProviderRejected, false},
		{"forbidden", 200, `{"code":1122}`, ListUploadedAppsBranchProviderRejected, false},
		{"hourly limit", 200, `{"code":1098}`, "", true},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, ListUploadedAppsBranchInvalidResponse, false},
		{"missing data", 200, `{"code":0}`, ListUploadedAppsBranchInvalidResponse, false},
		{"invalid app", 200, `{"code":0,"data":[{"buildName":"Example"}]}`, ListUploadedAppsBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, "secretKey").ListUploadedApps(), testConnection, ListUploadedAppsInput{})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
		})
	}
}
func TestInvalidPageAndKeyNeverCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, tc := range []struct {
		page int
		key  string
	}{{-1, "secretKey"}, {1, "bad key"}} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, tc.key).ListUploadedApps(), testConnection, ListUploadedAppsInput{Page: tc.page})
		require.NoError(t, err)
		require.Equal(t, ListUploadedAppsBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}
func TestTransportErrorDoesNotExposeAPIKey(t *testing.T) {
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, "http://127.0.0.1:1", "privateKey").ListUploadedApps(), testConnection, ListUploadedAppsInput{})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "privateKey")
}
