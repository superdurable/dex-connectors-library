// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package posthog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

type providerContext struct{ dex.Context }

func (providerContext) FlowID() string              { return "posthog-provider-test" }
func (providerContext) StepExecutionID() string     { return "provider-step" }
func (providerContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (providerContext) Done() <-chan struct{}       { return nil }
func (providerContext) Err() error                  { return nil }
func (providerContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "posthog", Name: "posthog"}

func newProviderClient(t *testing.T, address string) *Client {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {ProjectToken: sdkgo.NewSecretString("phc_test-token"), PersonalAPIKey: sdkgo.NewSecretString("phx_test-key")}}
	client, err := New(Config{CaptureURL: address, AppURL: address}, credentials)
	require.NoError(t, err)
	return client
}

func TestCaptureEventStableIdentityAndQuota(t *testing.T) {
	var firstUUID string
	var limited atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/i/v0/e/", r.URL.Path)
		require.Empty(t, r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.String(), "phc_test-token")
		var payload struct {
			Token      string `json:"token"`
			Event      string `json:"event"`
			DistinctID string `json:"distinct_id"`
			Timestamp  string `json:"timestamp"`
			UUID       string `json:"uuid"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, "phc_test-token", payload.Token)
		require.Equal(t, "Signed Up", payload.Event)
		require.Equal(t, "member-1", payload.DistinctID)
		require.Equal(t, "2026-10-29T00:00:00Z", payload.Timestamp)
		require.Len(t, payload.UUID, 36)
		if firstUUID != "" {
			require.Equal(t, firstUUID, payload.UUID)
		}
		firstUUID = payload.UUID
		if limited.Load() {
			_, _ = w.Write([]byte(`{"status":"Ok","quota_limited":["events"]}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"Ok"}`))
	}))
	defer server.Close()
	client := newProviderClient(t, server.URL)
	input := CaptureEventInput{Event: "Signed Up", DistinctID: "member-1", Timestamp: "2026-10-29T00:00:00Z"}
	for range 2 {
		result, err := sdkgo.RunMutation(providerContext{}, client.CaptureEvent(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, CaptureEventBranchAccepted, result.Branch)
		require.Equal(t, firstUUID, result.Value.EventID)
	}
	limited.Store(true)
	result, err := sdkgo.RunMutation(providerContext{}, client.CaptureEvent(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CaptureEventBranchQuotaLimited, result.Branch)
	require.NotContains(t, result.Failure.Message, "phc_test-token")
}

func TestCaptureEventRetriesAfterServerFailureAndRejectsBadTime(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := newProviderClient(t, server.URL)
	result, err := sdkgo.RunMutation(providerContext{}, client.CaptureEvent(), testConnection, CaptureEventInput{Event: "Signed Up", DistinctID: "member-1", Timestamp: "bad"})
	require.NoError(t, err)
	require.Equal(t, CaptureEventBranchDefect, result.Branch)
	require.EqualValues(t, 0, calls.Load())
	_, err = sdkgo.RunMutation(providerContext{}, client.CaptureEvent(), testConnection, CaptureEventInput{Event: "Signed Up", DistinctID: "member-1", Timestamp: "2026-10-29T00:00:00Z"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.EqualValues(t, 1, calls.Load())
}

func TestListInsightsBearerPaginationAndRateLimit(t *testing.T) {
	var limited atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/projects/42/insights/", r.URL.Path)
		require.Equal(t, "20", r.URL.Query().Get("limit"))
		require.Equal(t, "0", r.URL.Query().Get("offset"))
		require.Equal(t, "Bearer phx_test-key", r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.String(), "phx_test-key")
		if limited.Load() {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"count":2,"next":"https://example.test/next","results":[{"id":42,"name":"Signups by day","private":"SENTINEL"}]}`))
	}))
	defer server.Close()
	client := newProviderClient(t, server.URL)
	result, err := sdkgo.RunQuery(providerContext{}, client.ListInsights(), testConnection, ListInsightsInput{ProjectID: 42})
	require.NoError(t, err)
	require.Equal(t, ListInsightsBranchListed, result.Branch)
	require.True(t, result.Value.HasMore)
	require.Equal(t, "Signups by day", result.Value.Insights[0].Name)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
	limited.Store(true)
	_, err = sdkgo.RunQuery(providerContext{}, client.ListInsights(), testConnection, ListInsightsInput{ProjectID: 42})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), "phx_test-key")
}

func TestCaptureEventRetriesUnknownStatusesAndRejectsOnlyDefiniteRefusals(t *testing.T) {
	for status, isDefinite := range map[int]bool{
		http.StatusCreated: false, http.StatusFound: false, http.StatusConflict: false, http.StatusTooEarly: false,
		http.StatusUnavailableForLegalReasons: false, http.StatusBadRequest: true, http.StatusUnauthorized: true, http.StatusUnprocessableEntity: true,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		result, err := sdkgo.RunMutation(providerContext{}, newProviderClient(t, server.URL).CaptureEvent(), testConnection, CaptureEventInput{Event: "Signed Up", DistinctID: "member-1", Timestamp: "2026-10-29T00:00:00Z"})
		server.Close()
		if isDefinite {
			require.NoError(t, err, status)
			require.Equal(t, CaptureEventBranchProviderRejected, result.Branch, status)
			continue
		}
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, status)
	}
}

func TestListInsightsClassifiesCredentialFailures(t *testing.T) {
	for name, testCase := range map[string]struct {
		resolveErr error
		isTerminal bool
	}{
		"broker unavailable": {projectconfig.ErrCredentialBrokerUnavailable, false},
		"unlisted error":     {errors.New("credential store hiccup"), false},
		"reauthorization":    {projectconfig.ErrReauthorizationRequired, true},
		"object not found":   {projectconfig.ErrObjectNotFound, true},
		"broker rejected":    {projectconfig.ErrCredentialBrokerRejected, true},
	} {
		client, err := New(Config{CaptureURL: "http://127.0.0.1:1", AppURL: "http://127.0.0.1:1"}, failingCredentials{resolveErr: testCase.resolveErr})
		require.NoError(t, err)
		result, err := sdkgo.RunQuery(providerContext{}, client.ListInsights(), testConnection, ListInsightsInput{ProjectID: 42})
		if testCase.isTerminal {
			require.NoError(t, err, name)
			require.Equal(t, ListInsightsBranchDefect, result.Branch, name)
			continue
		}
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, name)
	}
}

type failingCredentials struct{ resolveErr error }

func (credentials failingCredentials) Resolve(sdkgo.Call) (Credentials, error) {
	return Credentials{}, credentials.resolveErr
}

func TestCaptureEventClassifiesCredentialFailures(t *testing.T) {
	for name, testCase := range map[string]struct {
		resolveErr error
		isTerminal bool
	}{
		"broker unavailable":            {projectconfig.ErrCredentialBrokerUnavailable, false},
		"static not configured":         {errors.New("connection is not configured"), false},
		"unlisted error":                {errors.New("credential store hiccup"), false},
		"sdk reauthorization":           {sdkgo.NewReauthorizationRequiredError(nil), true},
		"projectconfig reauthorization": {projectconfig.ErrReauthorizationRequired, true},
		"object not found":              {projectconfig.ErrObjectNotFound, true},
		"connection not selected":       {projectconfig.ErrConnectionNotSelected, true},
		"broker rejected":               {projectconfig.ErrCredentialBrokerRejected, true},
	} {
		t.Run(name, func(t *testing.T) {
			client, err := New(Config{CaptureURL: "http://127.0.0.1:1", AppURL: "http://127.0.0.1:1"}, failingCredentials{resolveErr: testCase.resolveErr})
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(providerContext{}, client.CaptureEvent(), testConnection, CaptureEventInput{Event: "Signed Up", DistinctID: "member-1", Timestamp: "2026-10-29T00:00:00Z"})
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, CaptureEventBranchDefect, result.Branch)
				return
			}
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		})
	}
}
